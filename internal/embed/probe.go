package embed

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// probeTimeout bounds a single cloud availability probe. Kept short so a dead or
// embedding-incapable endpoint can't stall recall for long before falling back.
const probeTimeout = 8 * time.Second

// probeText is the tiny input sent during a probe. Its content is irrelevant;
// we only care whether the endpoint returns a non-empty vector.
const probeText = "ping"

// Probe issues one lightweight embed call to verify a cloud embedder is actually
// usable, not merely constructable. Some OpenAI-compatible gateways serve chat
// but return 5xx on /v1/embeddings; without this check they would silently
// produce empty query vectors and break semantic/chunk recall. Probe derives its
// own short timeout from ctx and returns an error when the endpoint cannot embed.
func Probe(ctx context.Context, e Embedder) error {
	if ctx == nil {
		ctx = context.Background()
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	v, err := e.Embed(pctx, probeText)
	if err != nil {
		return err
	}
	if len(v) == 0 {
		return fmt.Errorf("embed probe: endpoint returned an empty vector")
	}
	return nil
}

// ProbeKey builds a stable cache key for an endpoint from its identity. It uses
// the key reference (not the secret) so no credential ever lands in a map key.
func ProbeKey(baseURL, model, keyRef string) string {
	return baseURL + "\x00" + model + "\x00" + keyRef
}

// ProbeCache memoizes cloud-embedder availability probes within one process so
// recall paths don't re-probe the network on every call. It is keyed by endpoint
// identity (see ProbeKey) so reconfiguration re-probes a new endpoint. Safe for
// concurrent use.
type ProbeCache struct {
	mu   sync.Mutex
	seen map[string]bool
}

// NewProbeCache builds an empty cache.
func NewProbeCache() *ProbeCache {
	return &ProbeCache{seen: make(map[string]bool)}
}

// Usable reports whether the cloud embedder identified by key can embed. On the
// first call for a key it runs a live Probe and caches the outcome; later calls
// return the cached result without touching the network. probed is true only on
// the call that actually ran the probe, so callers can log a fallback exactly
// once; err carries that probe's failure (if any).
func (c *ProbeCache) Usable(ctx context.Context, e Embedder, key string) (usable, probed bool, err error) {
	c.mu.Lock()
	if c.seen == nil {
		c.seen = make(map[string]bool)
	}
	if v, ok := c.seen[key]; ok {
		c.mu.Unlock()
		return v, false, nil
	}
	c.mu.Unlock()

	// Probe outside the lock: a network call must not block other lookups. A rare
	// concurrent double-probe of the same new key is harmless (idempotent).
	err = Probe(ctx, e)
	ok := err == nil

	c.mu.Lock()
	c.seen[key] = ok
	c.mu.Unlock()
	return ok, true, err
}
