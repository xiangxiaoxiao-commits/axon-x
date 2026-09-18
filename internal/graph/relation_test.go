package graph

import "testing"

func TestNormalizeRelationBackwardsCompatibility(t *testing.T) {
	fact := NormalizeRelation(Relation{From: " A ", To: "B", Label: "依赖"})
	if fact.State != RelationAsserted || fact.Inferred || fact.Confidence != 0.85 {
		t.Fatalf("old solid edge normalized incorrectly: %+v", fact)
	}
	weak := NormalizeRelation(Relation{From: "A", To: "B", Label: "相关", Inferred: true})
	if weak.State != RelationCandidate || !weak.Inferred || weak.Confidence != 0.45 {
		t.Fatalf("old inferred edge normalized incorrectly: %+v", weak)
	}
}

func TestRelationUsableForRecallUsesTrustGate(t *testing.T) {
	cases := []struct {
		name string
		r    Relation
		want bool
	}{
		{"verified", Relation{State: RelationVerified, Confidence: 1}, true},
		{"asserted strong", Relation{State: RelationAsserted, Confidence: 0.8}, true},
		{"asserted weak", Relation{State: RelationAsserted, Confidence: 0.5}, false},
		{"candidate", Relation{State: RelationCandidate, Confidence: 0.9}, false},
		{"rejected", Relation{State: RelationRejected, Confidence: 1}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RelationUsableForRecall(tc.r); got != tc.want {
				t.Fatalf("RelationUsableForRecall() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeCombinesRelationTrustAndProvenance(t *testing.T) {
	g := &Graph{
		Entities: []Entity{{Name: "A"}, {Name: "B"}},
		Relations: []Relation{{
			From: "A", To: "B", Label: "依赖", State: RelationCandidate,
			Confidence: 0.4, Sources: []string{"s1"}, Evidence: []string{"候选依据"},
		}},
	}
	g.Merge(nil, []Relation{{
		From: "A", To: "B", Label: "依赖", State: RelationVerified,
		Confidence: 1, Sources: []string{"s2"}, Evidence: []string{"代码确认"},
	}})
	if len(g.Relations) != 1 {
		t.Fatalf("relations = %d, want 1", len(g.Relations))
	}
	r := g.Relations[0]
	if r.State != RelationVerified || r.Inferred || r.Confidence != 1 {
		t.Fatalf("trust was not promoted: %+v", r)
	}
	if len(r.Sources) != 2 || len(r.Evidence) != 2 {
		t.Fatalf("provenance was not accumulated: %+v", r)
	}
}
