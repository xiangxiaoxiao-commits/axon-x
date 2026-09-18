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

func TestFilterExcludedAppliesAssertionReview(t *testing.T) {
	g := &Graph{Entities: []Entity{{Name: "Policy", Assertions: []Assertion{
		{Text: "must audit", State: RelationCandidate, Confidence: .4},
		{Text: "temporary guess", State: RelationAsserted, Confidence: .8},
	}}}}
	ex := &Exclusions{
		Obs: map[string]bool{}, Sessions: map[string]bool{}, RelationReviews: map[string]string{},
		AssertionReviews: map[string]string{
			ObsKey("Policy", "must audit"):      RelationVerified,
			ObsKey("Policy", "temporary guess"): RelationRejected,
		},
	}
	FilterExcluded(g, ex)
	if len(g.Entities) != 1 || len(g.Entities[0].Assertions) != 1 {
		t.Fatalf("reviewed assertions = %+v", g.Entities)
	}
	a := g.Entities[0].Assertions[0]
	if a.State != RelationVerified || a.Confidence != 1 {
		t.Fatalf("verified assertion = %+v", a)
	}
}

func TestUndoLatestReviewRestoresPreviousGovernance(t *testing.T) {
	ex := &Exclusions{Obs: map[string]bool{}, Sessions: map[string]bool{}, AssertionReviews: map[string]string{}, RelationReviews: map[string]string{}}
	key := ObsKey("Policy", "must audit")
	ex.RecordReview("assertion", key, "Policy: must audit", RelationRejected)
	ex.AssertionReviews[key] = RelationRejected
	ex.Obs[key] = true
	description, ok := ex.UndoLatestReview()
	if !ok || description == "" || ex.Obs[key] || ex.AssertionReviews[key] != "" {
		t.Fatalf("undo failed: description=%q exclusions=%+v", description, ex)
	}
	if len(ex.ReviewHistory) != 1 || !ex.ReviewHistory[0].Undone || ex.UndoableReviews() != 0 {
		t.Fatalf("audit history not retained: %+v", ex.ReviewHistory)
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

func TestFilterExcluded_WholeSessionBySource(t *testing.T) {
	g := &Graph{Entities: []Entity{{
		Name:         "Foo",
		Observations: []string{"from noisy session", "from good session"},
		ObsSources:   []string{"noisy", "good"},
	}}}
	ex := &Exclusions{Obs: map[string]bool{}, Sessions: map[string]bool{"noisy": true}}
	FilterExcluded(g, ex)

	if len(g.Entities) != 1 {
		t.Fatalf("entity should survive on its good fact, got %d", len(g.Entities))
	}
	e := g.Entities[0]
	if len(e.Observations) != 1 || e.Observations[0] != "from good session" {
		t.Fatalf("only the noisy-session fact should go, got %v", e.Observations)
	}
	if len(e.ObsSources) != 1 || e.ObsSources[0] != "good" {
		t.Fatalf("sources out of lockstep: %v", e.ObsSources)
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

func TestFilterExcluded_AppliesPersistentRelationReviews(t *testing.T) {
	g := &Graph{
		Entities: []Entity{
			{Name: "A", Observations: []string{"a"}},
			{Name: "B", Observations: []string{"b"}},
			{Name: "C", Observations: []string{"c"}},
		},
		Relations: []Relation{
			{From: "A", To: "B", Label: "调用", State: RelationCandidate, Confidence: 0.4},
			{From: "A", To: "C", Label: "相关", State: RelationCandidate, Confidence: 0.4},
		},
	}
	ex := &Exclusions{
		Obs: map[string]bool{}, Sessions: map[string]bool{},
		RelationReviews: map[string]string{
			RelationKey("A", "B", "调用"): RelationVerified,
			RelationKey("A", "C", "相关"): RelationRejected,
		},
	}
	FilterExcluded(g, ex)
	if len(g.Relations) != 1 {
		t.Fatalf("relations = %d, want 1", len(g.Relations))
	}
	r := g.Relations[0]
	if r.State != RelationVerified || r.Inferred || r.Confidence != 1 {
		t.Fatalf("review was not applied: %+v", r)
	}
}

func TestFilterExcluded_RelationOnlyReviewKeepsStructuralEntities(t *testing.T) {
	g := &Graph{
		Entities:  []Entity{{Name: "package/a"}, {Name: "package/b"}},
		Relations: []Relation{{From: "package/a", To: "package/b", Label: "依赖"}},
	}
	ex := &Exclusions{Obs: map[string]bool{}, Sessions: map[string]bool{}, RelationReviews: map[string]string{
		RelationKey("package/a", "package/b", "依赖"): RelationVerified,
	}}
	FilterExcluded(g, ex)
	if len(g.Entities) != 2 || len(g.Relations) != 1 {
		t.Fatalf("structural graph was damaged: entities=%d relations=%d", len(g.Entities), len(g.Relations))
	}
}
