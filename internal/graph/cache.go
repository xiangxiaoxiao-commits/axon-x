package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SessionCache is the distilled knowledge of one session, cached so focus/full
// builds can be assembled locally without re-calling the model. Mtime guards
// staleness: if the session file changed, the cache is re-extracted.
type SessionCache struct {
	SessionID string     `json:"sessionId"`
	Mtime     int64      `json:"mtime"` // source .jsonl mtime, unix millis
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

// cacheDir is <dataDir>/graphcache/<slug>.
func cacheDir(dataDir, slug string) string {
	return filepath.Join(dataDir, "graphcache", filepath.Base(slug))
}

// LoadCache returns the cache for a session, or (nil, false) if absent/stale.
func LoadCache(dataDir, slug, sessionID string, mtime int64) (*SessionCache, bool) {
	p := filepath.Join(cacheDir(dataDir, slug), sessionID+".json")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	var c SessionCache
	if json.Unmarshal(data, &c) != nil {
		return nil, false
	}
	if c.Mtime != mtime {
		return nil, false // source changed since caching
	}
	return &c, true
}

// SaveCache writes a session's distilled cache.
func SaveCache(dataDir, slug string, c *SessionCache) error {
	dir := cacheDir(dataDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dir, c.SessionID+".json")
	return os.WriteFile(p, data, 0o644)
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
