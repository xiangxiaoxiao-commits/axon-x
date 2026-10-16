package main

import (
	"testing"

	"axon/internal/graph"
)

// saveNsCache writes one namespace's cache directly, as indexing would.
func saveNsCache(t *testing.T, dir, slug string, ents map[string][]string) {
	t.Helper()
	entities := make([]graph.Entity, 0, len(ents))
	for name, obs := range ents {
		entities = append(entities, graph.Entity{Name: name, Type: "concept", Observations: obs})
	}
	err := graph.SaveCache(dir, slug, &graph.SessionCache{
		SessionID: "s-" + slug, Mtime: 1, Schema: graph.CacheSchema, Entities: entities,
	})
	if err != nil {
		t.Fatalf("SaveCache(%s): %v", slug, err)
	}
}

// A namespace with no siblings reads exactly as before.
func TestAssembleDomainSingleNamespace(t *testing.T) {
	dir := t.TempDir()
	saveNsCache(t, dir, "solo", map[string][]string{"A": {"only fact"}})
	h := &toolHandler{dataDir: dir}

	g, err := h.assembleDomain("solo")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Entities) != 1 || g.Entities[0].Name != "A" {
		t.Fatalf("entities = %+v, want just A", g.Entities)
	}
}

// A namespace lookup must see the whole domain: get_entity has to be able to show
// a fact that search_knowledge found, even when the two facts were recorded under
// different namespaces for the same work.
func TestAssembleDomainMergesSiblingNamespaces(t *testing.T) {
	dir := t.TempDir()
	saveNsCache(t, dir, "proj", map[string][]string{
		"A": {"recorded in proj"},
	})
	saveNsCache(t, dir, "proj-copy", map[string][]string{
		"A": {"recorded in proj-copy"},
		"B": {"only in the copy"},
	})
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
	}, nil)

	h := &toolHandler{dataDir: dir}
	g, err := h.assembleDomain("proj")
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]graph.Entity{}
	for _, e := range g.Entities {
		byName[e.Name] = e
	}
	if _, ok := byName["B"]; !ok {
		t.Errorf("entity only present in the sibling namespace is missing: %+v", byName)
	}
	// The shared entity carries both namespaces' facts, not just one.
	a := byName["A"]
	if len(a.Assertions) != 2 {
		t.Errorf("A assertions = %d, want both facts merged: %+v", len(a.Assertions), a.Assertions)
	}
	found := map[string]bool{}
	for _, as := range a.Assertions {
		found[as.Text] = true
	}
	for _, want := range []string{"recorded in proj", "recorded in proj-copy"} {
		if !found[want] {
			t.Errorf("fact %q missing after domain merge: %+v", want, found)
		}
	}
}

// Naming either member gives the same view, so the user does not have to know
// which name is the canonical one.
func TestAssembleDomainIsSymmetricAcrossMembers(t *testing.T) {
	dir := t.TempDir()
	saveNsCache(t, dir, "proj", map[string][]string{"A": {"x"}})
	saveNsCache(t, dir, "proj-copy", map[string][]string{"B": {"y"}})
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
	}, nil)
	h := &toolHandler{dataDir: dir}

	for _, slug := range []string{"proj", "proj-copy"} {
		g, err := h.assembleDomain(slug)
		if err != nil {
			t.Fatalf("assembleDomain(%s): %v", slug, err)
		}
		if len(g.Entities) != 2 {
			t.Errorf("assembleDomain(%s) entities = %d, want both", slug, len(g.Entities))
		}
	}
}

// Without a cached grouping the lookup stays on the one namespace rather than
// failing — clustering is an enhancement, not a prerequisite.
func TestAssembleDomainWithoutGroupingUsesTheNamespace(t *testing.T) {
	dir := t.TempDir()
	saveNsCache(t, dir, "proj", map[string][]string{"A": {"x"}})
	h := &toolHandler{dataDir: dir}

	g, err := h.assembleDomain("proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Entities) != 1 {
		t.Errorf("entities = %d, want 1", len(g.Entities))
	}
}
