package graph

import (
	"strings"
	"unicode"
)

// MergeAudit records one entity folded into another by alias normalization, or
// one such fold that was refused.
//
// Merging on name/alias is effectively an owl:sameAs claim, and sameAs is
// transitive: register A's aliases, and an entity naming one of them folds into A,
// whose aliases then absorb the next one. The graph is right to collapse
// "支付服务"/"PaymentService", but a chain of such collisions can also fuse two
// genuinely different things that merely shared a spelling somewhere along the
// way — and because the merge is silent and irreversible, there was previously no
// way to see that it happened or why.
type MergeAudit struct {
	// Folded is the name of the entity that was absorbed (or, for a refused fold,
	// the entity that was kept separate).
	Folded string
	// Into is the name of the canonical entity it joined (or would have joined).
	Into string
	// Via is the normalized name/alias key whose collision was considered.
	Via string
	// MatchKind says what the colliding key was to each side:
	//
	//	same-name      both entities are named by the key (exact identifier match)
	//	name-as-alias  the folded entity is named by it, the target only aliases it
	//	alias-as-name  the folded entity only aliases it, the target is named by it
	//	shared-alias   both merely alias it — neither is called that
	MatchKind string
	// Refused is true when the fold was declined. Only shared-alias collisions are
	// refused: see MergeAudited.
	Refused bool
	// TypeChanged is true when the absorbed entity's type differed from the
	// canonical entity's, which is a hint that the two are not the same thing
	// (a function folding into a file, say).
	TypeChanged bool
}

// Match-kind values for MergeAudit.MatchKind.
const (
	MatchSameName    = "same-name"
	MatchNameAsAlias = "name-as-alias"
	MatchAliasAsName = "alias-as-name"
	MatchSharedAlias = "shared-alias"
)

// MergeReport is the outcome of one MergeAudited call.
type MergeReport struct {
	// Folds is every entity that was absorbed, with the reason it collided.
	Folds []MergeAudit
	// Refused is every collision that was declined because the two entities only
	// shared a nickname. Kept so the decision is inspectable rather than merely
	// absent.
	Refused []MergeAudit
}

// Merge folds newly extracted entities/relations into g. See MergeAudited for the
// normalization rules; this wrapper discards the audit trail.
func (g *Graph) Merge(entities []Entity, relations []Relation) {
	g.MergeAudited(entities, relations)
}

