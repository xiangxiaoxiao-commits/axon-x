package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axon/internal/db"
	"axon/internal/graph"
)

// Manual knowledge-graph editing: let a user correct and denoise a project's
// graph directly from the knowledge view (delete entities, edit facts, remove
// relations, or manually add an entity/fact/relation). Every operation is a
// load→modify→save cycle guarded by graphMu so concurrent edits stay consistent.
//
// Multi-namespace domain awareness:
// When a project's knowledge spans several auto-merged namespaces (or the UI
// passes a "+"-joined slug list like "wanlian+Wanlian1"), edits automatically
// locate the namespace(s) that hold the target entity or relation instead of
// failing or requiring the user to switch out of the merged domain view.

// manualSource marks an observation whose text was created or edited by hand, so
// its provenance is "manual" rather than a session/code source.
const manualSource = "manual"

// candidateSlugs expands a single slug or "+"-joined slug string into the
// ordered list of candidate namespaces to inspect (including any auto-clustered
// sibling namespaces).
func (a *App) candidateSlugs(projectSlug string) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		s = filepath.Base(s)
		for _, existing := range out {
			if existing == s {
				return
			}
		}
		out = append(out, s)
	}
	for _, part := range strings.Split(projectSlug, "+") {
		add(part)
		if siblings, err := a.NamespacesFor(strings.TrimSpace(part)); err == nil {
			for _, sib := range siblings {
				add(sib)
			}
		}
	}
	return out
}

// resolveEntitySlugs returns the namespace slugs (from candidateSlugs) that
// currently contain entityName (matched by name or alias). Falls back to the
// first candidate slug when not found.
func (a *App) resolveEntitySlugs(dataDir, projectSlug, entityName string) []string {
	candidates := a.candidateSlugs(projectSlug)
	key := normName(entityName)
	if key == "" || len(candidates) == 0 {
		return candidates
	}
	var matched []string
	for _, slug := range candidates {
		g, err := a.assembleGraph(slug, "")
		if err != nil {
			continue
		}
		for _, e := range g.Entities {
			if matchesEntityByNameOrAlias(e, key) {
				matched = append(matched, slug)
				break
			}
		}
	}
	if len(matched) > 0 {
		return matched
	}
	return candidates[:1]
}

// ManualRelationInput describes an optional relation created alongside a
// manually recorded entity in the UI.
type ManualRelationInput struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
}

// ManualEntityInput is the payload for RememberManualEntity from the GUI.
type ManualEntityInput struct {
	Name         string                `json:"name"`
	Type         string                `json:"type"`
	Aliases      []string              `json:"aliases"`
	Observations []string              `json:"observations"`
	Relations    []ManualRelationInput `json:"relations"`
}

// RememberManualEntity writes a user-created entity (with facts, optional
// aliases, and optional relations) into the target namespace's manual cache and
// immediately rebuilds the assembled graph.
func (a *App) RememberManualEntity(projectSlug string, input ManualEntityInput) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	candidates := a.candidateSlugs(projectSlug)
	if len(candidates) == 0 {
		return fmt.Errorf("请先选择一个项目或知识域")
	}
	targetSlug := candidates[0]

	name := strings.TrimSpace(input.Name)
	if name == "" {
		return fmt.Errorf("实体名称不能为空")
	}
	typ := strings.TrimSpace(input.Type)
	if typ == "" {
		typ = "concept"
	}

	var obs []string
	seenObs := make(map[string]bool)
	for _, o := range input.Observations {
		o = strings.TrimSpace(o)
		if o == "" || seenObs[o] {
			continue
		}
		seenObs[o] = true
		obs = append(obs, o)
	}
	if len(obs) == 0 {
		return fmt.Errorf("请至少填写一条事实（Observation）")
	}

	var aliases []string
	seenAlias := make(map[string]bool)
	for _, al := range input.Aliases {
		al = strings.TrimSpace(al)
		if al == "" || normName(al) == normName(name) || seenAlias[normName(al)] {
			continue
		}
		seenAlias[normName(al)] = true
		aliases = append(aliases, al)
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	now := time.Now().UnixMilli()
	sources := make([]string, len(obs))
	for i := range sources {
		sources[i] = manualSource
	}
	ent := graph.Entity{
		Name:         name,
		Type:         typ,
		Aliases:      aliases,
		Observations: obs,
		ObsSources:   sources,
	}
	ents := []graph.Entity{ent}
	graph.StampEntityAssertions(ents, manualSource, now, graph.RelationVerified, 1.0)
	if emb, embErr := a.newEmbedder(); embErr == nil && emb != nil {
		a.embedEntities(emb, ents)
	}

	var rels []graph.Relation
	for _, r := range input.Relations {
		from := strings.TrimSpace(r.From)
		to := strings.TrimSpace(r.To)
		label := strings.TrimSpace(r.Label)
		if from == "" {
			from = name
		}
		if to == "" || label == "" {
			continue
		}
		rels = append(rels, graph.NormalizeRelation(graph.Relation{
			From:       from,
			To:         to,
			Label:      label,
			Inferred:   false,
			State:      graph.RelationVerified,
			Confidence: 1.0,
			Sources:    []string{manualSource},
		}))
	}

	// Ensure deleted facts with the same key are un-excluded when explicitly re-added
	if ex, exErr := graph.LoadExclusions(dataDir, targetSlug); exErr == nil {
		changed := false
		for _, o := range obs {
			k := graph.ObsKey(name, o)
			if ex.Obs[k] {
				delete(ex.Obs, k)
				changed = true
			}
		}
		if changed {
			_ = graph.SaveExclusions(dataDir, targetSlug, ex)
		}
	}

	manualID := fmt.Sprintf("manual:add:%d-%s", now, graph.ObsKey(name, obs[0])[:8])
	if err := graph.SaveCache(dataDir, targetSlug, &graph.SessionCache{
		SessionID: manualID,
		Mtime:     now,
		Schema:    graph.CacheSchema,
		Entities:  ents,
		Relations: rels,
	}); err != nil {
		return err
	}
	_, err = a.assembleGraph(targetSlug, "")
	return err
}

