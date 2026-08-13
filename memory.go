package main

import (
	"context"
	"fmt"
	"strings"

	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/provider"
)

// newEmbedder builds the embedder for every recall/index path according to the
// user's explicit EmbeddingMode — there is NO silent cloud->local degradation.
//
//   - keyword mode (default): always the pure-Go local lexical embedder. Never
//     touches the network, never errors.
//   - semantic mode: ONLY the configured cloud model. If cloud is not
//     configured, misconfigured, or the endpoint can't embed, this returns an
//     error and the caller must NOT fall back — recall/index simply proceeds
//     without vectors (empty semantic recall) and surfaces the problem, so a
//     broken setup is visible instead of masquerading as low-quality results.
//
// Cloud usability is verified with a live probe (embed a short string) the first
// time an endpoint is seen; the result is cached per process.
func (a *App) newEmbedder() (embed.Embedder, error) {
	if a.cfg.Get().EmbeddingMode != config.EmbeddingModeSemantic {
		// keyword (or unset): local lexical embedder, no network, no error.
		return embed.NewLocal(), nil
	}

	// Semantic mode: cloud only, no fallback.
	cloud, key, err := a.buildCloudEmbedder()
	if err != nil {
		return nil, fmt.Errorf("语义模式已开启，但云端 embedding 不可用：%w", err)
	}
	if cloud == nil {
		return nil, fmt.Errorf("语义模式已开启，但未配置可用的 embedding Provider（需 openai 协议）")
	}
	usable, probed, perr := a.embedProbeCache().Usable(a.ctx, cloud, key)
	if !usable {
		if probed && perr != nil {
			return nil, fmt.Errorf("语义模式已开启，但云端 embedding 调用失败：%w", perr)
		}
		return nil, fmt.Errorf("语义模式已开启，但云端 embedding 不可用")
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
