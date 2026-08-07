// Package sqlite implements store.Store on top of an *sql.DB opened by the
// db package (WAL, foreign keys on). Every write is parameterized and messages
// are appended immediately so nothing is lost on stop or crash (NFR 6.3).
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"axon/internal/model"
	"axon/internal/store"
)

// Store is the SQLite-backed implementation of store.Store.
type Store struct {
	db *sql.DB
}

// compile-time check that *Store satisfies the interface.
var _ store.Store = (*Store)(nil)

// New wraps an already-opened database in a Store.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// CreateConversation inserts a new conversation, generating a UUID v4 when the
// id is empty and stamping created_at/updated_at with the current unix millis.
func (s *Store) CreateConversation(ctx context.Context, c model.Conversation) (model.Conversation, error) {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	now := time.Now().UnixMilli()
	c.CreatedAt = now
	c.UpdatedAt = now

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversations (id, title, task_type, model, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.Title, c.TaskType, c.Model, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return model.Conversation{}, fmt.Errorf("insert conversation: %w", err)
	}

	return c, nil
}

// ListConversations returns all conversations ordered by updated_at DESC.
func (s *Store) ListConversations(ctx context.Context) ([]model.Conversation, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, task_type, model, created_at, updated_at
		 FROM conversations
		 ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	conversations := make([]model.Conversation, 0)
	for rows.Next() {
		var c model.Conversation
		if err := rows.Scan(&c.ID, &c.Title, &c.TaskType, &c.Model, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		conversations = append(conversations, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}

	return conversations, nil
}

// GetConversation returns a single conversation by id. A missing row surfaces
// as an error wrapping sql.ErrNoRows.
func (s *Store) GetConversation(ctx context.Context, id string) (model.Conversation, error) {
	var c model.Conversation
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, task_type, model, created_at, updated_at
		 FROM conversations
		 WHERE id = ?`,
		id,
	).Scan(&c.ID, &c.Title, &c.TaskType, &c.Model, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return model.Conversation{}, fmt.Errorf("get conversation %q: %w", id, err)
	}

	return c, nil
}

// RenameConversation updates the title and bumps updated_at.
func (s *Store) RenameConversation(ctx context.Context, id, title string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE conversations SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("rename conversation %q: %w", id, err)
	}

	return requireAffected(res, "rename conversation", id)
}

// DeleteConversation removes a conversation; its messages cascade via the
// foreign key.
func (s *Store) DeleteConversation(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM conversations WHERE id = ?`,
		id,
	)
	if err != nil {
		return fmt.Errorf("delete conversation %q: %w", id, err)
	}

	return requireAffected(res, "delete conversation", id)
}

// TouchConversation bumps updated_at, and updates model/task_type only when the
// respective argument is non-empty (COALESCE-NULLIF keeps existing values).
func (s *Store) TouchConversation(ctx context.Context, id, modelName, taskType string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE conversations
		 SET model = COALESCE(NULLIF(?, ''), model),
		     task_type = COALESCE(NULLIF(?, ''), task_type),
		     updated_at = ?
		 WHERE id = ?`,
		modelName, taskType, time.Now().UnixMilli(), id,
	)
	if err != nil {
		return fmt.Errorf("touch conversation %q: %w", id, err)
	}

	return requireAffected(res, "touch conversation", id)
}

// AppendMessage persists one message and bumps the parent conversation's
// updated_at in the same transaction, so a committed message and its sidebar
// ordering are always consistent and durable together.
func (s *Store) AppendMessage(ctx context.Context, m model.Message) (model.Message, error) {
	if m.Status == "" {
		m.Status = model.StatusComplete
	}
	m.CreatedAt = time.Now().UnixMilli()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Message{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages
		 (conversation_id, role, content, model, prompt_tokens, completion_tokens, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ConversationID, m.Role, m.Content, m.Model,
		m.PromptTokens, m.CompletionTokens, m.Status, m.CreatedAt,
	)
	if err != nil {
		return model.Message{}, fmt.Errorf("insert message: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return model.Message{}, fmt.Errorf("last insert id: %w", err)
	}
	m.ID = id

	// Bump the parent conversation so the sidebar reorders on new activity.
	if _, err := tx.ExecContext(ctx,
		`UPDATE conversations SET updated_at = ? WHERE id = ?`,
		m.CreatedAt, m.ConversationID,
	); err != nil {
		return model.Message{}, fmt.Errorf("bump conversation %q: %w", m.ConversationID, err)
	}

	if err := tx.Commit(); err != nil {
		return model.Message{}, fmt.Errorf("commit append message: %w", err)
	}

	return m, nil
}

// UpdateMessageContent replaces content/tokens/status of an existing message,
// e.g. finalizing a streamed reply (streaming -> complete) or marking it
// interrupted while keeping the partial text.
func (s *Store) UpdateMessageContent(ctx context.Context, id int64, content string, promptTokens, completionTokens int, status string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE messages
		 SET content = ?, prompt_tokens = ?, completion_tokens = ?, status = ?
		 WHERE id = ?`,
		content, promptTokens, completionTokens, status, id,
	)
	if err != nil {
		return fmt.Errorf("update message %d: %w", id, err)
	}

	return requireAffectedID(res, "update message", id)
}

// ListMessages returns all messages of a conversation ordered by id ASC.
func (s *Store) ListMessages(ctx context.Context, conversationID string) ([]model.Message, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, conversation_id, role, content, model,
		        prompt_tokens, completion_tokens, status, created_at
		 FROM messages
		 WHERE conversation_id = ?
		 ORDER BY id ASC`,
		conversationID,
	)
	if err != nil {
		return nil, fmt.Errorf("query messages for %q: %w", conversationID, err)
	}
	defer rows.Close()

	messages := make([]model.Message, 0)
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(
			&m.ID, &m.ConversationID, &m.Role, &m.Content, &m.Model,
			&m.PromptTokens, &m.CompletionTokens, &m.Status, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	return messages, nil
}

// requireAffected turns a zero-rows string-keyed update/delete into an error
// wrapping sql.ErrNoRows so callers can distinguish "not found".
func requireAffected(res sql.Result, op, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s %q rows affected: %w", op, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%s %q: %w", op, id, sql.ErrNoRows)
	}

	return nil
}

// requireAffectedID is requireAffected for integer-keyed rows (messages).
func requireAffectedID(res sql.Result, op string, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s %d rows affected: %w", op, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%s %d: %w", op, id, sql.ErrNoRows)
	}

	return nil
}