// MergeAudited folds newly extracted entities/relations into g, de-duplicating by
// normalized name so re-processing accumulates knowledge instead of piling up
// duplicate nodes. Observations (with their sources) are unioned; relations are
// de-duped by triple. It returns what was folded and what was refused.
//
// Alias normalization: an entity folds into an existing one when a name or alias
// of one collides with a name or alias of the other. This collapses
// "支付服务"/"PaymentService"/"payment" into a single node instead of three
// fragments.
//
// One collision is deliberately refused. When the colliding key is neither
// entity's name — both merely list it as an alias — the two are not called that;
// they share a nickname. On real data that case is 34% of all collisions and it
// is where the damage is: entities named "release/2.1.0" and "release/3.2.1" both
// alias "发布分支" and were fused into one node, as were the distinct "sit-14 环境"
// and "sit-36 环境" (via "sit 环境"), losing which release or environment a fact
// belonged to. A nickname is a category, not an identity.
//
// The asymmetry is intentional. Keeping two nodes for one concept leaves a
// duplicate that is still reachable (relations resolve through the same
// name/alias index either way) and can be merged later; fusing two distinct
// things is silent, transitive and irreversible. Prefer the recoverable error.
func (g *Graph) MergeAudited(entities []Entity, relations []Relation) MergeReport {
	var report MergeReport
	// Index every existing entity by its name and each of its aliases, so a new
	// entity matching any of those keys folds into it. keyKind records whether
	// each key was registered as a name or an alias (last registration wins, which
	// matches idx's own overwrite semantics), which is what classifies a collision.
	idx := make(map[string]int, len(g.Entities))
	keyKind := make(map[string]string, len(g.Entities))
	register := func(i int) {
		e := g.Entities[i]
		if k := normKey(e.Name); k != "" {
			idx[k] = i
			keyKind[k] = "name"
		}
		for _, a := range e.Aliases {
			if k := normKey(a); k != "" {
				idx[k] = i
				keyKind[k] = "alias"
			}
		}
	}
	for i := range g.Entities {
		NormalizeEntityAssertions(&g.Entities[i])
		register(i)
	}

	for _, ne := range entities {
		NormalizeEntityAssertions(&ne)
		key := normKey(ne.Name)
		if key == "" {
			continue
		}
		target, matchedKey, matchKind := -1, "", ""
		refused := -1
		// The entity's own name first: naming the same key is the strongest claim.
		if i, ok := idx[key]; ok {
			target, matchedKey = i, key
			if keyKind[key] == "name" {
				matchKind = MatchSameName
			} else {
				matchKind = MatchNameAsAlias
			}
		}
		// Then its aliases, in order. An alias matching a *name* is an identity
		// claim and still folds; an alias matching only another alias is refused,
		// but keep looking — a later alias may name an entity.
		if target < 0 {
			for _, a := range ne.Aliases {
				k := normKey(a)
				if k == "" {
					continue
				}
				i, ok := idx[k]
				if !ok {
					continue
				}
				if keyKind[k] == "name" {
					target, matchedKey, matchKind = i, k, MatchAliasAsName
					break
				}
				// Both entities merely list this key as an alias. Fold only when it
				// is nevertheless somebody's actual designation — their name or a
				// part of it — which is what gives the collision a referent a human
				// would recognise ("paymentservice" inside "pay.paymentservice").
				if sharedAliasIsDesignation(k, g.Entities[i].Name, ne.Name) {
					target, matchedKey, matchKind = i, k, MatchSharedAlias
					break
				}
				if refused < 0 {
					refused = i
					report.Refused = append(report.Refused, MergeAudit{
						Folded:      ne.Name,
						Into:        g.Entities[i].Name,
						Via:         k,
						MatchKind:   MatchSharedAlias,
						Refused:     true,
						TypeChanged: ne.Type != "" && g.Entities[i].Type != "" && ne.Type != g.Entities[i].Type,
					})
				}
			}
		}
		if target >= 0 {
			e := &g.Entities[target]
			report.Folds = append(report.Folds, MergeAudit{
				Folded:      ne.Name,
				Into:        e.Name,
				Via:         matchedKey,
				MatchKind:   matchKind,
				TypeChanged: ne.Type != "" && e.Type != "" && ne.Type != e.Type,
			})
			e.Assertions = append(e.Assertions, ne.Assertions...)
			// Union observations and their sources in lockstep.
			e.Observations, e.ObsSources = unionObs(
				e.Observations, e.ObsSources, ne.Observations, ne.ObsSources)
			if e.Type == "" {
				e.Type = ne.Type
			}
			// Fold the new entity's name and aliases into the canonical node so it
			// stays discoverable under all its names.
			e.Aliases = mergeAliases(e.Name, e.Aliases, ne)
			// Embedding: latest non-empty wins, so the freshest vector (from the
			// most recently indexed session) is kept.
			if len(ne.Embedding) > 0 {
				e.Embedding = ne.Embedding
			}
			NormalizeEntityAssertions(e)
			// Register any newly-added keys so future entities collide with them.
			register(target)
		} else {
			ne.Observations, ne.ObsSources = unionObs(nil, nil, ne.Observations, ne.ObsSources)
			NormalizeEntityAssertions(&ne)
			ne.Aliases = mergeAliases(ne.Name, nil, ne)
			g.Entities = append(g.Entities, ne)
			register(len(g.Entities) - 1)
		}
	}

	// Resolve relation endpoints through the same name/alias index used for
	// entities. This prevents alias spellings from creating parallel edges and
	// drops model hallucinations that point at entities which were not emitted.
	canonical := make(map[string]string, len(g.Entities)*2)
	for _, e := range g.Entities {
		for _, k := range entityKeys(e) {
			canonical[k] = e.Name
		}
	}
	resolve := func(name string) (string, bool) {
		c, ok := canonical[normKey(name)]
		return c, ok
	}

	// De-dupe relations by (from,to,label), normalized.
	positions := make(map[string]int, len(g.Relations))
	clean := make([]Relation, 0, len(g.Relations)+len(relations))
	for _, r := range append(g.Relations, relations...) {
		from, fromOK := resolve(r.From)
		to, toOK := resolve(r.To)
		if !fromOK || !toOK || from == to {
			continue
		}
		r.From, r.To = from, to
		r = NormalizeRelation(r)
		k := relKey(r)
		if pos, exists := positions[k]; exists {
			clean[pos] = MergeRelationTrust(clean[pos], r)
			continue
		}
		positions[k] = len(clean)
		clean = append(clean, r)
	}
	g.Relations = clean
	return report
}

