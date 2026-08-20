// Package retrieve holds the App-independent core of knowledge recall: given a
// project's assembled graph plus its verbatim chunks and a query, it runs the
// two-channel HybridRAG search (entity structure + raw chunks) and fuses each
// channel with Reciprocal Rank Fusion. It is shared by the Wails App
// (MatchKnowledge) and the standalone MCP server so neither re-implements RRF,
// cosine, or the seed/expand logic.
package retrieve

import (
	"sort"
	"strings"

	"axon/internal/embed"
	"axon/internal/graph"
)

// HybridRAG tuning. Kept next to the algorithm so both callers share one set of
// knobs.
const (
	// seedMinScore is the cloud-embedding cosine floor for an entity to be picked
	// as a semantic seed; seedTopK caps how many seeds are taken; expandHops is
	// how far to walk relations out from the seeds.
	seedMinScore = 0.30
	seedTopK     = 8
	expandHops   = 1

	// chunkRecallCandidates caps vector candidates before fusion; chunkInjectTopN
	// caps chunks after fusion; chunkMinScore is the cloud-embedding cosine floor
	// for a chunk.
	chunkRecallCandidates = 30
	chunkInjectTopN       = 5
	chunkMinScore         = 0.30

	// LocalSeedMinScore / LocalChunkMinScore are the cosine floors used when the
	// query was embedded by the local lexical fallback (LocalEmbedder). Its
	// character-n-gram/hashing vectors sit on a naturally lower cosine scale than
	// cloud neural embeddings — relevant content lands around 0.20 rather than
	// 0.40+ — so the standard 0.30 floors would silently filter every real hit.
	// Lower floors let those lexical matches through without touching cloud recall.
	LocalSeedMinScore  = 0.12
	LocalChunkMinScore = 0.12

	// rrfK is the Reciprocal Rank Fusion smoothing constant (industry-standard 60).
	rrfK = 60
)

// RecallOpts carries the embedder-dependent cosine floors for a recall run.
// SeedMinScore gates semantic entity seeds; ChunkMinScore gates raw chunks.
type RecallOpts struct {
	SeedMinScore  float32
	ChunkMinScore float32
}

// DefaultRecallOpts are the cloud-embedding floors (0.30 / 0.30), matching the
// original behavior.
func DefaultRecallOpts() RecallOpts {
	return RecallOpts{SeedMinScore: seedMinScore, ChunkMinScore: chunkMinScore}
}

// RecallOptsFor returns the cosine floors appropriate for the embedder that
// produced the query vector: the local lexical fallback uses the lower
// Local*MinScore floors, while cloud embeddings keep the standard 0.30 floors.
func RecallOptsFor(local bool) RecallOpts {
	if local {
		return RecallOpts{SeedMinScore: LocalSeedMinScore, ChunkMinScore: LocalChunkMinScore}
	}
	return DefaultRecallOpts()
}

// Result is the raw outcome of recall, before any caller-specific rendering.
// Hit is the fused, relation-expanded set of matched entity names (lowercased,
// as graph lookup keys). SemanticSeeds / KeywordHits are the canonical entity
// names each channel produced (for diagnostics and the recall-method signal).
// Chunks are the top fused verbatim fragments from the raw-context channel.
type Result struct {
	Hit           map[string]bool
	SemanticSeeds []string
	KeywordHits   []string
	Chunks        []graph.Chunk
}

// Recall runs the two-channel search over an already-assembled graph and its
// chunk set. qv is the query embedding (may be empty, in which case both vector
// paths go dark and recall degrades to substring matching over entities). text
// is the raw query, used for the keyword channels. chunks are the project's
// embedded verbatim fragments (pass nil to skip the raw-context channel).
func Recall(g *graph.Graph, chunks []graph.Chunk, qv []float32, text string) Result {
	return RecallWithOpts(g, chunks, qv, text, DefaultRecallOpts())
}

// RecallWithOpts is Recall with caller-supplied cosine floors so the thresholds
// can adapt to the embedder that produced qv (see RecallOptsFor). Recall keeps
// the original signature and calls this with the cloud defaults.
func RecallWithOpts(g *graph.Graph, chunks []graph.Chunk, qv []float32, text string, opts RecallOpts) Result {
	// --- Structure channel: semantic seeds + substring hits, RRF-fused, then
	// expanded one hop along relations. ---
	semanticSeeds := semanticSeeds(qv, g, opts.SeedMinScore)
	keywordHits := EntityKeywordHits(g, text)
	hit := map[string]bool{}
	for _, id := range RRFFuse(lowerAll(semanticSeeds), lowerAll(keywordHits)) {
		hit[id] = true
	}
	if len(semanticSeeds) > 0 {
		expandAlongRelations(g, hit, expandHops)
	}

	// --- Raw-context channel: vector recall + keyword hits, RRF-fused. Only runs
	// when the query embedded (otherwise there is no vector to rank chunks by). ---
	var chunkRanked []graph.Chunk
	if len(qv) > 0 {
		candidates := rankChunks(qv, chunks, opts.ChunkMinScore)
		kwChunks := chunkKeywordHits(chunks, text)
		chunkRanked = fuseChunks(candidates, kwChunks)
	}

	return Result{
		Hit:           hit,
		SemanticSeeds: semanticSeeds,
		KeywordHits:   keywordHits,
		Chunks:        chunkRanked,
	}
}

