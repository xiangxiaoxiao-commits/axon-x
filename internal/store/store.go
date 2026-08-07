// Package store is the persistence contract for Phase 1 archival.
// Implementations must persist every message append-only with fsync
// durability (WAL) so nothing is lost on stop, crash or power loss.
package store

import (
	"context"

	"axon/internal/model"
)

// Store is the repository over conversations and messages.
// All methods take a context for cancellation/timeout.
type Store interface {
	// --- Conversations (F1.4) ---

	// CreateConversation inserts a new conversation and returns it with
	// timestamps populated. If c.ID is empty a UUID v4 is generated.
	CreateConversation(ctx context.Context, c model.Conversation) (model.Conversation, error)

	// ListConversations returns conversations ordered by updated_at DESC.
	ListConversations(ctx context.Context) ([]model.Conversation, error)

	// GetConversation returns a single conversation by id.
	GetConversation(ctx context.Context, id string) (model.Conversation, error)

	// RenameConversation updates the title and bumps updated_at.
	RenameConversation(ctx context.Context, id, title string) error

	// DeleteConversation removes a conversation and cascades its messages.
	DeleteConversation(ctx context.Context, id string) error

	// TouchConversation bumps updated_at (and optionally model/taskType when
	// non-empty) so the sidebar reorders after activity.
	TouchConversation(ctx context.Context, id, model, taskType string) error

	// --- Messages (F1.3) ---

	// AppendMessage persists one message immediately (append-only) and
	// returns it with ID and CreatedAt populated. Must also bump the parent
	// conversation's updated_at in the same transaction.
	AppendMessage(ctx context.Context, m model.Message) (model.Message, error)

	// UpdateMessageContent replaces content/tokens/status of an existing
	// message. Used to finalize a streamed assistant message (streaming ->
	// complete) or mark it interrupted while keeping partial text.
	UpdateMessageContent(ctx context.Context, id int64, content string, promptTokens, completionTokens int, status string) error

	// ListMessages returns all messages of a conversation ordered by id ASC.
	ListMessages(ctx context.Context, conversationID string) ([]model.Message, error)
}
