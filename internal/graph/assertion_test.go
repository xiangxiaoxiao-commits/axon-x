package graph

import (
	"testing"
	"time"
)

func TestNormalizeEntityAssertionsUpgradesLegacyFacts(t *testing.T) {
	e := Entity{Observations: []string{"必须校验签名"}, ObsSources: []string{"s1"}}
	NormalizeEntityAssertions(&e)
	if len(e.Assertions) != 1 {
		t.Fatalf("assertions = %d, want 1", len(e.Assertions))
	}
	a := e.Assertions[0]
	if a.Text != "必须校验签名" || a.State != RelationAsserted || a.Confidence != 0.85 {
		t.Fatalf("legacy assertion normalized incorrectly: %+v", a)
	}
	if len(a.Sources) != 1 || a.Sources[0] != "s1" {
		t.Fatalf("legacy provenance lost: %+v", a.Sources)
	}
}

func TestMergeEntityAssertionsAccumulatesCorroboration(t *testing.T) {
	g := &Graph{Entities: []Entity{{
		Name: "支付服务", Assertions: []Assertion{{Text: "必须幂等", State: RelationAsserted, Confidence: .75, Sources: []string{"s1"}}},
	}}}
	g.Merge([]Entity{{
		Name: "支付服务", Assertions: []Assertion{{Text: "必须幂等", State: RelationVerified, Confidence: 1, Sources: []string{"code:a.go"}}},
	}}, nil)
	a := g.Entities[0].Assertions[0]
	if a.State != RelationVerified || a.Confidence != 1 || len(a.Sources) != 2 {
		t.Fatalf("corroboration was not merged: %+v", a)
	}
	if len(g.Entities[0].Observations) != 1 {
		t.Fatalf("legacy projection duplicated facts: %+v", g.Entities[0].Observations)
	}
}

func TestAssertionUsableRejectsExpiredAndCandidateFacts(t *testing.T) {
	now := time.UnixMilli(2_000)
	if AssertionUsable(Assertion{Text: "old", Confidence: .9, ValidUntil: 1_999}, now) {
		t.Fatal("expired assertion was usable")
	}
	if AssertionUsable(Assertion{Text: "guess", State: RelationCandidate, Confidence: .9}, now) {
		t.Fatal("candidate assertion was usable")
	}
	if !AssertionUsable(Assertion{Text: "verified", State: RelationVerified, Confidence: 1}, now) {
		t.Fatal("verified assertion was rejected")
	}
}

func TestNormalizeDoesNotPromoteCandidateFromLegacyProjection(t *testing.T) {
	e := Entity{Assertions: []Assertion{{Text: "needs review", State: RelationCandidate, Confidence: .45}}}
	NormalizeEntityAssertions(&e)
	NormalizeEntityAssertions(&e)
	if len(e.Assertions) != 1 || e.Assertions[0].State != RelationCandidate || e.Assertions[0].Confidence != .45 {
		t.Fatalf("candidate trust changed after repeated normalization: %+v", e.Assertions)
	}
}
