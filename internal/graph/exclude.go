package graph

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReviewEvent struct {
	Kind             string `json:"kind"` // assertion | relation
	Key              string `json:"key"`
	Description      string `json:"description"`
	PreviousState    string `json:"previousState,omitempty"`
	PreviousExcluded bool   `json:"previousExcluded,omitempty"`
	NewState         string `json:"newState"`
	At               int64  `json:"at"`
	Undone           bool   `json:"undone,omitempty"`
}

// Exclusions is the per-project set of observations the user has chosen to keep
// OUT of the assembled graph. It survives re-indexing: assembleGraph applies it
// after merging caches, so a deleted fact never comes back even though the
// source SessionCache still contains it.
//
// Keying is by content (entity name + observation text), not by index or
// session, so "I don't want this fact" holds globally — if several sessions
// distilled the same fact, excluding it once removes it everywhere.
type Exclusions struct {
	Obs              map[string]bool   `json:"obs"`                        // set of ObsKey -> true (single facts)
	Sessions         map[string]bool   `json:"sessions,omitempty"`         // set of session id -> true (whole sessions)
	AssertionReviews map[string]string `json:"assertionReviews,omitempty"` // ObsKey -> verified|rejected|candidate
	RelationReviews  map[string]string `json:"relationReviews,omitempty"`  // RelationKey -> verified|rejected|candidate
	ReviewHistory    []ReviewEvent     `json:"reviewHistory,omitempty"`
}

// ObsKey is the stable identity of an observation: sha1 of the normalized
// entity name + the trimmed observation text.
func ObsKey(entityName, obsText string) string {
	h := sha1.Sum([]byte(normKey(entityName) + "\x00" + strings.TrimSpace(obsText)))
	return hex.EncodeToString(h[:])
}

// RecordReview appends an auditable governance decision while keeping enough
// prior state to reverse an accidental action.
func (ex *Exclusions) RecordReview(kind, key, description, state string) {
	previous := ""
	if kind == "assertion" {
		previous = ex.AssertionReviews[key]
	} else {
		previous = ex.RelationReviews[key]
	}
	ex.ReviewHistory = append(ex.ReviewHistory, ReviewEvent{
		Kind: kind, Key: key, Description: description, PreviousState: previous,
		PreviousExcluded: ex.Obs[key], NewState: state, At: time.Now().UnixMilli(),
	})
}

// UndoLatestReview restores the most recent active governance decision and
// marks its audit event undone instead of deleting history.
func (ex *Exclusions) UndoLatestReview() (string, bool) {
	for i := len(ex.ReviewHistory) - 1; i >= 0; i-- {
		event := &ex.ReviewHistory[i]
		if event.Undone {
			continue
		}
		var reviews map[string]string
		if event.Kind == "assertion" {
			reviews = ex.AssertionReviews
			if event.PreviousExcluded {
				ex.Obs[event.Key] = true
			} else {
				delete(ex.Obs, event.Key)
			}
		} else {
			reviews = ex.RelationReviews
		}
		if event.PreviousState == "" {
			delete(reviews, event.Key)
		} else {
			reviews[event.Key] = event.PreviousState
		}
		event.Undone = true
		return event.Description, true
	}
	return "", false
}

func (ex *Exclusions) UndoableReviews() int {
	count := 0
	for _, event := range ex.ReviewHistory {
		if !event.Undone {
			count++
		}
	}
	return count
}

// RelationKey is the stable identity of a directed, labelled relationship.
// It deliberately excludes confidence/evidence so a review survives later
// re-extraction of the same semantic edge with richer metadata.
func RelationKey(from, to, label string) string {
	h := sha1.Sum([]byte(normKey(from) + "\x00" + normKey(to) + "\x00" + normKey(label)))
	return hex.EncodeToString(h[:])
}

// exclusionsPath stores the list OUTSIDE the cache dir (which LoadAllCache scans
// for *.json and would try to parse as a SessionCache). It lives beside the
// slug's cache dir as "<slug>.exclusions.json" under graphcache/.
func exclusionsPath(dataDir, slug string) string {
	return filepath.Join(dataDir, "graphcache", filepath.Base(slug)+".exclusions.json")
}

