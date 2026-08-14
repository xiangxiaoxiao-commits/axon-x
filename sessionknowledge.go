package main

import (
	"fmt"

	"axon/internal/db"
	"axon/internal/graph"
)

// Session-provenance surface: show which knowledge a given Claude Code session
// contributed to the project graph, and let the user exclude individual facts
// so they never get merged back in (even across re-indexing).

// DistilledObservation is one fact a session produced, plus whether the user
// has excluded it from the assembled graph.
type DistilledObservation struct {
	Text     string `json:"text"`
	Excluded bool   `json:"excluded"`
}

// DistilledEntity groups a session's facts under the entity they describe.
type DistilledEntity struct {
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Observations []DistilledObservation `json:"observations"`
}

// SessionKnowledge is everything a single session distilled into the graph.
type SessionKnowledge struct {
	SessionID string            `json:"sessionId"`
	Entities  []DistilledEntity `json:"entities"`
	// Indexed reports whether a distilled cache exists for this session at all
	// (false means it hasn't been indexed yet — nothing was summarized).
	Indexed bool `json:"indexed"`
}

// SessionDistilledKnowledge returns the knowledge a session produced, read
// straight from its cache file (graphcache/<slug>/<sessionID>.json), with each
// observation annotated by whether it's currently excluded.
func (a *App) SessionDistilledKnowledge(projectSlug, sessionID string) (SessionKnowledge, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return SessionKnowledge{}, err
	}
	out := SessionKnowledge{SessionID: sessionID, Entities: []DistilledEntity{}}

	cache, ok := graph.LoadCacheRaw(dataDir, projectSlug, sessionID)
	if !ok {
		return out, nil // not indexed yet
	}
	out.Indexed = true

	ex, _ := graph.LoadExclusions(dataDir, projectSlug)
	for _, e := range cache.Entities {
		de := DistilledEntity{Name: e.Name, Type: e.Type, Observations: []DistilledObservation{}}
		for _, o := range e.Observations {
			de.Observations = append(de.Observations, DistilledObservation{
				Text:     o,
				Excluded: ex.Obs[graph.ObsKey(e.Name, o)],
			})
		}
		if len(de.Observations) > 0 {
			out.Entities = append(out.Entities, de)
		}
	}
	return out, nil
}

// ExcludeObservation adds a fact to the project's exclusion list and rebuilds
// the graph so it disappears immediately (and stays gone on future re-index).
func (a *App) ExcludeObservation(projectSlug, entityName, obsText string) error {
	return a.setExcluded(projectSlug, entityName, obsText, true)
}

// UnexcludeObservation removes a fact from the exclusion list and rebuilds, so
// it reappears (from whatever caches still contain it).
func (a *App) UnexcludeObservation(projectSlug, entityName, obsText string) error {
	return a.setExcluded(projectSlug, entityName, obsText, false)
}

func (a *App) setExcluded(projectSlug, entityName, obsText string, excluded bool) error {
	if projectSlug == "" || entityName == "" {
		return fmt.Errorf("projectSlug 和 entityName 不能为空")
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	ex, err := graph.LoadExclusions(dataDir, projectSlug)
	if err != nil {
		return err
	}
	key := graph.ObsKey(entityName, obsText)
	if excluded {
		ex.Obs[key] = true
	} else {
		delete(ex.Obs, key)
	}
	if err := graph.SaveExclusions(dataDir, projectSlug, ex); err != nil {
		return err
	}

	// Rebuild the merged graph so the change is visible right away. assembleGraph
	// reapplies the (now-updated) exclusion list.
	_, err = a.assembleGraph(projectSlug, "")
	return err
}