// DeleteEntity removes an entity node and every relation that references it
// from all source cache files across the target namespace (and any merged
// sibling namespaces in the selection), then rebuilds their graph.json files.
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

	candidates := a.candidateSlugs(projectSlug)
	removedFrom := 0
	var touchedSlugs []string

	for _, slug := range candidates {
		dir := filepath.Join(dataDir, "graphcache", filepath.Base(slug))
		entries, readDirErr := os.ReadDir(dir)
		if readDirErr != nil {
			continue
		}
		slugRemoved := 0
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			fpath := filepath.Join(dir, entry.Name())
			data, readErr := os.ReadFile(fpath)
			if readErr != nil {
				continue
			}
			var cache graph.SessionCache
			if json.Unmarshal(data, &cache) != nil {
				continue
			}

			origEnts := len(cache.Entities)
			var keptEnts []graph.Entity
			for _, e := range cache.Entities {
				if matchesEntityByNameOrAlias(e, key) {
					continue
				}
				keptEnts = append(keptEnts, e)
			}

			origRels := len(cache.Relations)
			var keptRels []graph.Relation
			for _, r := range cache.Relations {
				if normName(r.From) == key || normName(r.To) == key {
					continue
				}
				keptRels = append(keptRels, r)
			}

			if len(keptEnts) == origEnts && len(keptRels) == origRels {
				continue
			}

			cache.Entities = keptEnts
			cache.Relations = keptRels
			removedFrom++
			slugRemoved++

			if len(cache.Entities) == 0 && len(cache.Relations) == 0 && len(cache.Chunks) == 0 {
				os.Remove(fpath)
			} else {
				out, _ := json.MarshalIndent(cache, "", "  ")
				_ = os.WriteFile(fpath, out, 0o644)
			}
		}
		if slugRemoved > 0 {
			touchedSlugs = append(touchedSlugs, slug)
		}
	}

	if removedFrom == 0 {
		return fmt.Errorf("未找到实体「%s」", entityName)
	}

	for _, slug := range touchedSlugs {
		if _, err := a.assembleGraph(slug, ""); err != nil {
			return err
		}
	}
	return nil
}

// matchesEntityByNameOrAlias checks if an entity matches the target key (by
// name or alias, case-insensitive).
func matchesEntityByNameOrAlias(e graph.Entity, key string) bool {
	if normName(e.Name) == key {
		return true
	}
	for _, al := range e.Aliases {
		if normName(al) == key {
			return true
		}
	}
	return false
}

