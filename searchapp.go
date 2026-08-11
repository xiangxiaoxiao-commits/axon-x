package main

import (
	"fmt"

	"axon/internal/claudedata"
	"axon/internal/db"
	"axon/internal/search"
)

// Search events for the frontend.
const (
	EventSearchProgress = "search:progress"
	EventSearchDone     = "search:done"
)

// IndexSearch builds the keyword index over all projects' sessions. Skips
// already-indexed sessions, so re-running is cheap. Emits progress.
func (a *App) IndexSearch() error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	ix, err := search.Open(dataDir)
	if err != nil {
		return err
	}
	defer ix.Close()

	projects, err := claudedata.ListProjects()
	if err != nil {
		return err
	}
	indexed := 0
	for _, p := range projects {
		sessions, err := claudedata.ListSessions(p.Slug)
		if err != nil {
			continue
		}
		for _, s := range sessions {
			if ix.HasSession(s.ID) {
				continue
			}
			msgs, err := claudedata.ReadSession(p.Slug, s.ID)
			if err != nil {
				continue
			}
			for _, m := range msgs {
				_ = ix.AddMessage(p.Slug, s.ID, s.Title, m.Role, m.Text, s.UpdatedAt)
			}
			indexed++
			a.emit(EventSearchProgress, map[string]any{"project": p.Path, "indexed": indexed})
		}
	}
	a.emit(EventSearchDone, map[string]any{"indexed": indexed})
	return nil
}

// SearchSessions runs a keyword search. projectSlug may be empty for all.
func (a *App) SearchSessions(keyword, projectSlug string) ([]search.Hit, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	ix, err := search.Open(dataDir)
	if err != nil {
		return nil, err
	}
	defer ix.Close()
	hits, err := ix.Query(keyword, projectSlug, 60)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	return hits, nil
}
