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
