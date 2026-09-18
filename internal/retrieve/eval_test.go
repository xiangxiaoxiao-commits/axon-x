package retrieve

import (
	"testing"

	"axon/internal/graph"
)

func TestOfflineRecallQualityGate(t *testing.T) {
	g := &graph.Graph{Entities: []graph.Entity{
		{Name: "PaymentService", Embedding: []float32{1, 0, 0}},
		{Name: "OrderService"},
		{Name: "Frontend", Embedding: []float32{0, 1, 0}},
		{Name: "Deployment", Embedding: []float32{0, 0, 1}},
	}, Relations: []graph.Relation{
		{From: "PaymentService", To: "OrderService", Label: "calls", State: graph.RelationVerified, Confidence: 1},
		{From: "PaymentService", To: "Frontend", Label: "possibly related", State: graph.RelationCandidate, Confidence: .5},
	}}
	chunks := []graph.Chunk{
		{ID: "payment#0", Source: "session:payment", Text: "payment idempotency", Embedding: []float32{1, 0, 0}},
		{ID: "frontend#0", Source: "code:frontend", Text: "frontend layout", Embedding: []float32{0, 1, 0}},
		{ID: "deploy#0", Source: "note:deploy", Text: "deployment rollback", Embedding: []float32{0, 0, 1}},
	}
	cases := []EvalCase{
		{Name: "trusted expansion", Query: "payment", QueryVector: []float32{1, 0, 0}, Expected: []string{"PaymentService", "OrderService"}, Forbidden: []string{"Frontend"}, TopK: 2},
		{Name: "frontend isolation", Query: "frontend", QueryVector: []float32{0, 1, 0}, Expected: []string{"Frontend"}, Forbidden: []string{"PaymentService", "OrderService"}, TopK: 1},
		{Name: "deployment", Query: "deployment", QueryVector: []float32{0, 0, 1}, Expected: []string{"Deployment"}, Forbidden: []string{"Frontend"}, TopK: 1},
	}
	report := Evaluate(g, chunks, cases, RecallOpts{SeedMinScore: .9, ChunkMinScore: .9})
	if report.PrecisionAtK < 1 || report.Recall < 1 || report.ErrorExpansionRate != 0 || report.SourceCoverage < 1 {
		t.Fatalf("recall quality gate failed: %+v", report)
	}
}
