package main

import (
	"context"
	"fmt"
	"strings"

	"axon/internal/embed"
	"axon/internal/provider"
)

// newEmbedder builds an embedder for semantic memory. Embeddings require an
// OpenAI-compatible endpoint (Anthropic has no embeddings API), so knowledge
// features degrade gracefully when none is configured: callers treat a nil
// embedder as "semantic features unavailable".
//
// Provider selection prefers the explicit embedding config (config.json's
// EmbeddingProvider + EmbeddingModel) so users can point semantic memory at a
// specific service and model (e.g. Zhipu embedding-3, bge-m3). When no
// embedding provider is configured it falls back to the first OpenAI-compatible
// provider with the embedder default model, preserving prior behavior.
func (a *App) newEmbedder() (embed.Embedder, error) {
	cfg := a.cfg.Get()

	var pc provider.Config
	model := cfg.EmbeddingModel

	if cfg.EmbeddingProvider != "" {
		found, ok := a.cfg.Provider(cfg.EmbeddingProvider)
		if !ok {
			return nil, fmt.Errorf("configured embedding provider %q not found", cfg.EmbeddingProvider)
		}
		if found.Protocol != "openai" {
			return nil, fmt.Errorf("embedding provider %q must use the openai protocol (embeddings go over the OpenAI-compatible API)", cfg.EmbeddingProvider)
		}
		pc = found
	} else {
		name, ok := a.providerForProtocol("openai")
		if !ok {
			return nil, fmt.Errorf("no OpenAI-compatible provider configured for embeddings")
		}
		pc, _ = a.cfg.Provider(name)
	}

	key, err := a.secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding api key: %w", err)
	}
	// Empty model -> embedder default (text-embedding-3-small).
	return embed.NewOpenAI(pc.BaseURL, key, model), nil
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
