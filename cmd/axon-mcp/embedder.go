package main

import (
	"fmt"

	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/provider"
	"axon/internal/secret"
)

// newEmbedder builds an embedder without any Wails App, mirroring the App's
// degradation chain: cloud (OpenAI-compatible) embedding is preferred, but when
// nothing is configured it falls back to the pure-Go LocalEmbedder so semantic
// recall still works offline (fuzzy lexical vectors) instead of collapsing to
// exact substring matching.
//
// Selection prefers the explicit embedding config (EmbeddingProvider +
// EmbeddingModel). A configured-but-broken provider still errors so real
// misconfiguration surfaces; only the "nothing configured" case goes local.
func newEmbedder(cfg *config.Manager, secrets secret.Store) (embed.Embedder, error) {
	c := cfg.Get()

	var pc provider.Config
	model := c.EmbeddingModel

	if c.EmbeddingProvider != "" {
		found, ok := cfg.Provider(c.EmbeddingProvider)
		if !ok {
			return nil, fmt.Errorf("configured embedding provider %q not found", c.EmbeddingProvider)
		}
		if found.Protocol != "openai" {
			return nil, fmt.Errorf("embedding provider %q must use the openai protocol", c.EmbeddingProvider)
		}
		pc = found
	} else {
		name, ok := providerForProtocol(cfg, "openai")
		if !ok {
			// No cloud embedding configured: use the local, zero-dependency embedder.
			return embed.NewLocal(), nil
		}
		pc, _ = cfg.Provider(name)
	}

	key, err := secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, fmt.Errorf("resolve embedding api key: %w", err)
	}
	return embed.NewOpenAI(pc.BaseURL, key, model), nil
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
