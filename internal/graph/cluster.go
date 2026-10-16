package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Namespaces are created by hand: a directory gets an .axon-project naming its
// namespace, an imported bundle takes its own title, a repo gets one. Nothing
// checks whether the knowledge inside is really separate, so the same project
// routinely ends up split across several — this machine had "wanlian" (built up
// over 190 indexed sessions) and "Wanlian1" (an imported bundle of the same
// work) sharing >98% of their entities, and every recall had to guess which to
// look in. Clustering derives the grouping from the knowledge itself instead of
// asking the user to maintain it.

// ClusterMinOverlap is the fraction of the smaller namespace's entity keys that
// must also appear in the other before the two are treated as one knowledge
// domain.
//
// Measured against real data rather than picked: the one genuinely duplicated
// pair shares 100% of the smaller side (Jaccard 0.98, 3146 shared keys), while
// every other pair of namespaces shares at most 7% — and those few hits are
// generic words that happen to appear in unrelated projects ("gaia", "glite").
// Any threshold in the wide gap between 7% and 98% separates them; 0.5 sits in
// the middle so that a namespace which is a genuine subset (an imported bundle
// of half a project, say) still joins its parent while coincidental overlap
// cannot.
//
// Overlap is measured against the *smaller* side, not Jaccard, because the
// relationship is usually containment — a bundle is a snapshot of part of a
// project — and Jaccard punishes the size difference for no reason.
const ClusterMinOverlap = 0.5

// ClusterLink records one pair whose overlap was considered, whether or not it
// resulted in a merge. A derived grouping has to be explainable: without this,
// "why are these two together" is unanswerable, which is the same defect as a
// silent alias fold.
type ClusterLink struct {
	A string `json:"a"`
	B string `json:"b"`
	// Shared is how many entity keys (name or alias, normalized) appear in both.
	Shared int `json:"shared"`
	// Smaller is the entity-key count of the smaller namespace.
	Smaller int `json:"smaller"`
	// Overlap is Shared/Smaller.
	Overlap float64 `json:"overlap"`
	// Merged is true when Overlap reached ClusterMinOverlap.
	Merged bool `json:"merged"`
}

// NamespaceCluster is one knowledge domain: the namespaces that hold the same
// subject matter, derived from shared entities rather than from how they were
// named.
type NamespaceCluster struct {
	// Name is a display label — the largest member namespace's name.
	Name string `json:"name"`
	// Slugs are the member namespaces, largest first.
	Slugs []string `json:"slugs"`
	// Entities is the size of the domain once the members are merged, so the
	// cost of a duplicate namespace is visible as a number.
	Entities int `json:"entities"`
	// Relations likewise.
	Relations int `json:"relations"`
	// SoleSource counts members that contribute knowledge found nowhere else in
	// the cluster. A member with SoleSource == 0 is fully redundant.
	SoleSource int `json:"soleSource"`
	// Redundant is true when every member's entities also exist in another
	// member, i.e. the cluster is one body of knowledge stored more than once.
	Redundant bool `json:"redundant"`
}

// NamespaceProfile is what clustering needs to know about one namespace.
type NamespaceProfile struct {
	Slug      string
	Keys      map[string]bool // normalized names + aliases
	Entities  int             // entities after assembly (Merge applied)
	Relations int
}

// ProfileNamespaces reads every namespace's entity keys and assembled size.
func ProfileNamespaces(dataDir string) ([]NamespaceProfile, error) {
	slugs, err := CacheNamespaces(dataDir)
	if err != nil {
		return nil, err
	}
	var out []NamespaceProfile
	for _, slug := range slugs {
		caches, err := LoadAllCache(dataDir, slug)
		if err != nil || len(caches) == 0 {
			continue
		}
		// Assemble through the real merge path so the sizes and the keys are the
		// same ones recall sees.
		g := &Graph{ProjectSlug: slug, Entities: []Entity{}, Relations: []Relation{}}
		for _, c := range caches {
			g.Merge(c.Entities, c.Relations)
		}
		if len(g.Entities) == 0 {
			continue
		}
		keys := make(map[string]bool, len(g.Entities)*2)
		for _, e := range g.Entities {
			for _, k := range entityKeys(e) {
				keys[k] = true
			}
		}
		out = append(out, NamespaceProfile{
			Slug:      slug,
			Keys:      keys,
			Entities:  len(g.Entities),
			Relations: len(g.Relations),
		})
	}
	return out, nil
}

