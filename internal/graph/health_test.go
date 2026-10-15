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

func TestAssessHealthFlagsClauseLabelsForReview(t *testing.T) {
	now := time.UnixMilli(10_000)
	g := &Graph{
		Entities: []Entity{{Name: "A"}, {Name: "B"}, {Name: "C"}},
		Relations: []Relation{
			{From: "A", To: "B", Label: "依赖", Sources: []string{"s1"}},
			{From: "A", To: "C", Label: "约束修改后需检查", Sources: []string{"s1"}},
		},
	}
	h := AssessHealth(g, now)
	if h.OversizedRelationLabels != 1 {
		t.Fatalf("oversizedRelationLabels = %d, want 1", h.OversizedRelationLabels)
	}
	found := false
	for _, issue := range h.Issues {
		if issue.Code == "oversized_relation_labels" {
			found = true
			if issue.Count != 1 {
				t.Errorf("issue count = %d, want 1", issue.Count)
			}
		}
	}
	if !found {
		t.Errorf("no oversized_relation_labels issue raised: %+v", h.Issues)
	}
}
