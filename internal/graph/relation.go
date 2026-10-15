package graph

import "strings"

const (
	RelationAsserted  = "asserted"
	RelationCandidate = "candidate"
	RelationVerified  = "verified"
	RelationRejected  = "rejected"
)

// NormalizeRelation fills trust metadata while remaining compatible with old
// graph/cache JSON. Old inferred edges become candidates; old solid edges stay
// asserted. It also clamps confidence and removes duplicate provenance text.
func NormalizeRelation(r Relation) Relation {
	r.From = strings.TrimSpace(r.From)
	r.To = strings.TrimSpace(r.To)
	r.Label = strings.TrimSpace(r.Label)
	r.State = strings.ToLower(strings.TrimSpace(r.State))
	switch r.State {
	case RelationAsserted, RelationCandidate, RelationVerified, RelationRejected:
	default:
		if r.Inferred {
			r.State = RelationCandidate
		} else {
			r.State = RelationAsserted
		}
	}
	if r.State == RelationCandidate {
		r.Inferred = true
	} else if r.State == RelationAsserted || r.State == RelationVerified {
		r.Inferred = false
	}
	if r.Confidence <= 0 {
		switch r.State {
		case RelationVerified:
			r.Confidence = 1
		case RelationCandidate:
			r.Confidence = 0.45
		default:
			r.Confidence = 0.85
		}
	}
	if r.Confidence > 1 {
		r.Confidence = 1
	}
	r.Sources = uniqueTrimmed(r.Sources)
	r.Evidence = uniqueTrimmed(r.Evidence)
	return r
}

// RelationUsableForRecall is the factual-recall gate. Candidate and rejected
// edges can remain visible/auditable but never pull neighboring entities into
// an answer. A low-confidence asserted edge is also withheld until improved.
func RelationUsableForRecall(r Relation) bool {
	r = NormalizeRelation(r)
	return (r.State == RelationAsserted || r.State == RelationVerified) && r.Confidence >= 0.65
}

// StampRelationProvenance attaches a source to every relation and folds each
// label onto the schema vocabulary (see schema.go). A folded label keeps its
// original text as evidence, so unifying the traversal key never discards the
// specialisation the model expressed: "包含字段" becomes 包含 with evidence
// "包含字段", which reads the same but now joins the 包含 traversal.
//
// It is idempotent: re-running with the same source adds no duplicate source
// (NormalizeRelation dedupes) and no duplicate evidence (an already-canonical
// label reports changed=false, so nothing is appended).
func StampRelationProvenance(relations []Relation, source string) {
	for i := range relations {
		if canonical, original, changed := CanonicalRelationLabel(relations[i].Label); changed {
			relations[i].Label = canonical
			relations[i].Evidence = append(relations[i].Evidence, original)
		}
		if source != "" {
			relations[i].Sources = append(relations[i].Sources, source)
		}
		relations[i] = NormalizeRelation(relations[i])
	}
}

// CacheSourceCodePrefix marks a cache key produced by static code analysis, as
// opposed to conversation distillation or an imported bundle. Like
// ImportSourcePrefix it is a naming convention on the cache key that carries the
// knowledge's origin, so trust can be graded by source rather than assumed.
const CacheSourceCodePrefix = "code:"

// RelationTrustForCache returns the trust grade relations from a cache should
// carry, based on where that knowledge came from:
//
//   - Static code analysis (import / contains / calls) reads the syntax tree, so
//     the fact is machine-checked: verified at 1.00.
//   - Conversation distillation is a model reading a transcript and may misread,
//     so it stays asserted at 0.85.
//   - An imported bundle keeps whatever grade its author recorded — re-grading
//     someone else's reviewed knowledge on import would be presumptuous, and the
//     zero values signal "leave it alone".
//
// This mirrors the data-source trust table in the architecture notes, which was
// documented but never actually applied: every cache was stamped asserted/0.85
// regardless of origin, so code-derived relations sat below their own declared
// trust level and the verified tier never held a single row.
func RelationTrustForCache(sessionID string) (state string, confidence float64, ok bool) {
	id := strings.TrimSpace(sessionID)
	switch {
	case strings.HasPrefix(id, CacheSourceCodePrefix):
		return RelationVerified, 1, true
	case strings.HasPrefix(id, ImportSourcePrefix):
		return "", 0, false
	default:
		return RelationAsserted, 0.85, true
	}
}

// StampRelationTrust overwrites the trust grade of every relation. Used on
// ingestion paths that know the source's grade; NormalizeRelation still clamps
// the values and fills any missing defaults.
func StampRelationTrust(relations []Relation, state string, confidence float64) {
	for i := range relations {
		if strings.TrimSpace(state) != "" {
			relations[i].State = state
		}
		if confidence > 0 {
			relations[i].Confidence = confidence
		}
		relations[i] = NormalizeRelation(relations[i])
	}
}

func uniqueTrimmed(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		key := strings.ToLower(s)
		if s == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func relationStateRank(state string) int {
	switch state {
	case RelationVerified:
		return 4
	case RelationAsserted:
		return 3
	case RelationCandidate:
		return 2
	case RelationRejected:
		return 1
	default:
		return 0
	}
}

// MergeRelationTrust combines corroborating copies of the same edge. Evidence
// and sources accumulate, confidence keeps the strongest observation, and a
// stronger lifecycle state wins (verified > asserted > candidate > rejected).
func MergeRelationTrust(base, incoming Relation) Relation {
	base = NormalizeRelation(base)
	incoming = NormalizeRelation(incoming)
	if relationStateRank(incoming.State) > relationStateRank(base.State) {
		base.State = incoming.State
		base.Inferred = incoming.Inferred
	}
	if incoming.Confidence > base.Confidence {
		base.Confidence = incoming.Confidence
	}
	base.Sources = uniqueTrimmed(append(base.Sources, incoming.Sources...))
	base.Evidence = uniqueTrimmed(append(base.Evidence, incoming.Evidence...))
	return NormalizeRelation(base)
}