// ClusterNamespaces groups namespaces that hold the same subject matter, and
// returns every pair it considered so the grouping can be inspected.
//
// Grouping is transitive: if A holds the same knowledge as B and B as C, all
// three form one domain even when A and C share little directly. That is the
// reason a graph of pairwise overlaps is not enough on its own — the connected
// components carry the answer.
func ClusterNamespaces(dataDir string) ([]NamespaceCluster, []ClusterLink, error) {
	profiles, err := ProfileNamespaces(dataDir)
	if err != nil {
		return nil, nil, err
	}
	links := compareProfiles(profiles)

	// Union-find over the merged pairs.
	parent := make(map[string]string, len(profiles))
	for _, p := range profiles {
		parent[p.Slug] = p.Slug
	}
	var find func(string) string
	find = func(x string) string {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	for _, l := range links {
		if !l.Merged {
			continue
		}
		ra, rb := find(l.A), find(l.B)
		if ra != rb {
			parent[rb] = ra
		}
	}

	members := map[string][]NamespaceProfile{}
	for _, p := range profiles {
		root := find(p.Slug)
		members[root] = append(members[root], p)
	}

	clusters := make([]NamespaceCluster, 0, len(members))
	for _, group := range members {
		// Largest member first, and take its name for the domain label.
		sort.Slice(group, func(i, j int) bool {
			if group[i].Entities != group[j].Entities {
				return group[i].Entities > group[j].Entities
			}
			return group[i].Slug < group[j].Slug
		})
		c := NamespaceCluster{Name: group[0].Slug}
		for _, p := range group {
			c.Slugs = append(c.Slugs, p.Slug)
		}
		c.Entities, c.Relations, c.SoleSource = mergeStats(dataDir, group)
		// Redundant exactly when no member owns knowledge the others lack.
		c.Redundant = c.SoleSource == 0
		clusters = append(clusters, c)
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].Entities != clusters[j].Entities {
			return clusters[i].Entities > clusters[j].Entities
		}
		return clusters[i].Name < clusters[j].Name
	})
	return clusters, links, nil
}

// compareProfiles measures every namespace pair's overlap.
func compareProfiles(profiles []NamespaceProfile) []ClusterLink {
	var links []ClusterLink
	for i := 0; i < len(profiles); i++ {
		for j := i + 1; j < len(profiles); j++ {
			a, b := profiles[i], profiles[j]
			small := a
			large := b
			if len(a.Keys) > len(b.Keys) {
				small, large = b, a
			}
			if len(small.Keys) == 0 {
				continue
			}
			shared := 0
			for k := range small.Keys {
				if large.Keys[k] {
					shared++
				}
			}
			overlap := float64(shared) / float64(len(small.Keys))
			links = append(links, ClusterLink{
				A: a.Slug, B: b.Slug,
				Shared: shared, Smaller: len(small.Keys),
				Overlap: overlap, Merged: overlap >= ClusterMinOverlap,
			})
		}
	}
	sort.Slice(links, func(i, j int) bool {
		if links[i].Overlap != links[j].Overlap {
			return links[i].Overlap > links[j].Overlap
		}
		return links[i].A < links[j].A
	})
	return links
}

// mergeStats assembles the group once and reports its merged size plus how many
// members contribute knowledge found nowhere else in the group.
func mergeStats(dataDir string, group []NamespaceProfile) (entities, relations, soleSource int) {
	if len(group) == 1 {
		return group[0].Entities, group[0].Relations, 1
	}
	g := &Graph{ProjectSlug: "cluster", Entities: []Entity{}, Relations: []Relation{}}
	for _, p := range group {
		caches, err := LoadAllCache(dataDir, p.Slug)
		if err != nil {
			continue
		}
		for _, c := range caches {
			g.Merge(c.Entities, c.Relations)
		}
	}
	// A member is redundant when every key it holds is also held by some other
	// member: it could be dropped without losing a name from the domain.
	for i, p := range group {
		unique := false
		for k := range p.Keys {
			if !anyOtherHasKey(group, i, k) {
				unique = true
				break
			}
		}
		if unique {
			soleSource++
		}
	}
	return len(g.Entities), len(g.Relations), soleSource
}

func anyOtherHasKey(group []NamespaceProfile, skip int, key string) bool {
	for i, p := range group {
		if i == skip {
			continue
		}
		if p.Keys[key] {
			return true
		}
	}
	return false
}

// ClusterNamespacesFromDir is the exported entry point for callers that already
// know the data directory, mirroring ClusterNamespaces' signature.
func ClusterNamespacesFromDir(dataDir string) ([]NamespaceCluster, []ClusterLink, error) {
	return ClusterNamespaces(dataDir)
}

// LoadClusters reads a cached clustering, if one was written by SaveClusters.
func LoadClusters(dataDir string) ([]NamespaceCluster, []ClusterLink, bool) {
	path := filepath.Join(dataDir, "clusters.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	var payload struct {
		Clusters []NamespaceCluster `json:"clusters"`
		Links    []ClusterLink      `json:"links"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, nil, false
	}
	return payload.Clusters, payload.Links, true
}

// SaveClusters caches a clustering so the UI does not re-read every cache on
// each open. Best-effort: a failure to write is not worth failing the caller.
func SaveClusters(dataDir string, clusters []NamespaceCluster, links []ClusterLink) {
	payload := struct {
		Clusters []NamespaceCluster `json:"clusters"`
		Links    []ClusterLink      `json:"links"`
	}{clusters, links}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(dataDir, "clusters.json.tmp")
	if os.WriteFile(tmp, data, 0o644) != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(dataDir, "clusters.json"))
}

// ClusterOf returns the cluster holding a slug, or nil.
func ClusterOf(clusters []NamespaceCluster, slug string) *NamespaceCluster {
	want := strings.TrimSpace(slug)
	for i := range clusters {
		for _, s := range clusters[i].Slugs {
			if s == want {
				return &clusters[i]
			}
		}
	}
	return nil
}
