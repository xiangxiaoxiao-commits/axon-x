package main

import (
	"sort"
	"strings"
	"unicode"

	"axon/internal/embed"
	"axon/internal/graph"
)

// This file de-pollutes what remember_knowledge writes.
//
// The read-time graph.Merge only folds entities that share a normalized name or
// alias. Repeated agent writes routinely name the same concept differently
// ("支付幂等" vs "支付回调幂等") with no shared alias, so they pile up as separate
// nodes and the graph rots. When a real semantic embedder is available, we can
// catch these before the write: cosine-match each incoming entity against the
// existing graph, and above a threshold relabel it to the existing canonical name
// (keeping its own name as an alias) so read-time Merge folds it correctly. We
// also drop incoming observations that are near-duplicates of ones already on the
// matched entity, so a hot node's fact list stops bloating with reworded repeats.

// entityMergeThreshold is the cosine floor for treating an incoming entity as the
// same thing as an existing one. Entity vectors embed name+observations, so
// near-identical concepts score very high; this is deliberately strict to avoid
// collapsing two genuinely distinct-but-related entities.
const entityMergeThreshold = 0.86

// obsDupThreshold is the token-set Jaccard floor above which an incoming
// observation is considered a reworded duplicate of an existing one.
const obsDupThreshold = 0.8

// resolveEntities relabels incoming entities onto existing canonical names when
// their embeddings are near-identical, and strips observations that duplicate
// (exactly or by heavy token overlap) ones already on the matched entity. It is
// pure over its inputs (no I/O) so it can be unit-tested directly; the caller
// gates it on having a real semantic embedder, since the local lexical embedder's
// surface-overlap vectors add nothing beyond what alias matching already catches.
//
// Only entities that carry an Embedding participate in cosine matching; the rest
// pass through unchanged (still subject to lexical Merge downstream).
func resolveEntities(existing []graph.Entity, incoming []graph.Entity) []graph.Entity {
	out := make([]graph.Entity, 0, len(incoming))
	for _, ne := range incoming {
		best, ok := bestEntityMatch(existing, ne)
		if ok && graph.NormName(best.Name) != graph.NormName(ne.Name) {
			// Fold onto the existing node: adopt its canonical name and keep the
			// incoming name as an alias so it stays discoverable under both.
			ne.Aliases = appendAlias(ne.Aliases, ne.Name)
			ne.Name = best.Name
		}
		if ok {
			ne.Observations, ne.ObsSources = dropDupObservations(best.Observations, ne.Observations, ne.ObsSources)
		}
		if len(ne.Observations) == 0 {
			// Every fact was already known: nothing durable to add. Skip the entity
			// entirely rather than writing a fact-less node.
			continue
		}
		out = append(out, ne)
	}
	return out
}

// bestEntityMatch returns the existing entity most cosine-similar to ne, if the
// best score clears entityMergeThreshold. Both must carry embeddings.
func bestEntityMatch(existing []graph.Entity, ne graph.Entity) (graph.Entity, bool) {
	if len(ne.Embedding) == 0 {
		return graph.Entity{}, false
	}
	var best graph.Entity
	var bestScore float32
	found := false
	for _, e := range existing {
		if len(e.Embedding) == 0 {
			continue
		}
		if s := embed.Cosine(ne.Embedding, e.Embedding); s > bestScore {
			bestScore, best, found = s, e, true
		}
	}
	if found && bestScore >= entityMergeThreshold {
		return best, true
	}
	return graph.Entity{}, false
}

// dropDupObservations returns the incoming observations (and aligned sources)
// with any that are near-duplicates of an existing observation removed. Exact
// case-insensitive matches and heavy token overlap (>= obsDupThreshold) both
// count as duplicates. graph.Merge already drops exact dups at read time; this
// additionally catches rewordings before they ever land on disk.
func dropDupObservations(existingObs, obs, src []string) (keptObs, keptSrc []string) {
	existTokens := make([]map[string]bool, len(existingObs))
	for i, o := range existingObs {
		existTokens[i] = tokenSet(o)
	}
	for i, o := range obs {
		if isDupObservation(tokenSet(o), existTokens) {
			continue
		}
		keptObs = append(keptObs, o)
		keptSrc = append(keptSrc, at(src, i))
	}
	return keptObs, keptSrc
}

// isDupObservation reports whether an observation's token set is a near-duplicate
// of any existing observation's token set.
func isDupObservation(t map[string]bool, existing []map[string]bool) bool {
	if len(t) == 0 {
		return false
	}
	for _, e := range existing {
		if jaccard(t, e) >= obsDupThreshold {
			return true
		}
	}
	return false
}

// jaccard is the token-set Jaccard similarity |A∩B| / |A∪B| in [0,1].
func jaccard(a, b map[string]bool) float32 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float32(inter) / float32(union)
}

// tokenSet splits text into a set of comparison tokens: runs of letters/digits
// become word tokens, while CJK ideographs are split per-character (they carry no
// spaces). Lowercased; punctuation and whitespace are dropped. This gives a
// language-agnostic surface for near-duplicate detection.
func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	var word []rune
	flush := func() {
		if len(word) > 0 {
			out[string(word)] = true
			word = word[:0]
		}
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.Is(unicode.Han, r):
			flush()
			out[string(r)] = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// at returns s[i] or "" when i is out of range (parallel-array safety when an
// observation has no aligned source).
func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return ""
}

// appendAlias adds name to aliases if not already present (case-insensitive).
func appendAlias(aliases []string, name string) []string {
	nk := graph.NormName(name)
	if nk == "" {
		return aliases
	}
	for _, a := range aliases {
		if graph.NormName(a) == nk {
			return aliases
		}
	}
	return append(aliases, name)
}

// sortedTokens is a test/debug helper: the token set as a sorted slice.
func sortedTokens(s string) []string {
	set := tokenSet(s)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
