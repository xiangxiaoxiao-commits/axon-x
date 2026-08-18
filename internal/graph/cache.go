package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CacheSchema is the current on-disk SessionCache layout version. Bump it when a
// new field must be back-filled into already-cached sessions. LoadCache treats a
// cache whose Schema is older than this as stale, forcing IndexProject to
// reprocess it (which, for the chunk field, only re-runs local chunking + embed,
// never the LLM — see IndexProject).
const CacheSchema = 2

// Chunk is one recall unit of verbatim source text plus its vector. It is the
// core store of the "raw context" channel: unlike distilled entities (a lossy
// summary), a Chunk carries the original words so the model can read details and
// reasoning first-hand.
type Chunk struct {
	ID        string    `json:"id"`             // "<sessionID>#<seq>" / "code:<path>#<seq>" / "task:<id>#<seq>"
	Text      string    `json:"text"`           // verbatim source fragment (conversation turn / code / task output)
	Source    string    `json:"source"`         // provenance tag: sessionID / "code:<path>" / "task:<id>"
	Kind      string    `json:"kind,omitempty"` // "chat" | "code" | "task"
	Embedding []float32 `json:"embedding,omitempty"`
}

// SessionCache is the distilled knowledge of one session, cached so focus/full
// builds can be assembled locally without re-calling the model. Mtime guards
// staleness: if the session file changed, the cache is re-extracted.
type SessionCache struct {
	SessionID string     `json:"sessionId"`
	Mtime     int64      `json:"mtime"`            // source .jsonl mtime, unix millis
	Schema    int        `json:"schema,omitempty"` // cache layout version; 0 on pre-chunk caches
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
	Chunks    []Chunk    `json:"chunks,omitempty"` // verbatim source blocks for the raw-context channel
}

// cacheDir is <dataDir>/graphcache/<slug>.
func cacheDir(dataDir, slug string) string {
	return filepath.Join(dataDir, "graphcache", filepath.Base(slug))
}

// LoadCache returns the cache for a session, or (nil, false) if absent/stale.
// A cache is stale when the source .jsonl changed (mtime mismatch) OR its Schema
// is older than CacheSchema — the latter forces a rebuild so new fields (e.g.
// Chunks) get back-filled into caches written by an older version.
func LoadCache(dataDir, slug, sessionID string, mtime int64) (*SessionCache, bool) {
	c, ok := LoadCacheRaw(dataDir, slug, sessionID)
	if !ok {
		return nil, false
	}
	if c.Mtime != mtime {
		return nil, false // source changed since caching
	}
	if c.Schema < CacheSchema {
		return nil, false // older layout: force reprocess to back-fill new fields
	}
	return c, true
}

// LoadCacheRaw returns the cached session as-is (ignoring freshness), or
// (nil, false) if absent/unreadable. Used by incremental back-fill: even when a
// cache is stale only because of a Schema bump, its already-distilled entities
// can be reused so only the new (cheap, LLM-free) fields are recomputed.
func LoadCacheRaw(dataDir, slug, sessionID string) (*SessionCache, bool) {
	p := filepath.Join(cacheDir(dataDir, slug), sanitizeFileName(sessionID)+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var c SessionCache
	if json.Unmarshal(data, &c) != nil {
		return nil, false
	}
	return &c, true
}

// SaveCache writes a session's distilled cache atomically (temp file + rename)
// so a crash mid-write can't leave a half-written, unparseable cache — important
// now that caches carry large chunk vectors.
func SaveCache(dataDir, slug string, c *SessionCache) error {
	dir := cacheDir(dataDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, sanitizeFileName(c.SessionID)+".json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return os.Rename(tmp, p)
}

// sanitizeFileName replaces characters that are illegal in Windows file names
// (: < > | " ? * \) with underscores. Colons are the most common offender:
// session IDs like "mcp:1234-abcd" or "codex:uuid" contain them, and Windows
// NTFS interprets ':' as an Alternate Data Stream separator.
func sanitizeFileName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch r {
		case ':', '<', '>', '|', '"', '?', '*', '\\':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LoadAllCache returns every cached session for a project.
func LoadAllCache(dataDir, slug string) ([]SessionCache, error) {
	dir := cacheDir(dataDir, slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionCache{}, nil
		}
		return nil, err
	}
	out := make([]SessionCache, 0, len(entries))
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c SessionCache
		if json.Unmarshal(data, &c) == nil {
			out = append(out, c)
		}
	}
	return out, nil
}
