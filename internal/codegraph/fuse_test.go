package codegraph

import (
	"testing"

	"axon/internal/graph"
)

// TestFusionWithConversationEntity verifies alias normalization fuses a code
// entity (pay.PaymentService, alias "payment service") with a conversation
// entity named "支付服务" that carries "PaymentService" as an alias.
func TestFusionWithConversationEntity(t *testing.T) {
	g := &graph.Graph{
		Entities: []graph.Entity{
			{Name: "支付服务", Type: "service", Observations: []string{"负责支付回调"}, Aliases: []string{"PaymentService", "payment"}},
		},
	}
	codeEnts := []graph.Entity{
		{Name: "pay.PaymentService", Type: "type", Observations: []string{"code: payment struct"}, Aliases: []string{"PaymentService", "payment service"}},
	}
	g.Merge(codeEnts, nil)
	if len(g.Entities) != 1 {
		t.Fatalf("expected fusion into 1 entity, got %d: %+v", len(g.Entities), g.Entities)
	}
	if len(g.Entities[0].Observations) != 2 {
		t.Errorf("expected merged observations, got %v", g.Entities[0].Observations)
	}
}