// UpdateEntityObservations replaces an entity's observations with the given
// list across the namespace(s) where the entity resides.
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

	slugs := a.resolveEntitySlugs(dataDir, projectSlug, entityName)
	if len(slugs) == 0 {
		return fmt.Errorf("未找到实体「%s」", entityName)
	}

	newObs := make([]string, 0, len(observations))
	newSet := make(map[string]bool, len(observations))
	for _, o := range observations {
		o = strings.TrimSpace(o)
		if o == "" || newSet[o] {
			continue // drop blanks the user cleared out
		}
		newSet[o] = true
		newObs = append(newObs, o)
	}

	updatedAny := false
	for i, slug := range slugs {
		g, gErr := a.assembleGraph(slug, "")
		if gErr != nil {
			continue
		}
		idx := -1
		for j := range g.Entities {
			if matchesEntityByNameOrAlias(g.Entities[j], key) {
				idx = j
				break
			}
		}
		if idx < 0 {
			continue
		}
		updatedAny = true
		e := &g.Entities[idx]

		ex, exErr := graph.LoadExclusions(dataDir, slug)
		if exErr != nil {
			return exErr
		}
		for _, old := range e.Observations {
			obsKey := graph.ObsKey(e.Name, old)
			if newSet[old] {
				delete(ex.Obs, obsKey)
			} else {
				ex.Obs[obsKey] = true
			}
		}
		for _, current := range newObs {
			delete(ex.Obs, graph.ObsKey(e.Name, current))
		}
		if err := graph.SaveExclusions(dataDir, slug, ex); err != nil {
			return err
		}

		// Write the manual entity cache into the primary matched namespace
		if i == 0 {
			var manualEntities []graph.Entity
			if len(newObs) > 0 {
				manualEntity := graph.Entity{
					Name:         e.Name,
					Type:         e.Type,
					Aliases:      append([]string(nil), e.Aliases...),
					Observations: newObs,
				}
				manualEntity.ObsSources = make([]string, len(newObs))
				for k := range manualEntity.ObsSources {
					manualEntity.ObsSources[k] = manualSource
				}
				manualEntities = []graph.Entity{manualEntity}
				graph.StampEntityAssertions(manualEntities, manualSource, time.Now().UnixMilli(), graph.RelationVerified, 1)
				if emb, embErr := a.newEmbedder(); embErr == nil && emb != nil {
					a.embedEntities(emb, manualEntities)
				}
			}
			manualID := "manual:entity:" + graph.ObsKey(e.Name, "")[:12]
			if err := graph.SaveCache(dataDir, slug, &graph.SessionCache{
				SessionID: manualID, Mtime: time.Now().UnixMilli(), Schema: graph.CacheSchema,
				Entities: manualEntities, Relations: []graph.Relation{},
			}); err != nil {
				return err
			}
		}
		if _, err := a.assembleGraph(slug, ""); err != nil {
			return err
		}
	}

	if !updatedAny {
		return fmt.Errorf("未找到实体「%s」", entityName)
	}
	return nil
}

// RenameEntity changes an entity's name and updates every relation endpoint that
// pointed at the old name across both graph.json and underlying session caches.
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

	slugs := a.resolveEntitySlugs(dataDir, projectSlug, oldName)
	renamedAny := false

	for _, slug := range slugs {
		g, gErr := graph.Load(dataDir, slug)
		if gErr != nil || len(g.Entities) == 0 {
			g, gErr = a.assembleGraph(slug, "")
			if gErr != nil {
				continue
			}
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
			continue
		}
		renamedAny = true

		e := &g.Entities[idx]
		if newKey != oldKey {
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
		if err := graph.Save(dataDir, g); err != nil {
			return err
		}

		// Also update underlying cache files in graphcache/<slug>/ so re-assembly keeps the new name
		dir := filepath.Join(dataDir, "graphcache", filepath.Base(slug))
		if entries, dErr := os.ReadDir(dir); dErr == nil {
			for _, entry := range entries {
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				fpath := filepath.Join(dir, entry.Name())
				raw, rErr := os.ReadFile(fpath)
				if rErr != nil {
					continue
				}
				var cache graph.SessionCache
				if json.Unmarshal(raw, &cache) != nil {
					continue
				}
				changed := false
				for i := range cache.Entities {
					if normName(cache.Entities[i].Name) == oldKey {
						if newKey != oldKey && !hasAlias(cache.Entities[i].Aliases, oldKey) {
							cache.Entities[i].Aliases = append(cache.Entities[i].Aliases, cache.Entities[i].Name)
						}
						cache.Entities[i].Name = newName
						changed = true
					}
				}
				for i := range cache.Relations {
					if normName(cache.Relations[i].From) == oldKey {
						cache.Relations[i].From = newName
						changed = true
					}
					if normName(cache.Relations[i].To) == oldKey {
						cache.Relations[i].To = newName
						changed = true
					}
				}
				if changed {
					if out, mErr := json.MarshalIndent(cache, "", "  "); mErr == nil {
						_ = os.WriteFile(fpath, out, 0o644)
					}
				}
			}
		}
	}

	if !renamedAny {
		return fmt.Errorf("未找到实体「%s」", oldName)
	}
	return nil
}

