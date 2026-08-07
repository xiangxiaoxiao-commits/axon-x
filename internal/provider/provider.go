// Package provider abstracts the chat APIs of different model vendors behind
// one streaming interface. Implementations translate a common ChatRequest into
// vendor-specific wire formats (OpenAI-compatible SSE, Anthropic events) and
// emit normalized ChatChunk values until the stream ends.
package provider

import "context"

// Role constants mirror model.Role; duplicated here to keep the provider
// package free of a store dependency.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// ChatMessage is one turn sent to the model.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is a vendor-neutral chat completion request.
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"maxTokens"` // 0 = provider default
}

// ChatChunk is one streamed piece of the assistant's reply. Exactly one of
// Delta (incremental text) or Done is meaningful per chunk; the final chunk
// has Done=true and may carry token usage.
type ChatChunk struct {
	Delta            string `json:"delta"`
	Done             bool   `json:"done"`
	PromptTokens     int    `json:"promptTokens"`
	CompletionTokens int    `json:"completionTokens"`
}

// Provider is a streaming chat backend for one vendor/protocol.
type Provider interface {
	// Name identifies the provider instance (e.g. "openai", "anthropic").
	Name() string

	// Chat starts a streaming completion. Chunks are delivered on the returned
	// channel, which is closed when the stream ends. Any error (including a
	// mid-stream failure) is delivered on the error channel exactly once; the
	// caller should select on both. Cancelling ctx stops generation and closes
	// the channels — partial output already delivered stays valid.
	Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, <-chan error)
}

// Config describes how to reach a provider. The API key is never stored here;
// it is fetched from the OS keychain at call time by key reference.
type Config struct {
	Name     string `json:"name"`     // instance id, unique
	Protocol string `json:"protocol"` // "openai" | "anthropic"
	BaseURL  string `json:"baseUrl"`  // e.g. https://api.openai.com/v1
	// KeyRef is the keychain account under which this provider's API key is
	// stored; the raw key is resolved via the secret package, never persisted
	// in config or DB.
	KeyRef string `json:"keyRef"`
}
