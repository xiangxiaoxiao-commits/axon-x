package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropicVersion is the required API version header value.
const anthropicVersion = "2023-06-01"

// defaultAnthropicMaxTokens is used when the request leaves MaxTokens at 0,
// since the Anthropic Messages API requires max_tokens to be present.
const defaultAnthropicMaxTokens = 4096

// AnthropicProvider talks to the Anthropic Messages API using its native SSE
// event stream.
type AnthropicProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewAnthropic builds a provider for the given endpoint. baseURL is the API
// root (e.g. https://api.anthropic.com/v1); "/messages" is appended per call.
// apiKey is the raw key already resolved from the keychain by the caller.
func NewAnthropic(baseURL, apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  &http.Client{},
	}
}

// Name implements Provider.
func (p *AnthropicProvider) Name() string { return "anthropic" }

type anthropicRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	System      string        `json:"system,omitempty"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Text string `json:"text"` // content_block_delta
	} `json:"delta"`
	Message struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"` // message_start
	Usage struct {
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"` // message_delta
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider. It streams the completion on the returned channel,
// which is closed when the stream ends; any error is delivered once on the
// error channel. Cancelling ctx aborts the HTTP read and closes the channels.
func (p *AnthropicProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, <-chan error) {
	chunks := make(chan ChatChunk)
	errs := make(chan error, 1)
	go func() {
		defer close(chunks)
		if err := p.stream(ctx, req, chunks); err != nil {
			errs <- err
		}
	}()
	return chunks, errs
}

func (p *AnthropicProvider) stream(ctx context.Context, req ChatRequest, out chan<- ChatChunk) error {
	system, messages := splitSystem(req.Messages)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultAnthropicMaxTokens
	}
	body := anthropicRequest{
		Model:       req.Model,
		Messages:    messages,
		System:      system,
		Temperature: req.Temperature,
		MaxTokens:   maxTokens,
		Stream:      true,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("anthropic: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError("anthropic", resp)
	}
	return parseAnthropicStream(ctx, resp.Body, out)
}

// splitSystem extracts role=system turns into the top-level system string
// (Anthropic keeps system outside the messages array) and returns the
// remaining conversational messages. Multiple system turns are joined by blank
// lines to preserve their content.
func splitSystem(msgs []ChatMessage) (system string, rest []ChatMessage) {
	var systemParts []string
	rest = make([]ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == RoleSystem {
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
			continue
		}
		rest = append(rest, m)
	}
	return strings.Join(systemParts, "\n\n"), rest
}

// parseAnthropicStream reads an Anthropic SSE body and emits chunks. It keys off
// the JSON "type" field rather than the SSE event line, and is decoupled from
// the network so it can be unit-tested with a canned byte stream.
func parseAnthropicStream(ctx context.Context, r io.Reader, out chan<- ChatChunk) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var promptTokens, completionTokens int
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("anthropic: parse stream event: %w", err)
		}
		switch ev.Type {
		case "message_start":
			promptTokens = ev.Message.Usage.InputTokens
		case "content_block_delta":
			if ev.Delta.Text == "" {
				continue
			}
			select {
			case out <- ChatChunk{Delta: ev.Delta.Text}:
			case <-ctx.Done():
				return ctx.Err()
			}
		case "message_delta":
			if ev.Usage.OutputTokens > 0 {
				completionTokens = ev.Usage.OutputTokens
			}
		case "error":
			return fmt.Errorf("anthropic: stream error: %s", truncate(ev.Error.Message, 300))
		case "message_stop":
			// Terminal event; stop reading.
			return emitAnthropicDone(ctx, out, promptTokens, completionTokens)
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("anthropic: read stream: %w", err)
	}
	return emitAnthropicDone(ctx, out, promptTokens, completionTokens)
}

func emitAnthropicDone(ctx context.Context, out chan<- ChatChunk, prompt, completion int) error {
	select {
	case out <- ChatChunk{Done: true, PromptTokens: prompt, CompletionTokens: completion}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