// DeleteRelation removes a single directed relation matching (from, to, label),
// all compared case-insensitively.
func (a *App) DeleteRelation(projectSlug, from, to, label string) error {
	return a.ReviewRelation(projectSlug, from, to, label, graph.RelationRejected)
}

// ReviewAssertion persists a human trust decision for one entity fact across
// any candidate namespace holding that fact.
func (a *App) ReviewAssertion(projectSlug, entityName, text, state string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	if normName(entityName) == "" || strings.TrimSpace(text) == "" {
		return fmt.Errorf("实体名和事实不能为空")
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state != graph.RelationVerified && state != graph.RelationCandidate && state != graph.RelationRejected {
		return fmt.Errorf("不支持的事实审核状态 %q", state)
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	candidates := a.candidateSlugs(projectSlug)
	reviewed := false
	for _, slug := range candidates {
		g, gErr := a.assembleGraph(slug, "")
		if gErr != nil {
			continue
		}
		found := false
		canonicalName := entityName
		for _, e := range g.Entities {
			if !matchesEntityByNameOrAlias(e, normName(entityName)) {
				continue
			}
			canonicalName = e.Name
			graph.NormalizeEntityAssertions(&e)
			for _, assertion := range e.Assertions {
				if strings.TrimSpace(assertion.Text) == strings.TrimSpace(text) {
					found = true
					break
				}
			}
		}
		if !found {
			continue
		}
		ex, exErr := graph.LoadExclusions(dataDir, slug)
		if exErr != nil {
			return exErr
		}
		assertionKey := graph.ObsKey(canonicalName, text)
		ex.RecordReview("assertion", assertionKey, fmt.Sprintf("%s：%s", canonicalName, text), state)
		ex.AssertionReviews[assertionKey] = state
		if state == graph.RelationRejected {
			ex.Obs[assertionKey] = true
		} else {
			delete(ex.Obs, assertionKey)
		}
		if err := graph.SaveExclusions(dataDir, slug, ex); err != nil {
			return err
		}
		if _, err := a.assembleGraph(slug, ""); err != nil {
			return err
		}
		reviewed = true
	}

	if !reviewed {
		return fmt.Errorf("未找到实体「%s」的指定事实", entityName)
	}
	return nil
}

// ReviewRelation persists a human decision outside source caches so it survives
// every future graph rebuild across single or merged namespaces.
func (a *App) ReviewRelation(projectSlug, from, to, label, state string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	fk, tk, lk := normName(from), normName(to), normName(label)
	if fk == "" || tk == "" || lk == "" {
		return fmt.Errorf("关系端点和标签不能为空")
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state != graph.RelationVerified && state != graph.RelationCandidate && state != graph.RelationRejected {
		return fmt.Errorf("不支持的关系审核状态 %q", state)
	}

	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	candidates := a.candidateSlugs(projectSlug)
	reviewed := false
	for _, slug := range candidates {
		g, gErr := a.assembleGraph(slug, "")
		if gErr != nil {
			continue
		}
		found := false
		for _, r := range g.Relations {
			if normName(r.From) == fk && normName(r.To) == tk && normName(r.Label) == lk {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		ex, exErr := graph.LoadExclusions(dataDir, slug)
		if exErr != nil {
			return exErr
		}
		relationKey := graph.RelationKey(from, to, label)
		ex.RecordReview("relation", relationKey, fmt.Sprintf("%s —%s→ %s", from, label, to), state)
		ex.RelationReviews[relationKey] = state
		if err := graph.SaveExclusions(dataDir, slug, ex); err != nil {
			return err
		}
		if _, err := a.assembleGraph(slug, ""); err != nil {
			return err
		}
		reviewed = true
	}

	if !reviewed {
		return fmt.Errorf("未找到关系「%s —%s→ %s」", from, label, to)
	}
	return nil
}

// UndoLastGraphReview reverses the latest assertion/relation review while
// retaining the event as an audit record marked undone.
func (a *App) UndoLastGraphReview(projectSlug string) (string, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return "", err
	}
	a.graphMu.Lock()
	defer a.graphMu.Unlock()

	candidates := a.candidateSlugs(projectSlug)
	for _, slug := range candidates {
		ex, exErr := graph.LoadExclusions(dataDir, slug)
		if exErr != nil || ex.UndoableReviews() == 0 {
			continue
		}
		description, ok := ex.UndoLatestReview()
		if !ok {
			continue
		}
		if err := graph.SaveExclusions(dataDir, slug, ex); err != nil {
			return "", err
		}
		if _, err := a.assembleGraph(slug, ""); err != nil {
			return "", err
		}
		return description, nil
	}
	return "", fmt.Errorf("没有可撤销的审核操作")
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

// MoveEntity reclassifies one knowledge card by moving it (and its relations)
// from one namespace to another. The UI uses it to mark a card as cross-project
// knowledge (target _global_) or to pull a misfiled general card back into the
// project — the only reliable correction when automatic scope detection guesses
// wrong.
func (a *App) MoveEntity(projectSlug, entityName, targetNamespace string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	entityName = strings.TrimSpace(entityName)
	targetNamespace = strings.TrimSpace(targetNamespace)
	if entityName == "" || targetNamespace == "" {
		return fmt.Errorf("实体名与目标知识库都必填")
	}

	// A merged domain view resolves to several namespaces; the card lives in
	// exactly one of them, so search every candidate.
	sources := a.resolveEntitySlugs(dataDir, projectSlug, entityName)
	if len(sources) == 0 {
		return fmt.Errorf("没有找到实体「%s」", entityName)
	}

	target := strings.ToLower(entityName)
	var found *graph.Entity
	var movedRels []graph.Relation

	for _, src := range sources {
		if src == targetNamespace {
			continue // already where it should be
		}
		dir := filepath.Join(dataDir, "graphcache", src)
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				continue
			}
			var cache graph.SessionCache
			if json.Unmarshal(data, &cache) != nil {
				continue
			}

			kept := cache.Entities[:0]
			for _, ent := range cache.Entities {
				if entityMatchesName(ent, target) {
					if found == nil {
						copied := ent
						found = &copied
					}
					continue
				}
				kept = append(kept, ent)
			}
			keptRels := cache.Relations[:0]
			for _, r := range cache.Relations {
				if strings.ToLower(r.From) == target || strings.ToLower(r.To) == target {
					movedRels = append(movedRels, r)
					continue
				}
				keptRels = append(keptRels, r)
			}
			cache.Entities = kept
			cache.Relations = keptRels

			if len(cache.Entities) == 0 && len(cache.Relations) == 0 && len(cache.Chunks) == 0 {
				_ = os.Remove(path)
				continue
			}
			out, _ := json.MarshalIndent(cache, "", "  ")
			_ = os.WriteFile(path, out, 0o644)
		}
	}

	if found == nil {
		return fmt.Errorf("实体「%s」已经在目标知识库中，无需移动", entityName)
	}

	// Scope travels with the card: anything moved into _global_ is cross-project
	// knowledge, anything moved out of it becomes project knowledge again.
	if targetNamespace == GlobalNamespace {
		found.Scope = "global"
	} else {
		found.Scope = ""
	}

	toDir := filepath.Join(dataDir, "graphcache", targetNamespace)
	if err := os.MkdirAll(toDir, 0o755); err != nil {
		return fmt.Errorf("创建目标知识库目录: %w", err)
	}
	moveID := fmt.Sprintf("moved-%d", time.Now().UnixMilli())
	payload := &graph.SessionCache{
		SessionID: moveID,
		Mtime:     time.Now().UnixMilli(),
		Schema:    graph.CacheSchema,
		Entities:  []graph.Entity{*found},
		Relations: movedRels,
	}
	out, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(filepath.Join(toDir, moveID+".json"), out, 0o644); err != nil {
		return fmt.Errorf("写入目标知识库: %w", err)
	}
	return nil
}

// entityMatchesName reports whether an entity answers to the given lowercase
// name, including via an alias.
func entityMatchesName(e graph.Entity, lowerName string) bool {
	if strings.ToLower(strings.TrimSpace(e.Name)) == lowerName {
		return true
	}
	for _, al := range e.Aliases {
		if strings.ToLower(strings.TrimSpace(al)) == lowerName {
			return true
		}
	}
	return false
}