// AssembleGraph merges every cached session's distilled knowledge for a project
// into one in-memory graph. Unlike the App's assembleGraph it is read-only: it
// never writes the merged graph back to disk, which is what a stateless MCP
// query wants. Returns an empty graph when the project has no cache yet.
func AssembleGraph(dataDir, projectSlug string) (*graph.Graph, error) {
	caches, err := graph.LoadAllCache(dataDir, projectSlug)
	if err != nil {
		return nil, err
	}
	g := &graph.Graph{ProjectSlug: projectSlug, Entities: []graph.Entity{}, Relations: []graph.Relation{}}
	for _, c := range caches {
		g.Merge(c.Entities, c.Relations)
	}
	return g, nil
}

// LoadChunks gathers every embedded chunk across a project's caches into one
// flat slice for vector recall. Chunks without an embedding are skipped (they
// can't be semantically matched).
func LoadChunks(dataDir, projectSlug string) []graph.Chunk {
	caches, err := graph.LoadAllCache(dataDir, projectSlug)
	if err != nil {
		return nil
	}
	var out []graph.Chunk
	for _, c := range caches {
		for _, ch := range c.Chunks {
			if len(ch.Embedding) > 0 {
				out = append(out, ch)
			}
		}
	}
	return out
}

// semanticSeeds returns the names of the top-K entities whose embedding is most
// similar to the query vector qv (cosine >= minScore), best first. Returns
// nil when qv is empty or no entity carries a vector, so the caller falls back
// to substring matching without erroring.
func semanticSeeds(qv []float32, g *graph.Graph, minScore float32) []string {
	if len(qv) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score float32
	}
	var cands []scored
	for _, e := range g.Entities {
		if len(e.Embedding) == 0 {
			continue
		}
		score := embed.Cosine(qv, e.Embedding)
		if score < minScore {
			continue
		}
		cands = append(cands, scored{name: e.Name, score: score})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > seedTopK {
		cands = cands[:seedTopK]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.name
	}
	return out
}

// EntityKeywordHits returns the canonical names of entities whose name or any
// alias appears literally in the text (case-insensitive substring). Names
// shorter than 3 runes are skipped to avoid false positives on common words.
func EntityKeywordHits(g *graph.Graph, text string) []string {
	lt := strings.ToLower(text)
	var hits []string
	for _, e := range g.Entities {
		n := strings.TrimSpace(e.Name)
		if n == "" || len([]rune(n)) < 3 {
			continue // skip very short names to avoid noise
		}
		matched := strings.Contains(lt, strings.ToLower(n))
		if !matched {
			for _, al := range e.Aliases {
				al = strings.TrimSpace(al)
				if al != "" && len([]rune(al)) >= 3 && strings.Contains(lt, strings.ToLower(al)) {
					matched = true
					break
				}
			}
		}
		if matched {
			hits = append(hits, e.Name)
		}
	}
	return hits
}

// expandAlongRelations grows the hit set by walking `hops` relation edges out
// from the currently-hit entities, treating relations as undirected so both
// upstream and downstream neighbors are pulled in.
func expandAlongRelations(g *graph.Graph, hit map[string]bool, hops int) {
	for h := 0; h < hops; h++ {
		frontier := map[string]bool{}
		for _, r := range g.Relations {
			from, to := strings.ToLower(r.From), strings.ToLower(r.To)
			// Only expand forward: if From is hit, pull in To.
			// Do NOT expand backward (hitting To should not pull in From),
			// because relations are directional — "A contains B" does not mean
			// mentioning B should bring all of A's knowledge into context.
			if hit[from] && !hit[to] {
				frontier[to] = true
			}
		}
		if len(frontier) == 0 {
			break
		}
		for n := range frontier {
			hit[n] = true
		}
	}
}

