package graph

import (
	"strings"
	"unicode"
)

// RelationSchema is Axon's RDFS-level predicate vocabulary: the smallest piece
// of "TBox" that makes relation labels reusable.
//
// Without a vocabulary, Label is a free string and the graph fragments along the
// way the model happened to phrase a relation:
//
//   - The same relation is written several ways ("部署于" / "部署到" / "安装于").
//     Each spelling becomes its own edge, so a traversal dead-ends at the variant
//     boundary even though the two edges mean the same thing.
//   - A specialised label ("包含字段") is strictly more specific than the generic
//     one ("包含") yet never joins it, so walking 包含 misses every field edge.
//   - A label can grow into a whole clause ("缺失时导致 currentNodeInfo 为 null").
//     A clause is unrepeatable by construction, so it can be neither unified nor
//     reasoned over.
//
// Three declarations mirror the semantic-web primitives these labels stand in for:
//
//	Aliases    — rdfs:subPropertyOf. The alias is a specialisation of Canonical.
//	             Folding preserves the original text as evidence, so unifying the
//	             traversal key never discards what the model actually said.
//	Inverse    — owl:inverseOf. The label's counter-relation, letting recall walk
//	             an edge backwards along a *declared* direction instead of
//	             guessing whether a reverse step is meaningful.
//	Transitive — owl:TransitiveProperty. Labels whose two-hop chains are also true
//	             statements, i.e. the safe candidates for bounded transitive walk.
//
// The vocabulary is deliberately small and data-derived: every entry below was
// taken from labels that actually occur in this machine's graphs, and each
// direction was verified against real edges before being declared. Anything not
// listed is left exactly as written — the schema narrows known labels, it never
// guesses at unknown ones.
type relationRule struct {
	canonical  string
	aliases    []string
	inverse    string
	transitive bool
}

var relationRules = []relationRule{
	// Containment: verified as gaiac --包含子模块--> jewel-member, so the
	// submodule labels are specialisations of 包含, not a different direction.
	{canonical: "包含", aliases: []string{
		"包含字段", "包含阶段", "包含子模块", "包含 submodule",
		"contains submodule", "包含/处理分支", "包含参数", "包含计费方式",
	}, inverse: "属于", transitive: true},
	{canonical: "属于", aliases: []string{"属于submodule", "属于一种最新口径"},
		inverse: "包含", transitive: true},

	{canonical: "调用", aliases: []string{"调用接口"}, inverse: "被调用", transitive: true},
	{canonical: "依赖", aliases: []string{"依赖项"}, inverse: "被依赖", transitive: true},
	{canonical: "前置于", inverse: "后置于", transitive: true},

	// Deployment: 部署到 / 部署在 / 安装于 all read "<thing> deployed at <place>".
	{canonical: "部署于", aliases: []string{"部署到", "部署在", "安装于"}, inverse: "承载"},
	{canonical: "承载", inverse: "部署于"},

	{canonical: "约束", inverse: "受约束于"},
	{canonical: "受约束于", aliases: []string{"受其约束", "受限于"}, inverse: "约束"},

	// Passive forms. "由其实现" points from the thing implemented TO the
	// implementer (asset/list 接口 --由其实现--> lite 服务), so it is the inverse
	// of 实现 rather than a synonym — folding it to 被实现 keeps the endpoints and
	// still declares the pair, whereas aliasing it to 实现 would invert the edge.
	{canonical: "实现", inverse: "被实现"},
	{canonical: "被实现", aliases: []string{"由其实现"}, inverse: "实现"},
	{canonical: "处理", inverse: "被处理"},
	{canonical: "被处理", aliases: []string{"由其处理"}, inverse: "处理"},

	{canonical: "映射到", aliases: []string{"映射"}, inverse: "被映射到"},
	{canonical: "生成", aliases: []string{"生成到"}, inverse: "被生成"},
	{canonical: "应用", aliases: []string{"应用于"}, inverse: "被应用"},
	{canonical: "查询", aliases: []string{"查询详情", "查询凭证"}, inverse: "被查询"},
	{canonical: "使用", inverse: "被使用"},
	{canonical: "修改", inverse: "被修改"},
	{canonical: "修复", inverse: "被修复"},
	{canonical: "提供", inverse: "被提供"},
	{canonical: "读取", inverse: "被读取"},
	{canonical: "写入", inverse: "被写入"},
	{canonical: "链接", inverse: "被链接"},
}

// MaxRelationLabelRunes is the longest label still treated as a predicate. Real
// predicates in this project's graphs top out at five characters ("包含子模块"),
// while the tail past this point is dominated by clauses ("约束修改后需检查",
// "限制无条件放行", "部署其构建镜像"). Six keeps every genuine predicate and asks
// a human to look at the rest.
//
// This is a review prompt, not a validator: a tersely worded clause of six
// characters or fewer will still slip through, and the check never rewrites a
// label on its own — deciding what a clause's real predicate is needs judgement,
// so the health panel surfaces the count and leaves the call to the user.
const MaxRelationLabelRunes = 6

var (
	canonicalByKey  = map[string]string{}
	ruleByCanonical = map[string]*relationRule{}
)

// schemaKey is the case- and whitespace-insensitive lookup key for a label, so
// "包含 submodule", "包含Submodule" and "包含 submodule " all resolve alike.
func schemaKey(label string) string {
	var b strings.Builder
	b.Grow(len(label))
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		if unicode.IsSpace(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func init() {
	for i := range relationRules {
		r := &relationRules[i]
		ruleByCanonical[r.canonical] = r
		canonicalByKey[schemaKey(r.canonical)] = r.canonical
		for _, alias := range r.aliases {
			canonicalByKey[schemaKey(alias)] = r.canonical
		}
	}
}

// CanonicalRelationLabel folds a label onto its schema canonical form. It
// returns the canonical label, the original text, and whether the label was
// rewritten. Unknown labels are returned untouched with changed=false: the
// schema narrows labels it recognises and never invents a canonical for the rest.
func CanonicalRelationLabel(label string) (canonical, original string, changed bool) {
	original = strings.TrimSpace(label)
	if original == "" {
		return "", "", false
	}
	if c, ok := canonicalByKey[schemaKey(original)]; ok {
		return c, original, c != original
	}
	return original, original, false
}

// InverseRelationLabel returns the declared inverse of a label. ok is false for
// unknown labels and for labels whose inverse was never declared, which is what
// keeps a reverse recall step a deliberate, declared operation rather than a
// guess. It resolves aliases first, so "部署到" reports the inverse of 部署于.
func InverseRelationLabel(label string) (string, bool) {
	canonical, _, _ := CanonicalRelationLabel(label)
	if r, ok := ruleByCanonical[canonical]; ok && r.inverse != "" {
		return r.inverse, true
	}
	return "", false
}

// IsTransitiveRelationLabel reports whether a label is declared transitive, i.e.
// "A —L→ B —L→ C" also implies "A —L→ C". Only these labels may be walked more
// than one hop; treating every label as transitive would explode the graph.
func IsTransitiveRelationLabel(label string) bool {
	canonical, _, _ := CanonicalRelationLabel(label)
	if r, ok := ruleByCanonical[canonical]; ok {
		return r.transitive
	}
	return false
}

// IsOversizedRelationLabel reports whether a label is a clause rather than a
// predicate. Such labels cannot be unified into a vocabulary, so the health panel
// surfaces them for review instead of letting them accumulate unnoticed.
func IsOversizedRelationLabel(label string) bool {
	return len([]rune(strings.TrimSpace(label))) > MaxRelationLabelRunes
}
