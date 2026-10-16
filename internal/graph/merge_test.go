package graph

import "testing"

// A key that names an existing entity is an identity claim: the new entity says
// it is also called that, and something with that name already exists.
func TestMergeFoldsWhenNameMatches(t *testing.T) {
	g := &Graph{Entities: []Entity{{Name: "支付服务", Type: "service"}}}
	rep := g.MergeAudited([]Entity{
		{Name: "支付服务", Type: "service", Observations: []string{"幂等键用订单号"}},
	}, nil)

	if len(g.Entities) != 1 {
		t.Fatalf("entities = %d, want 1", len(g.Entities))
	}
	if len(rep.Folds) != 1 || len(rep.Refused) != 0 {
		t.Fatalf("folds=%d refused=%d, want 1/0", len(rep.Folds), len(rep.Refused))
	}
	if rep.Folds[0].MatchKind != MatchSameName {
		t.Errorf("matchKind = %q, want %q", rep.Folds[0].MatchKind, MatchSameName)
	}
	if len(g.Entities[0].Observations) != 1 {
		t.Errorf("observations were not folded in: %+v", g.Entities[0].Observations)
	}
}

// The case aliases exist for: two spellings of one thing, where the shared string
// is a real designation of one of them (the bare form of a qualified name).
func TestMergeFoldsSharedAliasThatIsSomebodysName(t *testing.T) {
	g := &Graph{Entities: []Entity{{
		Name: "支付服务", Type: "service", Aliases: []string{"PaymentService", "payment"},
	}}}
	// The code-side entity is package-qualified, so "PaymentService" lands in its
	// aliases rather than being its name — yet it is plainly the same thing.
	rep := g.MergeAudited([]Entity{{
		Name: "pay.PaymentService", Type: "type", Aliases: []string{"PaymentService", "payment service"},
	}}, nil)

	if len(g.Entities) != 1 {
		t.Fatalf("qualified name did not fold: %+v", g.Entities)
	}
	if len(rep.Folds) != 1 || rep.Folds[0].MatchKind != MatchSharedAlias {
		t.Fatalf("folds = %+v, want one %q fold", rep.Folds, MatchSharedAlias)
	}
	// The absorbed name must stay discoverable.
	found := false
	for _, a := range g.Entities[0].Aliases {
		if a == "pay.PaymentService" {
			found = true
		}
	}
	if !found {
		t.Errorf("absorbed name not kept as an alias: %+v", g.Entities[0].Aliases)
	}
}

// Separator spelling must not decide identity: "asset_info" designates the same
// name as "assetinfo".
func TestMergeFoldsSharedAliasAcrossSeparatorSpellings(t *testing.T) {
	g := &Graph{Entities: []Entity{{Name: "资产信息", Aliases: []string{"assetinfo"}}}}
	g.MergeAudited([]Entity{{Name: "asset_info", Aliases: []string{"assetinfo"}}}, nil)
	if len(g.Entities) != 1 {
		t.Fatalf("separator spelling blocked a legitimate fold: %+v", g.Entities)
	}
}

