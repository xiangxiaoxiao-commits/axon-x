package graph

import "testing"

func TestCanonicalRelationLabelFoldsKnownAliases(t *testing.T) {
	cases := []struct {
		in        string
		canonical string
		changed   bool
	}{
		{"包含", "包含", false},
		{"包含字段", "包含", true},
		{"包含阶段", "包含", true},
		{"包含 submodule", "包含", true},
		{"contains submodule", "包含", true},
		{"属于submodule", "属于", true},
		{"部署到", "部署于", true},
		{"部署在", "部署于", true},
		{"安装于", "部署于", true},
		{"受其约束", "受约束于", true},
		{"映射", "映射到", true},
		{"由其实现", "被实现", true},
		// Whitespace and case must not matter, so a model that pads or
		// capitalises a label still folds.
		{"  包含 Submodule ", "包含", true},
		{"AUTHORS", "AUTHORS", false},
		// Unknown labels pass through untouched — the schema narrows what it
		// recognises and never invents a canonical for the rest.
		{"约束修改后需检查", "约束修改后需检查", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			canonical, original, changed := CanonicalRelationLabel(tc.in)
			if canonical != tc.canonical || changed != tc.changed {
				t.Fatalf("CanonicalRelationLabel(%q) = (%q, %q, %v), want (%q, _, %v)",
					tc.in, canonical, original, changed, tc.canonical, tc.changed)
			}
		})
	}
}

func TestInverseRelationLabelIsDeclaredNotGuessed(t *testing.T) {
	cases := []struct {
		in       string
		inverse  string
		declared bool
	}{
		{"包含", "属于", true},
		{"属于", "包含", true},
		// Resolving through an alias: 部署到 is a spelling of 部署于.
		{"部署到", "承载", true},
		{"调用", "被调用", true},
		{"被实现", "实现", true},
		// Undeclared relations must report no inverse, which is what stops
		// recall from walking them backwards.
		{"相关", "", false},
		{"约束修改后需检查", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			inverse, ok := InverseRelationLabel(tc.in)
			if ok != tc.declared || inverse != tc.inverse {
				t.Fatalf("InverseRelationLabel(%q) = (%q, %v), want (%q, %v)",
					tc.in, inverse, ok, tc.inverse, tc.declared)
			}
		})
	}
}

func TestTransitiveRelationLabelOnlyForDeclaredLabels(t *testing.T) {
	for _, want := range []string{"包含", "调用", "依赖", "属于", "前置于"} {
		if !IsTransitiveRelationLabel(want) {
			t.Errorf("%q should be declared transitive", want)
		}
	}
	// Aliases inherit the declaration of the canonical they fold onto.
	if !IsTransitiveRelationLabel("包含字段") {
		t.Error("包含字段 should inherit transitivity from 包含")
	}
	// Deployment is not transitive: A deployed on B, B deployed on C does not
	// put A on C.
	for _, unwanted := range []string{"部署于", "承载", "链接", "相关"} {
		if IsTransitiveRelationLabel(unwanted) {
			t.Errorf("%q should not be transitive", unwanted)
		}
	}
}

func TestIsOversizedRelationLabelSeparatesPredicatesFromClauses(t *testing.T) {
	// Every predicate that actually occurs in this project's graphs must pass.
	for _, predicate := range []string{
		"包含", "调用", "依赖", "属于", "部署于", "前置于",
		"包含子模块", "包含字段", "受约束于", "提交并推送到",
	} {
		if IsOversizedRelationLabel(predicate) {
			t.Errorf("predicate %q must not be flagged", predicate)
		}
	}
	// Clauses — the tail that made 13%+ of all relation labels into
	// unrepeatable sentences — must be flagged for review.
	for _, clause := range []string{
		"约束修改后需检查",
		"限制无条件放行",
		"部署其构建镜像",
		"部署到 sit-49",
		"缺失时导致 currentNodeInfo 为 null",
		"远程分支当前缺失于此前探查结果",
	} {
		if !IsOversizedRelationLabel(clause) {
			t.Errorf("clause %q should be flagged as oversized", clause)
		}
	}
}
