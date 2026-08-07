// Package model holds the core domain types shared across the store,
// providers and the Wails-bound API. JSON tags are the contract the
// Svelte frontend consumes, so keep them stable and camelCase.
package model

// Role of a message author.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// Message lifecycle status. Partial assistant output is persisted as
// StatusStreaming/StatusInterrupted so a stop or crash never loses text.
const (
	StatusComplete    = "complete"
	StatusStreaming   = "streaming"
	StatusInterrupted = "interrupted"
)

// Conversation is one chat thread.
type Conversation struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	TaskType  string `json:"taskType"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"createdAt"` // unix epoch millis
	UpdatedAt int64  `json:"updatedAt"` // unix epoch millis
}

// Message is one turn within a conversation.
type Message struct {
	ID               int64  `json:"id"`
	ConversationID   string `json:"conversationId"`
	Role             string `json:"role"`
	Content          string `json:"content"`
	Model            string `json:"model"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
	Status           string `json:"status"`
	CreatedAt        int64  `json:"createdAt"` // unix epoch millis
}
