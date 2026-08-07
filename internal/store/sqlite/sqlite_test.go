package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"axon/internal/db"
	"axon/internal/model"
	"axon/internal/store/sqlite"
)

// newStore opens a fresh, isolated database under t.TempDir() (WAL + foreign
// keys on, via db.Open) and returns the store together with the raw *sql.DB for
// tests that assert on the schema directly. It never touches the real
// AppDataDir.
func newStore(t *testing.T) (*sqlite.Store, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlite.New(sqlDB), sqlDB
}

// mustCreate creates a conversation and fails the test on error.
func mustCreate(t *testing.T, s *sqlite.Store, title string) model.Conversation {
	t.Helper()
	c, err := s.CreateConversation(context.Background(), model.Conversation{Title: title})
	if err != nil {
		t.Fatalf("CreateConversation(%q): %v", title, err)
	}
	return c
}

// TestCreateConversation verifies UUID generation for empty IDs, respect for
// caller-supplied IDs, and non-zero timestamps.
func TestCreateConversation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	t.Run("generates uuid when id empty", func(t *testing.T) {
		c, err := s.CreateConversation(ctx, model.Conversation{Title: "auto"})
		if err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		if c.ID == "" {
			t.Error("expected generated UUID, got empty ID")
		}
		if len(c.ID) != 36 {
			t.Errorf("expected 36-char UUID v4, got %q (len %d)", c.ID, len(c.ID))
		}
		if c.CreatedAt == 0 || c.UpdatedAt == 0 {
			t.Errorf("expected non-zero timestamps, got created=%d updated=%d", c.CreatedAt, c.UpdatedAt)
		}
		if c.CreatedAt != c.UpdatedAt {
			t.Errorf("on create, created_at (%d) should equal updated_at (%d)", c.CreatedAt, c.UpdatedAt)
		}
	})

	t.Run("keeps caller supplied id", func(t *testing.T) {
		c, err := s.CreateConversation(ctx, model.Conversation{ID: "fixed-id", Title: "kept"})
		if err != nil {
			t.Fatalf("CreateConversation: %v", err)
		}
		if c.ID != "fixed-id" {
			t.Errorf("expected caller id %q, got %q", "fixed-id", c.ID)
		}
	})
}

// TestGetConversation verifies round-trip read and not-found semantics.
func TestGetConversation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	created := mustCreate(t, s, "hello")
	got, err := s.GetConversation(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got != created {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, created)
	}

	_, err = s.GetConversation(ctx, "does-not-exist")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows for missing conversation, got %v", err)
	}
}

// TestListConversationsOrder verifies conversations come back ordered by
// updated_at DESC (most recently active first).
func TestListConversationsOrder(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	// Create three conversations with distinct timestamps.
	first := mustCreate(t, s, "first")
	time.Sleep(5 * time.Millisecond)
	second := mustCreate(t, s, "second")
	time.Sleep(5 * time.Millisecond)
	third := mustCreate(t, s, "third")

	list, err := s.ListConversations(ctx)
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 conversations, got %d", len(list))
	}

	// Newest updated_at first: third, second, first.
	wantOrder := []string{third.ID, second.ID, first.ID}
	for i, want := range wantOrder {
		if list[i].ID != want {
			t.Errorf("position %d: got %q, want %q (expected updated_at DESC)", i, list[i].ID, want)
		}
	}
}

// TestRenameConversation verifies the title changes and updated_at advances,
// and that renaming a missing conversation reports not-found.
func TestRenameConversation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "old title")
	time.Sleep(5 * time.Millisecond)

	if err := s.RenameConversation(ctx, c.ID, "new title"); err != nil {
		t.Fatalf("RenameConversation: %v", err)
	}

	got, err := s.GetConversation(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.Title != "new title" {
		t.Errorf("title not updated: got %q, want %q", got.Title, "new title")
	}
	if got.UpdatedAt <= c.UpdatedAt {
		t.Errorf("updated_at did not advance: before=%d after=%d", c.UpdatedAt, got.UpdatedAt)
	}

	if err := s.RenameConversation(ctx, "missing", "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("rename missing: expected sql.ErrNoRows, got %v", err)
	}
}

// TestDeleteConversation verifies deletion removes the row and that deleting a
// missing conversation reports not-found.
func TestDeleteConversation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "to delete")
	if err := s.DeleteConversation(ctx, c.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	if _, err := s.GetConversation(ctx, c.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("after delete: expected sql.ErrNoRows, got %v", err)
	}

	if err := s.DeleteConversation(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("delete missing: expected sql.ErrNoRows, got %v", err)
	}
}

// TestDeleteCascade verifies that deleting a conversation also removes its
// messages via ON DELETE CASCADE. This only works when foreign_keys=ON, which
// db.Open sets on every pooled connection, so the raw row count confirms the
// pragma is actually in effect.
func TestDeleteCascade(t *testing.T) {
	s, sqlDB := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "with messages")
	for i := 0; i < 3; i++ {
		if _, err := s.AppendMessage(ctx, model.Message{
			ConversationID: c.ID,
			Role:           model.RoleUser,
			Content:        "msg",
		}); err != nil {
			t.Fatalf("AppendMessage: %v", err)
		}
	}

	// Sanity: messages exist before delete.
	if got := countMessages(t, sqlDB, c.ID); got != 3 {
		t.Fatalf("expected 3 messages before delete, got %d", got)
	}

	if err := s.DeleteConversation(ctx, c.ID); err != nil {
		t.Fatalf("DeleteConversation: %v", err)
	}

	// The cascade must have removed the child rows.
	if got := countMessages(t, sqlDB, c.ID); got != 0 {
		t.Errorf("expected 0 messages after cascade delete, got %d (foreign_keys likely OFF)", got)
	}
}

