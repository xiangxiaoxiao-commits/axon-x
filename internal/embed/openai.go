package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultOpenAIModel is used when the caller passes an empty model id.
const defaultOpenAIModel = "text-embedding-3-small"

// OpenAIEmbedder implements Embedder against any OpenAI embeddings compatible
// endpoint. It is safe for concurrent use.
type OpenAIEmbedder struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAI builds an embedder for the given endpoint. baseURL is the API root
// (e.g. https://api.openai.com/v1); "/embeddings" is appended per call. apiKey
// is the raw key already resolved from the keychain by the caller. An empty
// model defaults to text-embedding-3-small.
func NewOpenAI(baseURL, apiKey, model string) *OpenAIEmbedder {
	if model == "" {
		model = defaultOpenAIModel
	}
	return &OpenAIEmbedder{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		// Embeddings are a single non-streaming request, so a whole-request
		// timeout is appropriate; the request context can still cancel earlier.
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Model implements Embedder.
func (e *OpenAIEmbedder) Model() string { return e.model }

type openaiEmbedRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	EncodingFormat string `json:"encoding_format"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed implements Embedder for a single text.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	buf, err := json.Marshal(openaiEmbedRequest{
		Model:          e.model,
		Input:          text,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("openai embed: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("openai embed: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("openai embed: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, embedStatusError(resp)
	}

	var parsed openaiEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openai embed: decode response: %w", err)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("openai embed: response contained no embedding data")
	}
	return parsed.Data[0].Embedding, nil
}

// embedStatusError maps a non-2xx response to a readable error. It never
// includes request headers or the API key; the response body (the vendor's own
// error text) is truncated and included only for non-auth failures.
func embedStatusError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	msg := truncateBody(strings.TrimSpace(string(body)), 300)
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("openai embed: authentication failed (HTTP %d): verify the configured API key", resp.StatusCode)
	case http.StatusTooManyRequests:
		return fmt.Errorf("openai embed: rate limited (HTTP 429): %s", msg)
	default:
		return fmt.Errorf("openai embed: request error (HTTP %d): %s", resp.StatusCode, msg)
	}
}

func truncateBody(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// Compile-time check that OpenAIEmbedder satisfies the Embedder interface.
var _ Embedder = (*OpenAIEmbedder)(nil)
