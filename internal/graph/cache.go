package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheSchema is the current on-disk SessionCache layout version. Bump it when a
// new field must be back-filled into already-cached sessions. LoadCache treats a
// cache whose Schema is older than this as stale, forcing IndexProject to
// reprocess it (which, for the chunk field, only re-runs local chunking + embed,
// never the LLM — see IndexProject).
//
// Bumping this also lets MigrateStaleCaches reach caches IndexProject can no
// longer see: a session whose source file was pruned still contributes to every
// assembled graph, so a version bump must be honoured by the cache itself, not
// only by the sessions an indexing run happens to rediscover.
//
// The steps most likely to need a bump are changes to what a cache carries
// rather than to its shape: relation provenance, schema-folded labels, and
// source-graded trust are the current examples, since a cache written before
// them holds a label the vocabulary would fold and a trust grade that ignored
// the knowledge's origin.
const CacheSchema = 6

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

// saveCacheFile writes a cache to an explicit path, atomically (temp file +
// rename) so a crash mid-write can't leave a half-written, unparseable cache —
// important now that caches carry large chunk vectors.
//
// Callers that already know the file a cache came from must use this instead of
// SaveCache: SaveCache derives the name from SessionID through sanitizeFileName,
// and for an id containing a character that sanitizing rewrites ("mcp:1234"
// becomes "mcp_1234") that lands on a *different* file — leaving the original in
// place and adding a second copy of the same knowledge.
func saveCacheFile(path string, c *SessionCache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return os.Rename(tmp, path)
}

// SaveCache writes a session's distilled cache under its canonical file name.
func SaveCache(dataDir, slug string, c *SessionCache) error {
	return saveCacheFile(filepath.Join(cacheDir(dataDir, slug), sanitizeFileName(c.SessionID)+".json"), c)
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

// CacheNamespaces lists the namespace slugs that have a cache directory. Unlike
// the App's ListNamespaces this reads the filesystem only, so it also sees a
// namespace whose graph.json has not been written yet.
func CacheNamespaces(dataDir string) ([]string, error) {
	root := filepath.Join(dataDir, "graphcache")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// MigrateStaleCaches upgrades every cached session whose Schema predates
// CacheSchema — including caches whose source session no longer exists on disk —
// and reports how many it upgraded.
//
// This closes a gap that IndexProject structurally cannot. IndexProject only
// revisits a cache it rediscovers by scanning the agent's session files, so once
// a session file is pruned (Claude Code rotating old sessions, or the user moving
// to another machine) its cache becomes an orphan: LoadAllCache keeps feeding it
// into every assembled graph, but no indexing run will ever touch it again. A
// schema bump therefore reaches live sessions only, and the orphaned majority
// stays sourceless and un-folded forever.
//
// The work is deliberately the same LLM-free back-fill IndexProject performs on a
// schema-only bump: stamp entity assertions, stamp relation provenance, fold
// labels, re-save at the current schema. No network, no re-distillation, no
// knowledge change — only the metadata the newer schema expects.
func MigrateStaleCaches(dataDir, slug string, now time.Time) (int, error) {
	dir := cacheDir(dataDir, slug)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	upgraded := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c SessionCache
		if json.Unmarshal(raw, &c) != nil {
			continue
		}
		if c.Schema >= CacheSchema {
			continue
		}
		c.Schema = CacheSchema
		// Entity assertions stay asserted across every origin: a code cache's
		// entity descriptions mix the static skeleton with optional LLM business
		// enrichment (see BuildGraphFromCode), so they are not uniformly
		// machine-checked facts the way its relation edges are.
		StampEntityAssertions(c.Entities, c.SessionID, now.UnixMilli(), RelationAsserted, 0.85)
		// Relations are graded by origin, so code-derived edges reach the verified
		// tier their source actually earns.
		if state, confidence, ok := RelationTrustForCache(c.SessionID); ok {
			StampRelationTrust(c.Relations, state, confidence)
		}
		StampRelationProvenance(c.Relations, c.SessionID)
		// Write back to the file we read, never to SaveCache's derived name: an
		// id like "mcp:1234" sanitizes to a different file name, which would
		// leave this stale original in place and add a duplicate of its
		// knowledge to every later assembly.
		if err := saveCacheFile(filepath.Join(dir, e.Name()), &c); err != nil {
			continue
		}
		upgraded++
	}
	return upgraded, nil
}

// MigrateAllStaleCaches runs MigrateStaleCaches over every namespace. It is
// best-effort per namespace: one unreadable directory never aborts the rest.
func MigrateAllStaleCaches(dataDir string, now time.Time) (int, map[string]int) {
	perNamespace := map[string]int{}
	slugs, err := CacheNamespaces(dataDir)
	if err != nil {
		return 0, perNamespace
	}
	total := 0
	for _, slug := range slugs {
		n, err := MigrateStaleCaches(dataDir, slug, now)
		if err != nil || n == 0 {
			continue
		}
		perNamespace[slug] = n
		total += n
	}
	return total, perNamespace
}