// countMessages returns the number of message rows for a conversation, reading
// the table directly to bypass any store-layer filtering.
func countMessages(t *testing.T, sqlDB *sql.DB, conversationID string) int {
	t.Helper()
	var n int
	if err := sqlDB.QueryRow(
		`SELECT COUNT(*) FROM messages WHERE conversation_id = ?`, conversationID,
	).Scan(&n); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return n
}

// TestAppendMessage verifies the returned ID/CreatedAt are populated and that
// the parent conversation's updated_at is bumped in the same transaction.
func TestAppendMessage(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "conv")
	time.Sleep(5 * time.Millisecond)

	m, err := s.AppendMessage(ctx, model.Message{
		ConversationID: c.ID,
		Role:           model.RoleUser,
		Content:        "hi there",
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if m.ID == 0 {
		t.Error("expected non-zero message ID")
	}
	if m.CreatedAt == 0 {
		t.Error("expected non-zero CreatedAt")
	}
	if m.Status != model.StatusComplete {
		t.Errorf("expected default status %q, got %q", model.StatusComplete, m.Status)
	}

	// Parent conversation must be bumped so the sidebar reorders.
	got, err := s.GetConversation(ctx, c.ID)
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if got.UpdatedAt <= c.UpdatedAt {
		t.Errorf("conversation updated_at not bumped: before=%d after=%d", c.UpdatedAt, got.UpdatedAt)
	}
}

// TestListMessages verifies ascending id order and that an empty conversation
// returns a non-nil, zero-length slice.
func TestListMessages(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	t.Run("empty conversation returns non-nil empty slice", func(t *testing.T) {
		c := mustCreate(t, s, "empty")
		msgs, err := s.ListMessages(ctx, c.ID)
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		if msgs == nil {
			t.Error("expected non-nil slice, got nil")
		}
		if len(msgs) != 0 {
			t.Errorf("expected 0 messages, got %d", len(msgs))
		}
	})

	t.Run("ordered by id asc", func(t *testing.T) {
		c := mustCreate(t, s, "chat")
		contents := []string{"first", "second", "third"}
		for _, content := range contents {
			if _, err := s.AppendMessage(ctx, model.Message{
				ConversationID: c.ID,
				Role:           model.RoleUser,
				Content:        content,
			}); err != nil {
				t.Fatalf("AppendMessage(%q): %v", content, err)
			}
		}

		msgs, err := s.ListMessages(ctx, c.ID)
		if err != nil {
			t.Fatalf("ListMessages: %v", err)
		}
		if len(msgs) != len(contents) {
			t.Fatalf("expected %d messages, got %d", len(contents), len(msgs))
		}
		for i := range msgs {
			if msgs[i].Content != contents[i] {
				t.Errorf("position %d: got content %q, want %q", i, msgs[i].Content, contents[i])
			}
			if i > 0 && msgs[i].ID <= msgs[i-1].ID {
				t.Errorf("ids not ascending: msgs[%d].ID=%d <= msgs[%d].ID=%d", i, msgs[i].ID, i-1, msgs[i-1].ID)
			}
		}
	})
}

// TestUpdateMessageContent verifies finalizing a streamed message
// (streaming -> complete) updates content/tokens/status, and that updating a
// missing message reports not-found.
func TestUpdateMessageContent(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	c := mustCreate(t, s, "streaming conv")

	// Persist a partial, still-streaming assistant message.
	m, err := s.AppendMessage(ctx, model.Message{
		ConversationID: c.ID,
		Role:           model.RoleAssistant,
		Content:        "partial",
		Status:         model.StatusStreaming,
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	// Finalize it as the stream completes.
	const finalContent = "partial and then the rest"
	if err := s.UpdateMessageContent(ctx, m.ID, finalContent, 12, 34, model.StatusComplete); err != nil {
		t.Fatalf("UpdateMessageContent: %v", err)
	}

	msgs, err := s.ListMessages(ctx, c.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	got := msgs[0]
	if got.Content != finalContent {
		t.Errorf("content: got %q, want %q", got.Content, finalContent)
	}
	if got.PromptTokens != 12 || got.CompletionTokens != 34 {
		t.Errorf("tokens: got prompt=%d completion=%d, want 12/34", got.PromptTokens, got.CompletionTokens)
	}
	if got.Status != model.StatusComplete {
		t.Errorf("status: got %q, want %q", got.Status, model.StatusComplete)
	}

	// Updating a non-existent message reports not-found.
	if err := s.UpdateMessageContent(ctx, 999999, "x", 0, 0, model.StatusComplete); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("update missing message: expected sql.ErrNoRows, got %v", err)
	}
}

// TestTouchConversationNotFound verifies touching a missing conversation
// reports not-found (the found path is exercised indirectly via AppendMessage).
func TestTouchConversationNotFound(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()

	if err := s.TouchConversation(ctx, "missing", "", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("touch missing: expected sql.ErrNoRows, got %v", err)
	}
}


