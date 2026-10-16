package main

import (
	"reflect"
	"testing"

	"axon/internal/graph"
)

// writeClusters caches a grouping the way the app does at startup.
func writeClusters(t *testing.T, dir string, clusters []graph.NamespaceCluster, links []graph.ClusterLink) {
	t.Helper()
	graph.SaveClusters(dir, clusters, links)
}

// A query naming one namespace must also see the knowledge filed under another
// holding the same subject matter. Without this, a project split across two
// namespaces answers only from whichever half was named.
func TestExpandToDomainsPullsInSiblingNamespaces(t *testing.T) {
	dir := t.TempDir()
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
		{Name: "other", Slugs: []string{"other"}},
	}, nil)
	h := &toolHandler{dataDir: dir}

	got := h.expandToDomains([]string{"proj"})
	want := []string{"proj", "proj-copy"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandToDomains(proj) = %v, want %v", got, want)
	}

	// Naming the smaller member works identically — the user should not have to
	// know which of the two is canonical.
	got = h.expandToDomains([]string{"proj-copy"})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expandToDomains(proj-copy) = %v, want %v", got, want)
	}
}

// A namespace with no siblings is returned as-is, and an unknown slug is not
// lost.
func TestExpandToDomainsPassesThroughUnknownAndSolo(t *testing.T) {
	dir := t.TempDir()
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "solo", Slugs: []string{"solo"}},
	}, nil)
	h := &toolHandler{dataDir: dir}

	if got := h.expandToDomains([]string{"solo"}); !reflect.DeepEqual(got, []string{"solo"}) {
		t.Errorf("solo = %v, want [solo]", got)
	}
	if got := h.expandToDomains([]string{"ghost"}); !reflect.DeepEqual(got, []string{"ghost"}) {
		t.Errorf("unknown slug = %v, want it preserved", got)
	}
}

// Overlapping selections must not produce duplicate namespaces, or the same
// knowledge would be scored twice in one result.
func TestExpandToDomainsDeduplicates(t *testing.T) {
	dir := t.TempDir()
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
	}, nil)
	h := &toolHandler{dataDir: dir}

	got := h.expandToDomains([]string{"proj", "proj-copy"})
	if !reflect.DeepEqual(got, []string{"proj", "proj-copy"}) {
		t.Errorf("expandToDomains(proj, proj-copy) = %v, want no duplicates", got)
	}
}

// With no cached grouping the selection passes through untouched: clustering
// reads every cache, and a lookup must not pay that cost.
func TestExpandToDomainsWithoutCacheIsIdentity(t *testing.T) {
	h := &toolHandler{dataDir: t.TempDir()}
	in := []string{"a", "b"}
	if got := h.expandToDomains(in); !reflect.DeepEqual(got, in) {
		t.Errorf("expandToDomains with no cache = %v, want %v", got, in)
	}
}

// resolveSearchSlugs is the entry point that has to apply the widening, for both
// an explicit project and an active scope.
func TestResolveSearchSlugsExpandsExplicitProjectAndScope(t *testing.T) {
	dir := t.TempDir()
	writeClusters(t, dir, []graph.NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
	}, nil)

	h := &toolHandler{dataDir: dir}
	if got := h.resolveSearchSlugs("proj"); !reflect.DeepEqual(got, []string{"proj", "proj-copy"}) {
		t.Errorf("explicit project = %v, want the whole domain", got)
	}

	// A scope expresses "work within this body of knowledge", so it widens the
	// same way — the sibling namespace holds that same body.
	h.activeScope = []string{"proj"}
	if got := h.resolveSearchSlugs(""); !reflect.DeepEqual(got, []string{"proj", "proj-copy"}) {
		t.Errorf("active scope = %v, want the whole domain", got)
	}
}
