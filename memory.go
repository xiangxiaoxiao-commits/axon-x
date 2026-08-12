package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"axon/internal/embed"
	"axon/internal/provider"
)

// newEmbedder builds the embedder used by every recall/index path. It NEVER
// returns an error: recall must always have a working embedder. A cloud
// (OpenAI-compatible) endpoint is preferred, but a cloud endpoint that is
// missing, misconfigured, or that serves chat while failing on /v1/embeddings
// (503) falls back to the pure-Go LocalEmbedder so semantic/chunk recall keeps
// working (fuzzy lexical vectors) instead of collapsing to empty query vectors.
//
// Degradation chain: usable cloud embedding > local embedding > (only if the
// query itself can't be embedded) substring matching.
//
// Cloud usability is verified with a live probe (embed a short string) the first
// time an endpoint is seen; the result is cached per process so later recalls
// don't re-probe the network. TestEmbedding is the explicit entry point that
// still surfaces misconfiguration as an error (see buildCloudEmbedder).
func (a *App) newEmbedder() (embed.Embedder, error) {
	cloud, key, err := a.buildCloudEmbedder()
	if err != nil {
		// Cloud is configured but broken (unknown provider, wrong protocol,
		// missing key). Don't fail recall: log and use the local fallback.
		log.Printf("axon: cloud embedding unavailable (%v); falling back to local embedder", err)
		return embed.NewLocal(), nil
	}
	if cloud == nil {
		// Nothing configured: local, zero-dependency embedder.
		return embed.NewLocal(), nil
	}

	usable, probed, perr := a.embedProbeCache().Usable(a.ctx, cloud, key)
	if !usable {
		if probed {
			log.Printf("axon: cloud embedding not usable (%v); falling back to local embedder", perr)
		}
		return embed.NewLocal(), nil
	}
	return cloud, nil
}

// buildCloudEmbedder resolves the configured cloud embedder without any probe or
// local fallback. It returns (nil, "", nil) when no cloud embedding is
// configured, and a real error when an explicit config is invalid — so the
// explicit TestEmbedding entry point can surface misconfiguration to the user.
// The returned key identifies the endpoint for probe caching (no secret in it).
func (a *App) buildCloudEmbedder() (embed.Embedder, string, error) {
	cfg := a.cfg.Get()

	var pc provider.Config
	model := cfg.EmbeddingModel

	if cfg.EmbeddingProvider != "" {
		found, ok := a.cfg.Provider(cfg.EmbeddingProvider)
		if !ok {
			return nil, "", fmt.Errorf("configured embedding provider %q not found", cfg.EmbeddingProvider)
		}
		if found.Protocol != "openai" {
			return nil, "", fmt.Errorf("embedding provider %q must use the openai protocol (embeddings go over the OpenAI-compatible API)", cfg.EmbeddingProvider)
		}
		pc = found
	} else {
		name, ok := a.providerForProtocol("openai")
		if !ok {
			return nil, "", nil // no cloud embedding configured
		}
		pc, _ = a.cfg.Provider(name)
	}

	key, err := a.secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, "", fmt.Errorf("resolve embedding api key: %w", err)
	}
	// Empty model -> embedder default (text-embedding-3-small).
	return embed.NewOpenAI(pc.BaseURL, key, model), embed.ProbeKey(pc.BaseURL, model, pc.KeyRef), nil
}

// embedProbeCache returns this App's cloud-embedder probe cache, creating it on
// first use. The lazy init (guarded by mu) covers tests that build App by struct
// literal, bypassing NewApp.
func (a *App) embedProbeCache() *embed.ProbeCache {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.embedProbe == nil {
		a.embedProbe = embed.NewProbeCache()
	}
	return a.embedProbe
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
