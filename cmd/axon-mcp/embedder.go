package main

import (
	"context"
	"fmt"
	"log"

	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/provider"
	"axon/internal/secret"
)

// newEmbedder builds the embedder for the recall path, honoring the SAME
// explicit EmbeddingMode the GUI writes — no silent cloud->local degradation:
//
//   - keyword mode (default): always the local lexical embedder.
//   - semantic mode: cloud model ONLY. If cloud is missing/misconfigured/not
//     usable, it returns nil and the caller runs recall WITHOUT a query vector
//     (keyword channel only) rather than pretending a local vector is semantic.
//     This keeps the query embedder consistent with how the graph was indexed.
//
// Cloud usability is verified with a live probe the first time an endpoint is
// seen; the result is cached in probe. probe must be non-nil.
func newEmbedder(ctx context.Context, cfg *config.Manager, secrets secret.Store, probe *embed.ProbeCache) embed.Embedder {
	if cfg.Get().EmbeddingMode != config.EmbeddingModeSemantic {
		return embed.NewLocal() // keyword (or unset): local lexical embedder
	}

	// Semantic mode: cloud only, no fallback.
	cloud, key, err := buildCloudEmbedder(cfg, secrets)
	if err != nil {
		log.Printf("axon-mcp: semantic mode but cloud embedding unavailable (%v); recall runs keyword-only", err)
		return nil
	}
	if cloud == nil {
		log.Printf("axon-mcp: semantic mode but no embedding provider configured; recall runs keyword-only")
		return nil
	}
	usable, probed, perr := probe.Usable(ctx, cloud, key)
	if !usable {
		if probed {
			log.Printf("axon-mcp: semantic mode but cloud embedding not usable (%v); recall runs keyword-only", perr)
		}
		return nil
	}
	return cloud
}

// buildCloudEmbedder resolves the configured cloud embedder without any probe or
// local fallback. It returns (nil, "", nil) when no cloud embedding is
// configured. The returned key identifies the endpoint for probe caching (it
// carries the key reference, never the secret itself).
func buildCloudEmbedder(cfg *config.Manager, secrets secret.Store) (embed.Embedder, string, error) {
	c := cfg.Get()

	var pc provider.Config
	model := c.EmbeddingModel

	if c.EmbeddingProvider != "" {
		found, ok := cfg.Provider(c.EmbeddingProvider)
		if !ok {
			return nil, "", fmt.Errorf("configured embedding provider %q not found", c.EmbeddingProvider)
		}
		if found.Protocol != "openai" {
			return nil, "", fmt.Errorf("embedding provider %q must use the openai protocol", c.EmbeddingProvider)
		}
		pc = found
	} else {
		name, ok := providerForProtocol(cfg, "openai")
		if !ok {
			return nil, "", nil // no cloud embedding configured
		}
		pc, _ = cfg.Provider(name)
	}

	key, err := secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, "", fmt.Errorf("resolve embedding api key: %w", err)
	}
	return embed.NewOpenAI(pc.BaseURL, key, model), embed.ProbeKey(pc.BaseURL, model, pc.KeyRef), nil
}

// providerForProtocol returns the name of a configured provider whose protocol
// matches, preferring the default provider when it qualifies. Standalone twin of
// App.providerForProtocol.
func providerForProtocol(cfg *config.Manager, protocol string) (string, bool) {
	c := cfg.Get()
	if c.DefaultProvider != "" {
		if pc, ok := cfg.Provider(c.DefaultProvider); ok && pc.Protocol == protocol {
			return pc.Name, true
		}
	}
	for _, pc := range c.Providers {
		if pc.Protocol == protocol {
			return pc.Name, true
		}
	}
	return "", false
}
