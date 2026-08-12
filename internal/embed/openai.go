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

// openaiBatchRequest sends an array input; the OpenAI embeddings API accepts a
// list and returns one datum per element, tagged with its index.
type openaiBatchRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// embedBatchSize bounds how many texts go in one request. The API accepts large
// arrays, but a moderate batch keeps request bodies and latency reasonable while
// still cutting HTTP round-trips by ~100x versus per-text calls.
const embedBatchSize = 96

// embedMaxRetries bounds transient-failure retries (rate limits, 5xx) per batch.
const embedMaxRetries = 3

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
	parsed, err := e.postEmbeddings(ctx, buf)
	if err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("openai embed: response contained no embedding data")
	}
	return parsed.Data[0].Embedding, nil
}

// EmbedBatch implements Embedder, splitting texts into fixed-size batches and
// concatenating the results in input order. Each batch retries a few times on
// rate-limit / 5xx errors with linear backoff.
func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedOneBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

// embedOneBatch embeds a single batch, ordering results by the response index so
// they line up one-to-one with the input slice.
func (e *OpenAIEmbedder) embedOneBatch(ctx context.Context, texts []string) ([][]float32, error) {
	buf, err := json.Marshal(openaiBatchRequest{
		Model:          e.model,
		Input:          texts,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("openai embed: encode batch request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= embedMaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		parsed, err := e.postEmbeddings(ctx, buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
			continue // transient: retry with backoff
		}
		if len(parsed.Data) != len(texts) {
			return nil, fmt.Errorf("openai embed: batch returned %d embeddings for %d inputs", len(parsed.Data), len(texts))
		}
		vecs := make([][]float32, len(texts))
		for _, d := range parsed.Data {
			if d.Index < 0 || d.Index >= len(vecs) {
				return nil, fmt.Errorf("openai embed: batch response index %d out of range", d.Index)
			}
			vecs[d.Index] = d.Embedding
		}
		return vecs, nil
	}
	return nil, fmt.Errorf("openai embed: batch failed after %d retries: %w", embedMaxRetries, lastErr)
}

// postEmbeddings sends a prepared JSON body to /embeddings and decodes the reply.
func (e *OpenAIEmbedder) postEmbeddings(ctx context.Context, body []byte) (*openaiEmbedResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embeddings", bytes.NewReader(body))
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
	return &parsed, nil
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
