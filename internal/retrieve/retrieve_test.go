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

func TestRecall_DoesNotExpandCandidateOrLowConfidenceRelations(t *testing.T) {
	g := &graph.Graph{
		Entities: []graph.Entity{
			{Name: "Seed", Embedding: []float32{1, 0}},
			{Name: "CandidateNeighbor", Embedding: []float32{0, 1}},
			{Name: "WeakNeighbor", Embedding: []float32{0, 1}},
		},
		Relations: []graph.Relation{
			{From: "Seed", To: "CandidateNeighbor", Label: "相关", State: graph.RelationCandidate, Confidence: 0.5},
			{From: "Seed", To: "WeakNeighbor", Label: "依赖", State: graph.RelationAsserted, Confidence: 0.5},
		},
	}
	res := Recall(g, nil, []float32{1, 0}, "unmatched query")
	if !res.Hit["seed"] {
		t.Fatalf("expected semantic seed, got %v", res.Hit)
	}
	if res.Hit["candidateneighbor"] || res.Hit["weakneighbor"] {
		t.Fatalf("untrusted relations expanded recall: %v", res.Hit)
	}
}

// TestRecall_ExpandsBackwardOnlyThroughDeclaredInverse is the case the old
// forward-only rule lost outright: asking about a specific callee has to surface
// its callers, which live on inbound edges.
func TestRecall_ExpandsBackwardOnlyThroughDeclaredInverse(t *testing.T) {
	g := &graph.Graph{
		Entities: []graph.Entity{
			{Name: "graph.Dir", Embedding: []float32{1, 0}},
			{Name: "config.Load", Embedding: []float32{0, 1}},
			{Name: "unrelated", Embedding: []float32{0, 1}},
		},
		Relations: []graph.Relation{
			// Inbound edge with a declared inverse (调用 -> 被调用).
			{From: "config.Load", To: "graph.Dir", Label: "调用",
				State: graph.RelationAsserted, Confidence: 0.9},
			// Inbound edge with no declared inverse: walking it back would be a
			// guess, so it must not expand.
			{From: "unrelated", To: "graph.Dir", Label: "相关",
				State: graph.RelationAsserted, Confidence: 0.9},
		},
	}
	res := Recall(g, nil, []float32{1, 0}, "unmatched query")
	if !res.Hit["graph.dir"] {
		t.Fatalf("expected the queried entity to be a seed, got %v", res.Hit)
	}
	if !res.Hit["config.load"] {
		t.Errorf("declared inverse did not pull in the caller: %v", res.Hit)
	}
	if res.Hit["unrelated"] {
		t.Errorf("undeclared relation expanded backward: %v", res.Hit)
	}
}

// TestRecall_SecondHopStaysOnTransitiveLabels guards the closure: widening
// expandHops must not turn arbitrary two-edge chains into recall.
func TestRecall_SecondHopStaysOnTransitiveLabels(t *testing.T) {
	g := &graph.Graph{
		Entities: []graph.Entity{
			{Name: "Repo", Embedding: []float32{1, 0}},
			{Name: "mid", Embedding: []float32{0, 1}},
			{Name: "leafSymbol", Embedding: []float32{0, 1}},
			{Name: "deployedPlace", Embedding: []float32{0, 1}},
			{Name: "beyondDeploy", Embedding: []float32{0, 1}},
		},
		Relations: []graph.Relation{
			// 包含 is transitive: Repo -> mid -> leafSymbol is two hops and
			// should resolve.
			{From: "Repo", To: "mid", Label: "包含", State: graph.RelationAsserted, Confidence: 0.9},
			{From: "mid", To: "leafSymbol", Label: "包含", State: graph.RelationAsserted, Confidence: 0.9},
			// 部署于 is not transitive: two hops must NOT resolve.
			{From: "Repo", To: "deployedPlace", Label: "部署于", State: graph.RelationAsserted, Confidence: 0.9},
			{From: "deployedPlace", To: "beyondDeploy", Label: "部署于", State: graph.RelationAsserted, Confidence: 0.9},
		},
	}
	res := Recall(g, nil, []float32{1, 0}, "unmatched query")
	if !res.Hit["leafsymbol"] {
		t.Errorf("transitive label did not reach the second hop: %v", res.Hit)
	}
	if res.Hit["beyonddeploy"] {
		t.Errorf("non-transitive label was walked to the second hop: %v", res.Hit)
	}
}

