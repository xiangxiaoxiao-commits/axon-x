package main

import (
	"fmt"
	"strings"

	"axon/internal/db"
	"axon/internal/graph"
)

// Manual knowledge-graph editing: let a user correct and denoise a project's
// graph directly from the knowledge view (delete entities, edit facts, remove
// relations). Every operation is a load→modify→save cycle guarded by graphMu so
// concurrent edits stay consistent. Persistence itself is atomic (temp file +
// rename) at the graph package level.

// manualSource marks an observation whose text was created or edited by hand, so
// its provenance is "manual" rather than a session/code source.
const manualSource = "manual"

// DeleteEntity removes an entity node and every relation that references it
// (as source or target). Matching is case-insensitive on the trimmed name, the
// same normalization the graph uses elsewhere. Deleting a non-existent entity is
// a no-op error so the UI can surface a stale-state hint.
func (a *App) DeleteEntity(projectSlug, entityName string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	key := normName(entityName)
	if key == "" {
		return fmt.Errorf("实体名不能为空")
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	g, err := graph.Load(dataDir, projectSlug)
	if err != nil {
		return err
	}
	kept := g.Entities[:0]
	found := false
	for _, e := range g.Entities {
		if normName(e.Name) == key {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return fmt.Errorf("未找到实体「%s」", entityName)
	}
	g.Entities = kept

	rels := g.Relations[:0]
	for _, r := range g.Relations {
		if normName(r.From) == key || normName(r.To) == key {
			continue
		}
		rels = append(rels, r)
	}
	g.Relations = rels

	return graph.Save(dataDir, g)
}

// UpdateEntityObservations replaces an entity's observations with the given
// list (the user having edited, deleted or added facts). ObsSources is kept in
// lockstep: an observation whose text is unchanged keeps its original source;
// any new or edited text is marked "manual" so provenance stays honest. Blank
// observations are dropped.
func (a *App) UpdateEntityObservations(projectSlug, entityName string, observations []string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	key := normName(entityName)
	if key == "" {
		return fmt.Errorf("实体名不能为空")
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	g, err := graph.Load(dataDir, projectSlug)
	if err != nil {
		return err
	}
	idx := -1
	for i := range g.Entities {
		if normName(g.Entities[i].Name) == key {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("未找到实体「%s」", entityName)
	}
	e := &g.Entities[idx]

	// Map each existing observation to its source so unchanged facts keep their
	// provenance; edited/new facts fall back to "manual".
	prevSrc := make(map[string]string, len(e.Observations))
	for i, o := range e.Observations {
		prevSrc[o] = obsSourceAt(e.ObsSources, i)
	}

	newObs := make([]string, 0, len(observations))
	newSrc := make([]string, 0, len(observations))
	for _, o := range observations {
		o = strings.TrimSpace(o)
		if o == "" {
			continue // drop blanks the user cleared out
		}
		src := manualSource
		if s, ok := prevSrc[o]; ok {
			src = s // unchanged text keeps its original provenance
		}
		newObs = append(newObs, o)
		newSrc = append(newSrc, src)
	}
	e.Observations = newObs
	e.ObsSources = newSrc

	return graph.Save(dataDir, g)
}

// RenameEntity changes an entity's name and updates every relation endpoint that
// pointed at the old name, so edges stay connected. The old name is added to the
// entity's aliases so semantic/substring lookups on it still resolve. Errors if
// the old name is missing or the new name collides with a different entity.
func (a *App) RenameEntity(projectSlug, oldName, newName string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	oldKey, newKey := normName(oldName), normName(newName)
	if oldKey == "" || newKey == "" {
		return fmt.Errorf("实体名不能为空")
	}
	newName = strings.TrimSpace(newName)

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	g, err := graph.Load(dataDir, projectSlug)
	if err != nil {
		return err
	}
	idx := -1
	for i := range g.Entities {
		n := normName(g.Entities[i].Name)
		if n == oldKey {
			idx = i
		} else if n == newKey && newKey != oldKey {
			return fmt.Errorf("已存在实体「%s」，不能重名", newName)
		}
	}
	if idx < 0 {
		return fmt.Errorf("未找到实体「%s」", oldName)
	}

	e := &g.Entities[idx]
	if newKey != oldKey {
		// Preserve the old name as an alias so existing references still match.
		if !hasAlias(e.Aliases, oldKey) && normName(e.Name) != newKey {
			e.Aliases = append(e.Aliases, e.Name)
		}
	}
	e.Name = newName

	for i := range g.Relations {
		if normName(g.Relations[i].From) == oldKey {
			g.Relations[i].From = newName
		}
		if normName(g.Relations[i].To) == oldKey {
			g.Relations[i].To = newName
		}
	}

	return graph.Save(dataDir, g)
}

// DeleteRelation removes a single directed relation matching (from, to, label),
// all compared case-insensitively. A no-op error is returned when no such edge
// exists, so the UI can reflect stale state.
func (a *App) DeleteRelation(projectSlug, from, to, label string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	fk, tk, lk := normName(from), normName(to), normName(label)

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	g, err := graph.Load(dataDir, projectSlug)
	if err != nil {
		return err
	}
	rels := g.Relations[:0]
	found := false
	for _, r := range g.Relations {
		if !found && normName(r.From) == fk && normName(r.To) == tk && normName(r.Label) == lk {
			found = true
			continue // drop the first exact match
		}
		rels = append(rels, r)
	}
	if !found {
		return fmt.Errorf("未找到关系「%s —%s→ %s」", from, label, to)
	}
	g.Relations = rels

	return graph.Save(dataDir, g)
}

// normName lowercases and trims a name for case-insensitive matching, mirroring
// the graph package's internal normalization.
func normName(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// hasAlias reports whether aliases already contains a name normalizing to key.
func hasAlias(aliases []string, key string) bool {
	for _, a := range aliases {
		if normName(a) == key {
			return true
		}
	}
	return false
}
