package graph

import (
	"testing"
	"time"
)

func TestAssessHealthReportsActionableQualityGaps(t *testing.T) {
	now := time.UnixMilli(10_000)
	g := &Graph{
		Entities: []Entity{
			{Name: "A", Assertions: []Assertion{{Text: "trusted", State: RelationVerified, Confidence: 1, Sources: []string{"code:a"}}}},
			{Name: "B", Assertions: []Assertion{{Text: "expired", Confidence: .9, ValidUntil: 9_000}}},
			{Name: "C"},
		},
		Relations: []Relation{{From: "A", To: "B", Label: "相关", State: RelationCandidate, Confidence: .4}},
	}
	h := AssessHealth(g, now)
	if h.Entities != 3 || h.Assertions != 2 || h.TrustedAssertions != 1 {
		t.Fatalf("wrong assertion metrics: %+v", h)
	}
	if h.CandidateRelations != 1 || h.SourcelessRelations != 1 || h.IsolatedEntities != 1 {
		t.Fatalf("wrong relation metrics: %+v", h)
	}
	if h.Score >= 100 || len(h.Issues) == 0 {
		t.Fatalf("unhealthy graph reported as healthy: %+v", h)
	}
}
