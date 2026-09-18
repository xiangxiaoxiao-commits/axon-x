package graph

import (
	"strings"
	"time"
	"unicode"
)

// AssertionConflictIndexes conservatively detects direct positive/negative
// contradictions inside one entity. It intentionally requires the remaining
// normalized text to be identical, avoiding speculative semantic judgments.
func AssertionConflictIndexes(e Entity) map[int]bool {
	return assertionConflictIndexesAt(e, time.Now())
}

func assertionConflictIndexesAt(e Entity, now time.Time) map[int]bool {
	NormalizeEntityAssertions(&e)
	type polarity struct {
		positive []int
		negative []int
	}
	groups := map[string]*polarity{}
	for i, a := range e.Assertions {
		// A weak candidate, rejected fact or expired snapshot must not silence a
		// currently trusted assertion merely because its wording is opposite.
		if !AssertionUsable(a, now) {
			continue
		}
		skeleton, negative := assertionPolaritySkeleton(a.Text)
		if len([]rune(skeleton)) < 4 {
			continue
		}
		p := groups[skeleton]
		if p == nil {
			p = &polarity{}
			groups[skeleton] = p
		}
		if negative {
			p.negative = append(p.negative, i)
		} else {
			p.positive = append(p.positive, i)
		}
	}
	out := map[int]bool{}
	for _, p := range groups {
		if len(p.positive) == 0 || len(p.negative) == 0 {
			continue
		}
		for _, i := range append(p.positive, p.negative...) {
			out[i] = true
		}
	}
	return out
}

// UsableAssertions is the entity-level factual gate. Expired, unreviewed and
// directly conflicting assertions remain auditable but cannot enter answers.
func UsableAssertions(e Entity, now time.Time) []Assertion {
	NormalizeEntityAssertions(&e)
	conflicts := assertionConflictIndexesAt(e, now)
	out := make([]Assertion, 0, len(e.Assertions))
	for i, a := range e.Assertions {
		if !conflicts[i] && AssertionUsable(a, now) {
			out = append(out, a)
		}
	}
	return out
}

func assertionPolaritySkeleton(text string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(text))
	negative := false
	// Map an explicit negative phrase to its positive counterpart. This keeps
	// the semantic verb in the skeleton ("不允许" -> "允许") so only a direct
	// polarity pair can collide.
	for _, pair := range [][2]string{
		{"不允许", "允许"}, {"不得", "可以"}, {"不能", "能"}, {"不会", "会"},
		{"无需", "需要"}, {"没有", "有"}, {"禁止", "允许"},
		{"must not", "must"}, {"should not", "should"}, {"does not", "does"},
		{"do not", "do"}, {"cannot", "can"}, {" not ", " "},
	} {
		if strings.Contains(s, pair[0]) {
			negative = true
			s = strings.ReplaceAll(s, pair[0], pair[1])
		}
	}
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, s)
	return s, negative
}
