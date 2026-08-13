package main

import (
	"reflect"
	"testing"

	"axon/internal/graph"
)

func TestTokenSet_CJKAndLatin(t *testing.T) {
	// CJK splits per character; Latin splits per word; punctuation dropped.
	got := sortedTokens("用 bizOrderId 做幂等键!")
	want := []string{"bizorderid", "做", "幂", "用", "等", "键"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokenSet = %v, want %v", got, want)
	}
}

func TestJaccard(t *testing.T) {
	a := tokenSet("用 bizOrderId 做幂等键")
	b := tokenSet("做幂等键，用 bizOrderId") // reordered + repunctuated, same tokens
	if s := jaccard(a, b); s < obsDupThreshold {
		t.Fatalf("reordered obs jaccard = %.2f, want >= %.2f", s, obsDupThreshold)
	}
	c := tokenSet("完全不同的一句话关于缓存策略")
	if s := jaccard(a, c); s >= obsDupThreshold {
		t.Fatalf("unrelated obs jaccard = %.2f, want < %.2f", s, obsDupThreshold)
	}
}

func TestResolveEntities_RelabelsOntoCanonical(t *testing.T) {
	existing := []graph.Entity{{
		Name:         "支付服务",
		Observations: []string{"负责扣款"},
		Embedding:    []float32{1, 0, 0},
	}}
	incoming := []graph.Entity{{
		Name:         "PaymentService", // different name, near-identical vector
		Observations: []string{"提供退款接口"},
		Embedding:    []float32{0.99, 0.01, 0},
	}}
	out := resolveEntities(existing, incoming)
	if len(out) != 1 {
		t.Fatalf("want 1 entity, got %d", len(out))
	}
	if out[0].Name != "支付服务" {
		t.Errorf("want relabeled to canonical 支付服务, got %q", out[0].Name)
	}
	// Original name preserved as alias so it stays discoverable.
	foundAlias := false
	for _, al := range out[0].Aliases {
		if al == "PaymentService" {
			foundAlias = true
		}
	}
	if !foundAlias {
		t.Errorf("want PaymentService kept as alias, aliases=%v", out[0].Aliases)
	}
}

func TestResolveEntities_DropsDuplicateObservationsAndEmptyEntity(t *testing.T) {
	existing := []graph.Entity{{
		Name:         "幂等",
		Observations: []string{"用 bizOrderId 做幂等键"},
		Embedding:    []float32{1, 0, 0},
	}}
	incoming := []graph.Entity{{
		Name:         "幂等",
		Observations: []string{"做幂等键，用 bizOrderId"}, // reordered dup -> dropped
		Embedding:    []float32{1, 0, 0},
	}}
	out := resolveEntities(existing, incoming)
	// All observations were dupes, so the fact-less entity is dropped entirely.
	if len(out) != 0 {
		t.Fatalf("want entity dropped (all obs dup), got %d: %+v", len(out), out)
	}
}

func TestResolveEntities_KeepsDistinctEntity(t *testing.T) {
	existing := []graph.Entity{{
		Name:         "支付服务",
		Observations: []string{"负责扣款"},
		Embedding:    []float32{1, 0, 0},
	}}
	incoming := []graph.Entity{{
		Name:         "缓存策略",
		Observations: []string{"用 Redis 做二级缓存"},
		Embedding:    []float32{0, 1, 0}, // orthogonal -> below threshold
	}}
	out := resolveEntities(existing, incoming)
	if len(out) != 1 || out[0].Name != "缓存策略" {
		t.Fatalf("distinct entity should pass through unchanged, got %+v", out)
	}
}

func TestResolveEntities_NoEmbeddingPassesThrough(t *testing.T) {
	existing := []graph.Entity{{Name: "支付服务", Observations: []string{"扣款"}, Embedding: []float32{1, 0}}}
	incoming := []graph.Entity{{Name: "新概念", Observations: []string{"一条事实"}}} // no embedding
	out := resolveEntities(existing, incoming)
	if len(out) != 1 || out[0].Name != "新概念" {
		t.Fatalf("no-embedding entity should pass through, got %+v", out)
	}
}
