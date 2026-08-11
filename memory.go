package main

import (
	"context"
	"fmt"
	"strings"

	"axon/internal/embed"
	"axon/internal/provider"
)

// newEmbedder builds an embedder from the first OpenAI-protocol provider that
// has a stored key. Embeddings require an OpenAI-compatible endpoint (Anthropic
// has no embeddings API), so knowledge features degrade gracefully when none is
// configured: callers treat a nil embedder as "semantic features unavailable".
func (a *App) newEmbedder() (embed.Embedder, error) {
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return nil, fmt.Errorf("no OpenAI-compatible provider configured for embeddings")
	}
	pc, _ := a.cfg.Provider(name)
	key, err := a.secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding api key: %w", err)
	}
	// Empty model -> embedder default (text-embedding-3-small).
	return embed.NewOpenAI(pc.BaseURL, key, ""), nil
}

// collectReply runs a non-streaming-style collection over the provider's stream,
// concatenating all deltas into the full reply text.
func collectReply(ctx context.Context, prov provider.Provider, req provider.ChatRequest) (string, error) {
	chunks, errs := prov.Chat(ctx, req)
	var b strings.Builder
	for chunk := range chunks {
		b.WriteString(chunk.Delta)
	}
	select {
	case err := <-errs:
		if err != nil {
			return "", err
		}
	default:
	}
	return b.String(), nil
}
