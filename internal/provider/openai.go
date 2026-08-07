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

// OpenAIProvider talks to any OpenAI Chat Completions compatible endpoint using
// server-sent events (SSE) streaming.
type OpenAIProvider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAI builds a provider for the given endpoint. baseURL is the API root
// (e.g. https://api.openai.com/v1); "/chat/completions" is appended per call.
// apiKey is the raw key already resolved from the keychain by the caller.
func NewOpenAI(baseURL, apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		// No client-level timeout: streaming responses are long-lived and
		// cancellation is driven by the request context instead.
		client: &http.Client{},
	}
}

// Name implements Provider.
func (p *OpenAIProvider) Name() string { return "openai" }

type openaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openaiRequest struct {
	Model         string               `json:"model"`
	Messages      []ChatMessage        `json:"messages"`
	Temperature   float64              `json:"temperature"`
	MaxTokens     int                  `json:"max_tokens,omitempty"`
	Stream        bool                 `json:"stream"`
	StreamOptions *openaiStreamOptions `json:"stream_options,omitempty"`
}

type openaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type openaiStreamEvent struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *openaiUsage `json:"usage"`
}

// Chat implements Provider. It streams the completion on the returned channel,
// which is closed when the stream ends; any error is delivered once on the
// error channel. Cancelling ctx aborts the HTTP read and closes the channels.
func (p *OpenAIProvider) Chat(ctx context.Context, req ChatRequest) (<-chan ChatChunk, <-chan error) {
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

func (p *OpenAIProvider) stream(ctx context.Context, req ChatRequest, out chan<- ChatChunk) error {
	body := openaiRequest{
		Model:         req.Model,
		Messages:      req.Messages,
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		Stream:        true,
		StreamOptions: &openaiStreamOptions{IncludeUsage: true},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("openai: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError("openai", resp)
	}
	return parseOpenAIStream(ctx, resp.Body, out)
}

// parseOpenAIStream reads an OpenAI SSE body and emits chunks. It is decoupled
// from the network so it can be unit-tested with a canned byte stream.
func parseOpenAIStream(ctx context.Context, r io.Reader, out chan<- ChatChunk) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var usage openaiUsage
	var haveUsage bool
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var ev openaiStreamEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return fmt.Errorf("openai: parse stream chunk: %w", err)
		}
		if ev.Usage != nil {
			usage = *ev.Usage
			haveUsage = true
		}
		for _, choice := range ev.Choices {
			if choice.Delta.Content == "" {
				continue
			}
			select {
			case out <- ChatChunk{Delta: choice.Delta.Content}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("openai: read stream: %w", err)
	}

	done := ChatChunk{Done: true}
	if haveUsage {
		done.PromptTokens = usage.PromptTokens
		done.CompletionTokens = usage.CompletionTokens
	}
	select {
	case out <- done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// statusError maps a non-2xx response to a readable error. It never includes
// request headers or the API key; the response body (the vendor's own error
// text) is truncated and included only for non-auth failures.
func statusError(name string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := truncate(strings.TrimSpace(string(body)), 300)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s: authentication failed (HTTP %d): verify the configured API key", name, resp.StatusCode)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s: rate limited (HTTP 429): %s", name, msg)
	default:
		return fmt.Errorf("%s: request error (HTTP %d): %s", name, resp.StatusCode, msg)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
