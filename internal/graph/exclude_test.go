package graph

import "testing"

func TestFilterExcluded_RemovesFactAndKeepsSources(t *testing.T) {
	g := &Graph{Entities: []Entity{{
		Name:         "PaymentService",
		Type:         "service",
		Observations: []string{"负责支付回调校验", "用了幂等表防重复扣款"},
		ObsSources:   []string{"sessA", "sessB"},
	}}}
	ex := &Exclusions{Obs: map[string]bool{
		ObsKey("PaymentService", "负责支付回调校验"): true,
	}}
	FilterExcluded(g, ex)

	if len(g.Entities) != 1 {
		t.Fatalf("entity should survive, got %d", len(g.Entities))
	}
	e := g.Entities[0]
	if len(e.Observations) != 1 || e.Observations[0] != "用了幂等表防重复扣款" {
		t.Fatalf("wrong observations kept: %v", e.Observations)
	}
	if len(e.ObsSources) != 1 || e.ObsSources[0] != "sessB" {
		t.Fatalf("ObsSources must stay in lockstep, got %v", e.ObsSources)
	}
}

func TestFilterExcluded_DropsEmptyEntityAndItsRelations(t *testing.T) {
	g := &Graph{
		Entities: []Entity{
			{Name: "Foo", Observations: []string{"only fact"}, ObsSources: []string{"s1"}},
			{Name: "Bar", Observations: []string{"keep me"}, ObsSources: []string{"s1"}},
		},
		Relations: []Relation{{From: "Foo", To: "Bar", Label: "依赖"}},
	}
	ex := &Exclusions{Obs: map[string]bool{ObsKey("Foo", "only fact"): true}}
	FilterExcluded(g, ex)

	if len(g.Entities) != 1 || g.Entities[0].Name != "Bar" {
		t.Fatalf("emptied entity Foo should be dropped, got %+v", g.Entities)
	}
	if len(g.Relations) != 0 {
		t.Fatalf("relation referencing dropped entity should go, got %v", g.Relations)
	}
}

// The crux: an excluded fact must NOT come back after a fresh Merge from caches.
func TestFilterExcluded_SurvivesReMerge(t *testing.T) {
	cacheEnts := []Entity{{
		Name: "Foo", Observations: []string{"noise fact", "good fact"},
		ObsSources: []string{"sess1", "sess1"},
	}}
	ex := &Exclusions{Obs: map[string]bool{ObsKey("Foo", "noise fact"): true}}

	// Simulate assembleGraph: merge caches, then filter.
	g := &Graph{Entities: []Entity{}, Relations: []Relation{}}
	g.Merge(cacheEnts, nil)
	FilterExcluded(g, ex)

	if len(g.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(g.Entities))
	}
	for _, o := range g.Entities[0].Observations {
		if o == "noise fact" {
			t.Fatal("excluded fact came back after re-merge")
		}
	}
}
