package graph

import "testing"

// saveNs writes one cache holding the given entity names into a namespace.
func saveNs(t *testing.T, dir, slug, session string, names ...string) {
	t.Helper()
	ents := make([]Entity, 0, len(names))
	for _, n := range names {
		ents = append(ents, Entity{Name: n, Type: "concept"})
	}
	err := SaveCache(dir, slug, &SessionCache{
		SessionID: session, Mtime: 1, Schema: CacheSchema, Entities: ents,
	})
	if err != nil {
		t.Fatalf("SaveCache(%s): %v", slug, err)
	}
}

func TestClusterMergesNamespacesSharingKnowledge(t *testing.T) {
	dir := t.TempDir()
	// Same body of knowledge filed under two names — the real case: a project
	// indexed for months under "proj" and exported/imported as "proj-copy".
	saveNs(t, dir, "proj", "s1", "A", "B", "C", "D")
	saveNs(t, dir, "proj-copy", "s2", "A", "B", "C", "D")

	clusters, links, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("clusters = %d, want 1 merged domain: %+v", len(clusters), clusters)
	}
	c := clusters[0]
	if len(c.Slugs) != 2 {
		t.Errorf("members = %v, want both namespaces", c.Slugs)
	}
	// The label comes from the larger member; ties break on name.
	if c.Name != "proj" {
		t.Errorf("domain name = %q, want proj", c.Name)
	}
	if c.Entities != 4 {
		t.Errorf("merged entities = %d, want 4 (no duplication)", c.Entities)
	}
	// Every entity exists on both sides, so neither member contributes anything
	// unique: the domain is one body of knowledge stored twice.
	if !c.Redundant || c.SoleSource != 0 {
		t.Errorf("redundancy = %v soleSource=%d, want true/0", c.Redundant, c.SoleSource)
	}
	if len(links) != 1 || !links[0].Merged || links[0].Overlap != 1 {
		t.Errorf("link = %+v, want one merged link at 100%% overlap", links)
	}
}

func TestClusterKeepsUnrelatedNamespacesApart(t *testing.T) {
	dir := t.TempDir()
	saveNs(t, dir, "alpha", "s1", "A", "B", "C", "D", "E", "F", "G", "H")
	// Shares one word out of eight: a coincidence, not shared subject matter.
	saveNs(t, dir, "beta", "s2", "A", "X", "Y", "Z", "P", "Q", "R", "S")

	clusters, links, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 2 {
		t.Fatalf("clusters = %d, want 2 separate domains: %+v", len(clusters), clusters)
	}
	if len(links) != 1 || links[0].Merged {
		t.Fatalf("link = %+v, want one unmerged link", links)
	}
	// 1/8 = 12.5% overlap, below the threshold, and the link records why.
	if links[0].Shared != 1 || links[0].Smaller != 8 {
		t.Errorf("link counters = %+v, want shared 1 of 8", links[0])
	}
}

// Grouping must be transitive: a bundle of a bundle still names the same subject
// matter, even where the two ends share little directly. This is why connected
// components, not a pairwise rule, carry the answer.
func TestClusterGroupingIsTransitive(t *testing.T) {
	dir := t.TempDir()
	// A⊇B⊇C by construction: each is a superset of the next.
	saveNs(t, dir, "big", "s1", "A", "B", "C", "D", "E", "F")
	saveNs(t, dir, "mid", "s2", "A", "B", "C", "D", "E", "F") // 100% of mid
	saveNs(t, dir, "small", "s3", "A", "B", "C")              // 100% of small

	clusters, _, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("clusters = %d, want one domain: %+v", len(clusters), clusters)
	}
	if len(clusters[0].Slugs) != 3 {
		t.Errorf("members = %v, want all three", clusters[0].Slugs)
	}
}

// Overlap is measured against the smaller side, so a namespace that is a genuine
// subset joins its parent even when the sizes differ a lot.
func TestClusterOverlapUsesSmallerSide(t *testing.T) {
	dir := t.TempDir()
	big := []string{}
	for i := 0; i < 100; i++ {
		big = append(big, "E"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	saveNs(t, dir, "full", "s1", big...)
	// A small bundle of the same work: 6 entities, all present in full.
	saveNs(t, dir, "excerpt", "s2", big[0], big[1], big[2], big[3], big[4], big[5])

	clusters, links, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("a subset did not join its parent: %+v (links %+v)", clusters, links)
	}
	// The small side is fully covered, so it is NOT a sole source of knowledge,
	// but the full namespace is.
	if clusters[0].SoleSource != 1 {
		t.Errorf("soleSource = %d, want 1 (only the full namespace is unique)",
			clusters[0].SoleSource)
	}
	if clusters[0].Redundant {
		t.Error("domain reported redundant though one member holds unique knowledge")
	}
}

// A namespace whose knowledge exists nowhere else is not redundant, and must be
// counted — dropping it would lose knowledge.
func TestClusterCountsSoleSourceMembers(t *testing.T) {
	dir := t.TempDir()
	saveNs(t, dir, "one", "s1", "A", "B", "C", "shared")
	saveNs(t, dir, "two", "s2", "A", "B", "C", "shared")

	clusters, _, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 {
		t.Fatalf("clusters = %d, want 1", len(clusters))
	}
	if clusters[0].SoleSource != 0 || !clusters[0].Redundant {
		t.Errorf("identical namespaces should be redundant: %+v", clusters[0])
	}
}

func TestClusterOfFindsTheOwningDomain(t *testing.T) {
	clusters := []NamespaceCluster{
		{Name: "proj", Slugs: []string{"proj", "proj-copy"}},
		{Name: "other", Slugs: []string{"other"}},
	}
	if c := ClusterOf(clusters, "proj-copy"); c == nil || c.Name != "proj" {
		t.Errorf("ClusterOf(proj-copy) = %+v, want the proj domain", c)
	}
	if c := ClusterOf(clusters, "missing"); c != nil {
		t.Errorf("ClusterOf(missing) = %+v, want nil", c)
	}
}

func TestClusterResultCachesAndReloads(t *testing.T) {
	dir := t.TempDir()
	saveNs(t, dir, "ns", "s1", "A", "B")
	clusters, links, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}
	SaveClusters(dir, clusters, links)

	gotClusters, gotLinks, ok := LoadClusters(dir)
	if !ok {
		t.Fatal("cached clustering did not reload")
	}
	if len(gotClusters) != len(clusters) || len(gotLinks) != len(links) {
		t.Errorf("reloaded %d clusters / %d links, want %d / %d",
			len(gotClusters), len(gotLinks), len(clusters), len(links))
	}
	if gotClusters[0].Name != clusters[0].Name {
		t.Errorf("cached name = %q, want %q", gotClusters[0].Name, clusters[0].Name)
	}
}

func TestLoadClustersMissingFileIsNotAnError(t *testing.T) {
	if _, _, ok := LoadClusters(t.TempDir()); ok {
		t.Error("LoadClusters reported a hit with no cache file present")
	}
}