// LoadExclusions returns the project's exclusion set (empty, never nil, if none).
func LoadExclusions(dataDir, slug string) (*Exclusions, error) {
	ex := &Exclusions{Obs: map[string]bool{}, Sessions: map[string]bool{}, AssertionReviews: map[string]string{}, RelationReviews: map[string]string{}}
	data, err := os.ReadFile(exclusionsPath(dataDir, slug))
	if err != nil {
		if os.IsNotExist(err) {
			return ex, nil
		}
		return ex, err
	}
	_ = json.Unmarshal(data, ex)
	if ex.Obs == nil {
		ex.Obs = map[string]bool{}
	}
	if ex.Sessions == nil {
		ex.Sessions = map[string]bool{}
	}
	if ex.RelationReviews == nil {
		ex.RelationReviews = map[string]string{}
	}
	if ex.AssertionReviews == nil {
		ex.AssertionReviews = map[string]string{}
	}
	return ex, nil
}

// SaveExclusions atomically writes the exclusion set (temp file + rename).
func SaveExclusions(dataDir, slug string, ex *Exclusions) error {
	p := exclusionsPath(dataDir, slug)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ex, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// FilterExcluded removes every excluded observation (and its parallel source)
// from g in place. An entity left with zero observations is dropped entirely,
// along with any relation that referenced it.
func FilterExcluded(g *Graph, ex *Exclusions) {
	if ex == nil || (len(ex.Obs) == 0 && len(ex.Sessions) == 0 && len(ex.AssertionReviews) == 0 && len(ex.RelationReviews) == 0) {
		return
	}
	keptEnts := g.Entities[:0]
	dropped := map[string]bool{} // normKey of entities removed for being empty
	for _, e := range g.Entities {
		NormalizeEntityAssertions(&e)
		hadAssertions := len(e.Assertions) > 0
		assertions := e.Assertions[:0]
		for _, assertion := range e.Assertions {
			assertionKey := ObsKey(e.Name, assertion.Text)
			if ex.Obs[assertionKey] {
				continue // single fact excluded
			}
			if len(assertion.Sources) > 0 && len(ex.Sessions) > 0 {
				activeSources := assertion.Sources[:0]
				for _, source := range assertion.Sources {
					if !ex.Sessions[source] {
						activeSources = append(activeSources, source)
					}
				}
				if len(activeSources) == 0 {
					continue
				}
				assertion.Sources = activeSources
			}
			switch ex.AssertionReviews[assertionKey] {
			case RelationRejected:
				continue
			case RelationVerified:
				assertion.State = RelationVerified
				assertion.Confidence = 1
			case RelationCandidate:
				assertion.State = RelationCandidate
				if assertion.Confidence <= 0 || assertion.Confidence > 0.6 {
					assertion.Confidence = 0.45
				}
			}
			assertions = append(assertions, NormalizeAssertion(assertion))
		}
		e.Assertions = assertions
		e.Observations = nil
		e.ObsSources = nil
		NormalizeEntityAssertions(&e)
		if hadAssertions && len(e.Assertions) == 0 {
			dropped[normKey(e.Name)] = true
			continue
		}
		keptEnts = append(keptEnts, e)
	}
	g.Entities = keptEnts

	rels := g.Relations[:0]
	for _, r := range g.Relations {
		if dropped[normKey(r.From)] || dropped[normKey(r.To)] {
			continue
		}
		if len(r.Sources) > 0 && len(ex.Sessions) > 0 {
			activeSources := r.Sources[:0]
			for _, source := range r.Sources {
				if !ex.Sessions[source] {
					activeSources = append(activeSources, source)
				}
			}
			if len(activeSources) == 0 {
				continue
			}
			r.Sources = activeSources
		}
		switch ex.RelationReviews[RelationKey(r.From, r.To, r.Label)] {
		case RelationRejected:
			continue
		case RelationVerified:
			r.State = RelationVerified
			r.Inferred = false
			r.Confidence = 1
		case RelationCandidate:
			r.State = RelationCandidate
			r.Inferred = true
			if r.Confidence <= 0 || r.Confidence > 0.6 {
				r.Confidence = 0.45
			}
		}
		rels = append(rels, NormalizeRelation(r))
	}
	g.Relations = rels
}

// obsSourceAt safely reads ObsSources[i] (empty when shorter/absent).
func obsSourceAt(src []string, i int) string {
	if i < len(src) {
		return src[i]
	}
	return ""
}