// sharedAliasIsDesignation reports whether key is at least one entity's own
// designation: its whole name, or a part of it. Used to decide whether a
// collision between two aliases identifies the same thing.
//
// The distinction it draws: "paymentservice" is one entity's actual name and
// merely an alias of the other, so the two are plausibly the same thing; whereas
// "发布分支" is nobody's name — release/2.1.0 and release/3.2.1 both merely label
// themselves with it, and it is a category, not an identity.
//
// Containment rather than equality, because names are routinely qualified
// ("pay.PaymentService", "jewel.member_org") while the alias is the bare form.
// Comparison drops separators, so the identifier "asset_info" counts as the
// designation behind the alias "assetinfo" — spelling the same name with an
// underscore does not make it a different name.
func sharedAliasIsDesignation(key string, names ...string) bool {
	k := designationKey(key)
	if k == "" {
		return false
	}
	for _, n := range names {
		if d := designationKey(n); d != "" && strings.Contains(d, k) {
			return true
		}
	}
	return false
}

// designationKey reduces a name to its letters and digits, lowercased, so
// separator and spacing differences ("asset_info", "assetInfo", "asset info")
// compare equal when deciding what an alias designates.
func designationKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}

// entityKeys returns the normalized lookup keys for an entity: its name plus
// every alias, empties dropped.
func entityKeys(e Entity) []string {
	out := make([]string, 0, 1+len(e.Aliases))
	if k := normKey(e.Name); k != "" {
		out = append(out, k)
	}
	for _, a := range e.Aliases {
		if k := normKey(a); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// mergeAliases builds the alias set for a canonical entity named canonical: the
// existing aliases, plus the incoming entity's name and aliases, de-duplicated
// case-insensitively and with the canonical name itself excluded (it is not its
// own alias). Order is preserved.
func mergeAliases(canonical string, existing []string, ne Entity) []string {
	canon := normKey(canonical)
	seen := map[string]bool{canon: true}
	out := make([]string, 0, len(existing)+1+len(ne.Aliases))
	add := func(s string) {
		k := normKey(s)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, a := range existing {
		add(a)
	}
	add(ne.Name)
	for _, a := range ne.Aliases {
		add(a)
	}
	return out
}

func relKey(r Relation) string {
	return normKey(r.From) + "\x00" + normKey(r.To) + "\x00" + normKey(r.Label)
}

// unionObs appends observations from addObs to baseObs, skipping empties and
// case-insensitive duplicates, keeping a parallel source slice aligned. The
// source of an added observation is addSrc[i] when present, otherwise "".
// baseSrc is normalized to the same length as baseObs first (older caches may
// carry no sources). Order is preserved.
func unionObs(baseObs, baseSrc, addObs, addSrc []string) (obs, src []string) {
	seen := make(map[string]bool, len(baseObs)+len(addObs))
	obs = make([]string, 0, len(baseObs)+len(addObs))
	src = make([]string, 0, len(baseObs)+len(addObs))
	emit := func(o, s string) {
		k := normKey(o)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		obs = append(obs, o)
		src = append(src, s)
	}
	for i, o := range baseObs {
		emit(o, at(baseSrc, i))
	}
	for i, o := range addObs {
		emit(o, at(addSrc, i))
	}
	return obs, src
}

// at returns s[i] or "" when i is out of range (parallel-array safety for older
// caches whose source slice is shorter than or missing next to observations).
func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}