// TestRecall_RecordsHowEachEntityWasReached covers the auditability contract: a
// direct match and an inferred neighbour must be distinguishable, or a hop
// through the graph gets mistaken for the graph's answer.
func TestRecall_RecordsHowEachEntityWasReached(t *testing.T) {
	g := &graph.Graph{
		Entities: []graph.Entity{
			{Name: "graph.Dir", Embedding: []float32{1, 0}},
			{Name: "config.Load", Embedding: []float32{0, 1}},
			{Name: "main.startup", Embedding: []float32{0, 1}},
			{Name: "unrelated", Embedding: []float32{0, 1}},
		},
		Relations: []graph.Relation{
			// Reverse hop: the query matches the object, the caller comes in
			// through the declared inverse of 调用.
			{From: "config.Load", To: "graph.Dir", Label: "调用",
				State: graph.RelationAsserted, Confidence: 0.9},
			// Two forward hops on a transitive label.
			{From: "graph.Dir", To: "main.startup", Label: "包含",
				State: graph.RelationAsserted, Confidence: 0.9},
			// No declared inverse: must not expand, so must not appear in Derived.
			{From: "unrelated", To: "graph.Dir", Label: "相关",
				State: graph.RelationAsserted, Confidence: 0.9},
		},
	}
	res := Recall(g, nil, []float32{1, 0}, "unmatched query")

	// The seed matched the query itself, so it carries no derivation.
	if _, ok := res.Derived["graph.dir"]; ok {
		t.Errorf("the directly matched entity was marked as derived: %+v", res.Derived["graph.dir"])
	}
	if !res.Hit["graph.dir"] {
		t.Fatalf("expected the queried entity to be a seed, got %v", res.Hit)
	}

	reverse, ok := res.Derived["config.load"]
	if !ok {
		t.Fatalf("reverse hop was not recorded: %+v", res.Derived)
	}
	if !reverse.Reverse {
		t.Errorf("reverse hop not flagged as reverse: %+v", reverse)
	}
	if reverse.Via != "graph.Dir" || reverse.Label != "调用" || reverse.Hops != 1 {
		t.Errorf("reverse derivation = %+v, want via graph.Dir / 调用 / 1 hop", reverse)
	}

	forward, ok := res.Derived["main.startup"]
	if !ok {
		t.Fatalf("forward hop was not recorded: %+v", res.Derived)
	}
	if forward.Reverse {
		t.Errorf("forward hop wrongly flagged as reverse: %+v", forward)
	}
	if forward.Via != "graph.Dir" || forward.Label != "包含" || forward.Hops != 1 {
		t.Errorf("forward derivation = %+v, want via graph.Dir / 包含 / 1 hop", forward)
	}

	if _, ok := res.Derived["unrelated"]; ok || res.Hit["unrelated"] {
		t.Errorf("undeclared relation expanded recall: %+v", res.Derived)
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

func TestAssembleGraphAppliesGovernanceAndChunkSessionExclusion(t *testing.T) {
	dataDir := t.TempDir()
	slug := "project"
	if err := graph.SaveCache(dataDir, slug, &graph.SessionCache{
		SessionID: "s1", Mtime: 1, Schema: graph.CacheSchema,
		Entities: []graph.Entity{
			{Name: "A", Observations: []string{"from excluded session"}, ObsSources: []string{"s1"}},
			{Name: "B", Observations: []string{"also excluded"}, ObsSources: []string{"s1"}},
		},
		Relations: []graph.Relation{{From: "A", To: "B", Label: "相关"}},
		Chunks:    []graph.Chunk{{ID: "s1#0", Source: "s1", Text: "hidden", Embedding: []float32{1}}},
	}); err != nil {
		t.Fatal(err)
	}
	ex := &graph.Exclusions{
		Obs: map[string]bool{}, Sessions: map[string]bool{"s1": true},
		RelationReviews: map[string]string{graph.RelationKey("A", "B", "相关"): graph.RelationRejected},
	}
	if err := graph.SaveExclusions(dataDir, slug, ex); err != nil {
		t.Fatal(err)
	}
	g, err := AssembleGraph(dataDir, slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Entities) != 0 || len(g.Relations) != 0 {
		t.Fatalf("governance not applied: %+v", g)
	}
	if chunks := LoadChunks(dataDir, slug); len(chunks) != 0 {
		t.Fatalf("excluded session chunks leaked: %+v", chunks)
	}
}
