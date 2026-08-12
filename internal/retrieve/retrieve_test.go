package retrieve

import (
	"context"
	"testing"

	"axon/internal/embed"
	"axon/internal/graph"
)

// embedText is a tiny helper: the local embedder is deterministic and offline,
// so it gives stable vectors for tests without any network or config.
func embedText(t *testing.T, e *embed.LocalEmbedder, s string) []float32 {
	t.Helper()
	v, err := e.Embed(context.Background(), s)
	if err != nil {
		t.Fatalf("embed %q: %v", s, err)
	}
	return v
}

func TestRecall_SemanticAndKeywordChannels(t *testing.T) {
	emb := embed.NewLocal()

	pay := "支付服务 幂等设计 回调重复投递 防重"
	unrelated := "前端 页面 布局 样式"

	g := &graph.Graph{
		ProjectSlug: "demo",
		Entities: []graph.Entity{
			{Name: "支付服务", Type: "service", Observations: []string{"幂等键用订单号防止回调重复扣款"}, Aliases: []string{"PaymentService"}, Embedding: embedText(t, emb, pay)},
			{Name: "前端页面", Type: "module", Observations: []string{"页面布局与样式"}, Embedding: embedText(t, emb, unrelated)},
		},
		Relations: []graph.Relation{
			{From: "支付服务", To: "订单服务", Label: "依赖"},
		},
	}
	chunks := []graph.Chunk{
		{ID: "s1#0", Source: "s1", Kind: "chat", Text: "关于支付服务幂等设计的讨论：回调可能重复投递", Embedding: embedText(t, emb, pay)},
		{ID: "s1#1", Source: "s1", Kind: "chat", Text: "前端页面样式调整", Embedding: embedText(t, emb, unrelated)},
	}

	qv := embedText(t, emb, "支付回调重复投递怎么做幂等")
	res := Recall(g, chunks, qv, "支付回调重复投递怎么做幂等")

	if !res.Hit["支付服务"] {
		t.Errorf("expected 支付服务 in hit set, got %v", res.Hit)
	}
	// Relation expansion should pull in the linked neighbor when a semantic seed
	// fired.
	if len(res.SemanticSeeds) > 0 && !res.Hit["订单服务"] {
		t.Errorf("expected relation expansion to add 订单服务, got %v", res.Hit)
	}
	if len(res.Chunks) == 0 {
		t.Fatal("expected at least one recalled chunk")
	}
	if res.Chunks[0].ID != "s1#0" {
		t.Errorf("expected payment chunk s1#0 ranked first, got %q", res.Chunks[0].ID)
	}
}

func TestRecall_KeywordOnlyWithoutVectors(t *testing.T) {
	g := &graph.Graph{
		ProjectSlug: "demo",
		Entities: []graph.Entity{
			{Name: "PaymentService", Type: "service", Observations: []string{"handles refunds"}},
		},
	}
	// No query vector => both vector paths dark; entity substring must still hit.
	res := Recall(g, nil, nil, "how does PaymentService handle refunds")
	if !res.Hit["paymentservice"] {
		t.Errorf("expected keyword hit on paymentservice, got %v", res.Hit)
	}
	if len(res.Chunks) != 0 {
		t.Errorf("expected no chunks without a query vector, got %d", len(res.Chunks))
	}
}

func TestRRFFuse_RanksSharedIDsHigher(t *testing.T) {
	got := RRFFuse([]string{"a", "b", "c"}, []string{"b", "d"})
	if len(got) != 4 {
		t.Fatalf("fused len = %d, want 4", len(got))
	}
	if got[0] != "b" {
		t.Errorf("top = %q, want b (high in both lists)", got[0])
	}
}
