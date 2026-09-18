package graph

import (
	"strings"
	"time"
)

// Assertion is one independently governable fact about an entity.
type Assertion struct {
	Text       string   `json:"text"`
	State      string   `json:"state,omitempty"`      // asserted | candidate | verified | rejected
	Confidence float64  `json:"confidence,omitempty"` // 0..1
	Sources    []string `json:"sources,omitempty"`
	ObservedAt int64    `json:"observedAt,omitempty"` // unix millis; when Axon learned it
	ValidUntil int64    `json:"validUntil,omitempty"` // unix millis; 0 means no declared expiry
}

func NormalizeAssertion(a Assertion) Assertion {
	a.Text = strings.TrimSpace(a.Text)
	a.State = strings.ToLower(strings.TrimSpace(a.State))
	switch a.State {
	case RelationAsserted, RelationCandidate, RelationVerified, RelationRejected:
	default:
		a.State = RelationAsserted
	}
	if a.Confidence <= 0 {
		if a.State == RelationVerified {
			a.Confidence = 1
		} else if a.State == RelationCandidate {
			a.Confidence = 0.45
		} else {
			a.Confidence = 0.85
		}
	}
	if a.Confidence > 1 {
		a.Confidence = 1
	}
	a.Sources = uniqueTrimmed(a.Sources)
	return a
}

func AssertionUsable(a Assertion, now time.Time) bool {
	a = NormalizeAssertion(a)
	if a.State != RelationAsserted && a.State != RelationVerified {
		return false
	}
	if a.Confidence < 0.65 {
		return false
	}
	return a.ValidUntil == 0 || now.UnixMilli() <= a.ValidUntil
}

func MergeAssertionTrust(base, incoming Assertion) Assertion {
	base = NormalizeAssertion(base)
	incoming = NormalizeAssertion(incoming)
	if relationStateRank(incoming.State) > relationStateRank(base.State) {
		base.State = incoming.State
	}
	if incoming.Confidence > base.Confidence {
		base.Confidence = incoming.Confidence
	}
	base.Sources = uniqueTrimmed(append(base.Sources, incoming.Sources...))
	if incoming.ObservedAt > base.ObservedAt {
		base.ObservedAt = incoming.ObservedAt
	}
	if base.ValidUntil == 0 || (incoming.ValidUntil > 0 && incoming.ValidUntil < base.ValidUntil) {
		base.ValidUntil = incoming.ValidUntil
	}
	return NormalizeAssertion(base)
}

// NormalizeEntityAssertions upgrades legacy observation arrays into governed
// assertions and regenerates the arrays as a compatibility projection.
func NormalizeEntityAssertions(e *Entity) {
	if e == nil {
		return
	}
	byText := make(map[string]int, len(e.Assertions)+len(e.Observations))
	out := make([]Assertion, 0, len(e.Assertions)+len(e.Observations))
	add := func(a Assertion) {
		a = NormalizeAssertion(a)
		key := strings.ToLower(a.Text)
		if key == "" {
			return
		}
		if i, ok := byText[key]; ok {
			out[i] = MergeAssertionTrust(out[i], a)
			return
		}
		byText[key] = len(out)
		out = append(out, a)
	}
	for _, a := range e.Assertions {
		add(a)
	}
	for i, text := range e.Observations {
		// When modern Assertions already contain this text, Observations is only
		// their compatibility projection. Never merge its legacy default trust
		// back into the governed assertion (that would promote candidates on
		// every normalization pass).
		if _, exists := byText[strings.ToLower(strings.TrimSpace(text))]; exists {
			continue
		}
		var sources []string
		if i < len(e.ObsSources) && strings.TrimSpace(e.ObsSources[i]) != "" {
			sources = []string{e.ObsSources[i]}
		}
		add(Assertion{Text: text, State: RelationAsserted, Confidence: 0.85, Sources: sources})
	}
	e.Assertions = out
	e.Observations = make([]string, 0, len(out))
	e.ObsSources = make([]string, 0, len(out))
	for _, a := range out {
		e.Observations = append(e.Observations, a.Text)
		if len(a.Sources) > 0 {
			e.ObsSources = append(e.ObsSources, a.Sources[0])
		} else {
			e.ObsSources = append(e.ObsSources, "")
		}
	}
}

// StampEntityAssertions attaches ingestion provenance/trust to every assertion
// currently projected by an entity. It is shared by session, code, task, MCP
// and manual ingestion paths so all sources produce the same governed model.
func StampEntityAssertions(entities []Entity, source string, observedAt int64, state string, confidence float64) {
	for i := range entities {
		NormalizeEntityAssertions(&entities[i])
		for j := range entities[i].Assertions {
			a := &entities[i].Assertions[j]
			a.Sources = append(a.Sources, source)
			if observedAt > a.ObservedAt {
				a.ObservedAt = observedAt
			}
			if strings.TrimSpace(state) != "" {
				a.State = state
			}
			if confidence > 0 {
				a.Confidence = confidence
			}
			*a = NormalizeAssertion(*a)
		}
		NormalizeEntityAssertions(&entities[i])
	}
}