// The case this guard exists for: two entities that merely share a nickname.
// Neither is called that, so the shared string is a category, not an identity.
func TestMergeRefusesSharedAliasThatIsNobodysName(t *testing.T) {
	cases := []struct {
		name        string
		existing    Entity
		incoming    Entity
		viaContains string
	}{
		{
			name:        "release branches sharing a category label",
			existing:    Entity{Name: "release/3.2.1", Type: "concept", Aliases: []string{"发布分支"}},
			incoming:    Entity{Name: "release/2.1.0", Type: "concept", Aliases: []string{"发布分支"}},
			viaContains: "发布分支",
		},
		{
			name:        "unrelated plans sharing a generic name",
			existing:    Entity{Name: "审批历史查询分流方案", Aliases: []string{"方案A"}},
			incoming:    Entity{Name: "gaia-janus 透传层补齐方案", Aliases: []string{"方案A"}},
			viaContains: "方案a",
		},
		{
			name:        "a service and an unrelated concept sharing an abbreviation",
			existing:    Entity{Name: "数字合约使用控制", Aliases: []string{"DUC"}},
			incoming:    Entity{Name: "newhome", Aliases: []string{"duc"}},
			viaContains: "duc",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := &Graph{Entities: []Entity{tc.existing}}
			rep := g.MergeAudited([]Entity{tc.incoming}, nil)

			if len(g.Entities) != 2 {
				t.Fatalf("distinct entities were fused: %+v", g.Entities)
			}
			if len(rep.Refused) != 1 {
				t.Fatalf("refusal not recorded: folds=%+v refused=%+v", rep.Folds, rep.Refused)
			}
			r := rep.Refused[0]
			if !r.Refused || r.MatchKind != MatchSharedAlias {
				t.Errorf("refusal = %+v, want Refused with MatchKind %q", r, MatchSharedAlias)
			}
			if r.Folded != tc.incoming.Name || r.Into != tc.existing.Name {
				t.Errorf("refusal named the wrong pair: %+v", r)
			}
		})
	}
}

// Refusing a fold must not lose the knowledge: both nodes survive intact and
// remain individually addressable.
func TestMergeRefusedFoldKeepsBothEntitiesIntact(t *testing.T) {
	g := &Graph{Entities: []Entity{{
		Name: "release/3.2.1", Type: "concept",
		Aliases:    []string{"发布分支"},
		Assertions: []Assertion{{Text: "3.2.1 的发布说明", Confidence: 0.9}},
	}}}
	g.MergeAudited([]Entity{{
		Name: "release/2.1.0", Type: "concept",
		Aliases:    []string{"发布分支"},
		Assertions: []Assertion{{Text: "2.1.0 的发布说明", Confidence: 0.9}},
	}}, nil)

	if len(g.Entities) != 2 {
		t.Fatalf("entities = %d, want 2", len(g.Entities))
	}
	byName := map[string]Entity{}
	for _, e := range g.Entities {
		byName[e.Name] = e
	}
	// Each version keeps its own fact — the whole point: which release a fact
	// belongs to stays recoverable.
	for name, want := range map[string]string{
		"release/3.2.1": "3.2.1 的发布说明",
		"release/2.1.0": "2.1.0 的发布说明",
	} {
		e, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing", name)
		}
		if len(e.Assertions) != 1 || e.Assertions[0].Text != want {
			t.Errorf("%s assertions = %+v, want only %q", name, e.Assertions, want)
		}
	}
}

// An alias matching the other entity's *name* is still an identity claim — the
// incoming entity says it is also called something that already has a name — and
// must fold regardless of which side carries the name.
func TestMergeFoldsAliasMatchingAnExistingName(t *testing.T) {
	g := &Graph{Entities: []Entity{{Name: "PaymentService", Type: "service"}}}
	rep := g.MergeAudited([]Entity{{
		Name: "支付服务", Type: "service", Aliases: []string{"PaymentService"},
	}}, nil)

	if len(g.Entities) != 1 {
		t.Fatalf("alias matching a name did not fold: %+v", g.Entities)
	}
	if len(rep.Folds) != 1 || rep.Folds[0].MatchKind != MatchAliasAsName {
		t.Fatalf("folds = %+v, want one %q fold", rep.Folds, MatchAliasAsName)
	}
	// The folded name stays discoverable under the kept node.
	if g.Entities[0].Name != "PaymentService" {
		t.Errorf("canonical name changed to %q", g.Entities[0].Name)
	}
	found := false
	for _, a := range g.Entities[0].Aliases {
		if a == "支付服务" {
			found = true
		}
	}
	if !found {
		t.Errorf("absorbed name not kept as an alias: %+v", g.Entities[0].Aliases)
	}
}
