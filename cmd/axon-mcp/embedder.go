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

// newEmbedder builds the embedder for the recall path, mirroring the App's
// degradation chain. It NEVER returns an error: a cloud (OpenAI-compatible)
// endpoint is preferred, but a missing, misconfigured, or embedding-incapable
// endpoint (e.g. a gateway that serves chat but 503s on /v1/embeddings) falls
// back to the pure-Go LocalEmbedder so semantic/chunk recall keeps working
// instead of producing empty query vectors.
//
// Cloud usability is verified with a live probe the first time an endpoint is
// seen; the result is cached in probe so later recalls don't re-probe. probe
// must be non-nil (see main / embed.NewProbeCache).
func newEmbedder(ctx context.Context, cfg *config.Manager, secrets secret.Store, probe *embed.ProbeCache) embed.Embedder {
	cloud, key, err := buildCloudEmbedder(cfg, secrets)
	if err != nil {
		log.Printf("axon-mcp: cloud embedding unavailable (%v); falling back to local embedder", err)
		return embed.NewLocal()
	}
	if cloud == nil {
		return embed.NewLocal()
	}

	usable, probed, perr := probe.Usable(ctx, cloud, key)
	if !usable {
		if probed {
			log.Printf("axon-mcp: cloud embedding not usable (%v); falling back to local embedder", perr)
		}
		return embed.NewLocal()
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
