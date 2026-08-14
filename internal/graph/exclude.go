package graph

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Exclusions is the per-project set of observations the user has chosen to keep
// OUT of the assembled graph. It survives re-indexing: assembleGraph applies it
// after merging caches, so a deleted fact never comes back even though the
// source SessionCache still contains it.
//
// Keying is by content (entity name + observation text), not by index or
// session, so "I don't want this fact" holds globally — if several sessions
// distilled the same fact, excluding it once removes it everywhere.
type Exclusions struct {
	Obs map[string]bool `json:"obs"` // set of ObsKey -> true
}

// ObsKey is the stable identity of an observation: sha1 of the normalized
// entity name + the trimmed observation text.
func ObsKey(entityName, obsText string) string {
	h := sha1.Sum([]byte(normKey(entityName) + "\x00" + strings.TrimSpace(obsText)))
	return hex.EncodeToString(h[:])
}

// exclusionsPath stores the list OUTSIDE the cache dir (which LoadAllCache scans
// for *.json and would try to parse as a SessionCache). It lives beside the
// slug's cache dir as "<slug>.exclusions.json" under graphcache/.
func exclusionsPath(dataDir, slug string) string {
	return filepath.Join(dataDir, "graphcache", filepath.Base(slug)+".exclusions.json")
}

// LoadExclusions returns the project's exclusion set (empty, never nil, if none).
func LoadExclusions(dataDir, slug string) (*Exclusions, error) {
	ex := &Exclusions{Obs: map[string]bool{}}
	data, err := os.ReadFile(exclusionsPath(dataDir, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return ex, nil
		}
		return ex, err
	}
	_ = json.Unmarshal(data, ex)
	if ex.Obs == nil {
		ex.Obs = map[string]bool{}
	}
	return ex, nil
}

// SaveExclusions atomically writes the exclusion set (temp file + rename).
func SaveExclusions(dataDir, slug string, ex *Exclusions) error {
	p := exclusionsPath(dataDir, slug)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// FilterExcluded removes every excluded observation (and its parallel source)
// from g in place. An entity left with zero observations is dropped entirely,
// along with any relation that referenced it.
func FilterExcluded(g *Graph, ex *Exclusions) {
	if ex == nil || len(ex.Obs) == 0 {
		return
	}
	keptEnts := g.Entities[:0]
	dropped := map[string]bool{} // normKey of entities removed for being empty
	for _, e := range g.Entities {
		obs := e.Observations[:0]
		var src []string
		for i, o := range e.Observations {
			if ex.Obs[ObsKey(e.Name, o)] {
				continue // excluded
			}
			obs = append(obs, o)
			if s := obsSourceAt(e.ObsSources, i); s != "" || len(e.ObsSources) > 0 {
				src = append(src, s)
			}
		}
		e.Observations = obs
		e.ObsSources = src
		if len(e.Observations) == 0 {
			dropped[normKey(e.Name)] = true
			continue
		}
		keptEnts = append(keptEnts, e)
	}
	g.Entities = keptEnts

	if len(dropped) > 0 {
		rels := g.Relations[:0]
		for _, r := range g.Relations {
			if dropped[normKey(r.From)] || dropped[normKey(r.To)] {
				continue
			}
			rels = append(rels, r)
		}
		g.Relations = rels
	}
}

// obsSourceAt safely reads ObsSources[i] (empty when shorter/absent).
func obsSourceAt(src []string, i int) string {
	if i < len(src) {
		return src[i]
	}
	return ""
}
