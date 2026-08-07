package provider

import (
	"context"
	"strings"
	"testing"
)

// collectStream drains a parser against a canned SSE body and returns the
// deltas concatenated, the final Done chunk, and any error.
func collectStream(parse func(context.Context, *strings.Reader, chan<- ChatChunk) error, raw string) (string, ChatChunk, error) {
	out := make(chan ChatChunk, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- parse(context.Background(), strings.NewReader(raw), out)
		close(out)
	}()
	var text strings.Builder
	var done ChatChunk
	for c := range out {
		if c.Done {
			done = c
			continue
		}
		text.WriteString(c.Delta)
	}
	return text.String(), done, <-errCh
}

func TestParseOpenAIStream(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"choices":[{"delta":{"role":"assistant","content":""}}]}`,
		`data: {"choices":[{"delta":{"content":"Hel"}}]}`,
		`data: {"choices":[{"delta":{"content":"lo"}}]}`,
		`data: {"choices":[{"delta":{}}]}`,
		`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":2}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	text, done, err := collectStream(
		func(ctx context.Context, r *strings.Reader, out chan<- ChatChunk) error {
			return parseOpenAIStream(ctx, r, out)
		}, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hello" {
		t.Fatalf("text = %q, want %q", text, "Hello")
	}
	if !done.Done {
		t.Fatal("expected a Done chunk")
	}
	if done.PromptTokens != 11 || done.CompletionTokens != 2 {
		t.Fatalf("usage = (%d,%d), want (11,2)", done.PromptTokens, done.CompletionTokens)
	}
}

func TestParseAnthropicStream(t *testing.T) {
	raw := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"usage":{"input_tokens":9}}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}`,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":" there"}}`,
		`event: message_delta`,
		`data: {"type":"message_delta","usage":{"output_tokens":3}}`,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	text, done, err := collectStream(
		func(ctx context.Context, r *strings.Reader, out chan<- ChatChunk) error {
			return parseAnthropicStream(ctx, r, out)
		}, raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Hi there" {
		t.Fatalf("text = %q, want %q", text, "Hi there")
	}
	if !done.Done {
		t.Fatal("expected a Done chunk")
	}
	if done.PromptTokens != 9 || done.CompletionTokens != 3 {
		t.Fatalf("usage = (%d,%d), want (9,3)", done.PromptTokens, done.CompletionTokens)
	}
}

func TestSplitSystem(t *testing.T) {
	msgs := []ChatMessage{
		{Role: RoleSystem, Content: "be brief"},
		{Role: RoleUser, Content: "hi"},
		{Role: RoleSystem, Content: "and kind"},
		{Role: RoleAssistant, Content: "ok"},
	}
	system, rest := splitSystem(msgs)
	if system != "be brief\n\nand kind" {
		t.Fatalf("system = %q", system)
	}
	if len(rest) != 2 || rest[0].Role != RoleUser || rest[1].Role != RoleAssistant {
		t.Fatalf("rest = %+v", rest)
	}
}