// rankChunks returns candidate chunks whose cosine >= minScore, sorted
// best-first and capped at chunkRecallCandidates. Returns nil when the query
// can't be embedded or nothing clears the floor.
func rankChunks(qv []float32, chunks []graph.Chunk, minScore float32) []graph.Chunk {
	if len(qv) == 0 || len(chunks) == 0 {
		return nil
	}
	type scored struct {
		ch    graph.Chunk
		score float32
	}
	var cands []scored
	for _, ch := range chunks {
		s := embed.Cosine(qv, ch.Embedding)
		if s < minScore {
			continue
		}
		cands = append(cands, scored{ch: ch, score: s})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > chunkRecallCandidates {
		cands = cands[:chunkRecallCandidates]
	}
	out := make([]graph.Chunk, len(cands))
	for i, c := range cands {
		out[i] = c.ch
	}
	return out
}

// chunkKeywordHits returns chunks whose text contains a query term, preserving
// input order. Terms are whitespace-split tokens of length >= 2.
func chunkKeywordHits(chunks []graph.Chunk, text string) []graph.Chunk {
	terms := QueryTerms(text)
	if len(terms) == 0 {
		return nil
	}
	// Require a chunk to contain the majority of query terms (at least half,
	// minimum 1) to be considered a keyword hit. This prevents single common
	// words like "更新" from pulling in unrelated chunks.
	threshold := len(terms) / 2
	if threshold < 1 {
		threshold = 1
	}
	var out []graph.Chunk
	for _, ch := range chunks {
		lc := strings.ToLower(ch.Text)
		matched := 0
		for _, t := range terms {
			if len([]rune(t)) >= 2 && strings.Contains(lc, t) {
				matched++
			}
		}
		if matched >= threshold {
			out = append(out, ch)
		}
	}
	return out
}

// fuseChunks RRF-fuses the vector-recall ranking with the keyword-hit ranking by
// chunk ID, returning the top chunks (capped at chunkInjectTopN) in fused order.
func fuseChunks(vectorRanked, keywordRanked []graph.Chunk) []graph.Chunk {
	byID := map[string]graph.Chunk{}
	ids := func(chunks []graph.Chunk) []string {
		out := make([]string, 0, len(chunks))
		for _, c := range chunks {
			byID[c.ID] = c
			out = append(out, c.ID)
		}
		return out
	}
	fused := RRFFuse(ids(vectorRanked), ids(keywordRanked))
	var out []graph.Chunk
	for _, id := range fused {
		out = append(out, byID[id])
		if len(out) >= chunkInjectTopN {
			break
		}
	}
	return out
}

// RRFFuse merges several ranked lists of ids into one order by Reciprocal Rank
// Fusion: score(id) = Σ 1/(rrfK + rank), rank 1-based within each list. It looks
// only at ranks, not raw scores, so it fuses channels with incomparable units.
// Ties break by first appearance, keeping output stable.
func RRFFuse(lists ...[]string) []string {
	score := map[string]float64{}
	firstSeen := map[string]int{}
	order := 0
	for _, list := range lists {
		for rank, id := range list {
			score[id] += 1.0 / float64(rrfK+rank+1)
			if _, ok := firstSeen[id]; !ok {
				firstSeen[id] = order
				order++
			}
		}
	}
	fused := make([]string, 0, len(score))
	for id := range score {
		fused = append(fused, id)
	}
	sort.Slice(fused, func(i, j int) bool {
		if score[fused[i]] != score[fused[j]] {
			return score[fused[i]] > score[fused[j]]
		}
		return firstSeen[fused[i]] < firstSeen[fused[j]]
	})
	return fused
}

// QueryTerms lowercases and splits a query into de-duplicated tokens >= 2 chars,
// filtering out common stop words that cause noise in keyword matching.
func QueryTerms(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range strings.Fields(strings.ToLower(text)) {
		f = strings.Trim(f, ".,;:!?()[]{}\"'`")
		if len([]rune(f)) < 2 || seen[f] {
			continue
		}
		if isStopWord(f) {
			continue
		}
		seen[f] = true
		out = append(out, f)
	}
	return out
}

// isStopWord returns true for common Chinese/English words that are too generic
// to be useful as search terms and would cause false positive matches.
func isStopWord(w string) bool {
	switch w {
	case "的", "了", "是", "在", "有", "和", "就", "不", "也", "都",
		"这", "那", "要", "会", "对", "能", "把", "到", "从", "被",
		"更新", "修改", "处理", "使用", "需要", "进行", "通过", "如何",
		"查看", "检查", "确认", "获取", "设置", "添加", "删除", "创建",
		"the", "is", "in", "to", "of", "and", "for", "on", "at",
		"how", "what", "this", "that", "with", "from", "can", "do":
		return true
	}
	return false
}

// lowerAll lowercases every element of a slice (normalizing entity names into
// RRF ids).
func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}
