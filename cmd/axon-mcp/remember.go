package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/retrieve"
)

// rememberArgs is the remember_knowledge argument shape. It mirrors the same
// entity/relation model the indexing pipeline distills, so agent-written
// knowledge merges into the graph exactly like conversation/code/obsidian
// knowledge (via alias normalization in graph.Merge).
type rememberArgs struct {
	Project  string `json:"project"`
	Entities []struct {
		Name         string   `json:"name"`
		Type         string   `json:"type"`
		Observations []string `json:"observations"`
		Aliases      []string `json:"aliases"`
	} `json:"entities"`
	Relations []struct {
		From  string `json:"from"`
		To    string `json:"to"`
		Label string `json:"label"`
	} `json:"relations"`
}

// rememberKnowledge writes agent-supplied durable knowledge back into a
// project's graph. This is the write half of the "用得越多越懂" loop for the
// MCP path: search/get are read-only, this one grows the graph.
//
// It persists the knowledge as its own cache entry keyed by "mcp:<ts>" (so it
// is provenance-distinguishable and never clobbers session/code/obsidian
// caches). Because AssembleGraph re-merges every cache on each query, the new
// facts are immediately live for the next search_knowledge / get_entity, with
// alias normalization folding them into existing same-named entities.
func (h *toolHandler) rememberKnowledge(raw json.RawMessage) (*toolResult, error) {
	var a rememberArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	slug, _ := h.resolveSlug(a.Project)
	if slug == "" {
		return nil, fmt.Errorf("无法确定项目：当前目录没有 .axon-project 文件，请显式传 project 参数或创建 .axon-project")
	}
	a.Project = slug

	// Build entities, dropping empties and stamping provenance so each fact is
	// traceable back to "this MCP session learned it". The key carries a random
	// suffix, not just a millisecond timestamp: parallel subagents fanning out
	// under one Claude Code session can call remember_knowledge in the same
	// millisecond, and a timestamp-only key would make them collide on the same
	// cache file (and .tmp) and clobber each other's knowledge.
	source := fmt.Sprintf("mcp:%d-%s", time.Now().UnixMilli(), randSuffix())
	var ents []graph.Entity
	for _, e := range a.Entities {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		var obs []string
		for _, o := range e.Observations {
			if o = strings.TrimSpace(o); o != "" {
				obs = append(obs, o)
			}
		}
		if len(obs) == 0 {
			continue // an entity with no facts adds nothing durable
		}
		src := make([]string, len(obs))
		for i := range src {
			src[i] = source
		}
		typ := strings.TrimSpace(e.Type)
		if typ == "" {
			typ = "concept"
		}
		ents = append(ents, graph.Entity{
			Name:         name,
			Type:         typ,
			Observations: obs,
			Aliases:      trimAll(e.Aliases),
			ObsSources:   src,
		})
	}
	if len(ents) == 0 {
		return nil, fmt.Errorf("没有可记录的实体（每个实体至少要有一条 observation）")
	}

	var rels []graph.Relation
	for _, r := range a.Relations {
		from, to, label := strings.TrimSpace(r.From), strings.TrimSpace(r.To), strings.TrimSpace(r.Label)
		if from == "" || to == "" || label == "" {
			continue
		}
		rels = append(rels, graph.Relation{From: from, To: to, Label: label})
	}

	// Embed the new entities so they are semantically recallable, honoring the
	// same EmbeddingMode the GUI/recall path uses. A nil embedder (semantic mode
	// with unusable cloud) means we store without vectors — they still match via
	// substring/keyword — rather than blocking the write.
	emb := newEmbedder(h.ctx, h.cfg, h.secrets, h.probe)
	if emb != nil {
		embedEntities(h.ctx, emb, ents)
	}

	// Write-time entity resolution: only with a real semantic embedder (the local
	// lexical embedder's surface-overlap vectors add nothing past alias matching).
	// Relabel near-identical incoming entities onto their existing canonical name
	// and strip reworded-duplicate observations, so repeated agent writes fold
	// into one node instead of polluting the graph with near-duplicates.
	if emb != nil && emb.Model() != embed.LocalModelID {
		if g, err := retrieve.AssembleGraph(h.dataDir, a.Project); err == nil {
			ents = resolveEntities(g.Entities, ents)
		}
		if len(ents) == 0 {
			return textResult(fmt.Sprintf("这些知识项目 `%s` 里都已经有了，未新增。", a.Project)), nil
		}
	}

	// Persist as a dedicated cache entry. Mtime is "now" and the key is unique
	// per write, so re-remembering never overwrites prior knowledge; graph.Merge
	// dedupes facts and folds them into existing entities on the next assemble.
	if err := graph.SaveCache(h.dataDir, a.Project, &graph.SessionCache{
		SessionID: source,
		Mtime:     time.Now().UnixMilli(),
		Schema:    graph.CacheSchema,
		Entities:  ents,
		Relations: rels,
	}); err != nil {
		return nil, fmt.Errorf("save knowledge: %w", err)
	}

	names := make([]string, len(ents))
	for i, e := range ents {
		names[i] = e.Name
	}
	return textResult(fmt.Sprintf(
		"已记住 %d 个实体（%s）%s，写入项目 `%s` 的知识图谱。下次 search_knowledge / get_entity 即可召回。",
		len(ents), strings.Join(names, "、"),
		relCountSuffix(len(rels)), a.Project,
	)), nil
}

// embedEntities fills each entity's Embedding in place (best-effort: a failed
// embed leaves that entity vector-less, still matchable by substring). Mirrors
// the App's embedEntities without importing the main package.
func embedEntities(ctx context.Context, emb embed.Embedder, ents []graph.Entity) {
	for i := range ents {
		text := strings.TrimSpace(strings.Join(append([]string{ents[i].Name}, ents[i].Observations...), " "))
		if text == "" {
			continue
		}
		if v, err := emb.Embed(ctx, text); err == nil {
			ents[i].Embedding = v
		}
	}
}

// randSuffix returns a short random hex string, disambiguating cache keys written
// in the same millisecond by concurrent callers. On the astronomically unlikely
// rand failure it degrades to a fixed marker (the millisecond still separates
// most writes).
func randSuffix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "x"
	}
	return hex.EncodeToString(b[:])
}

// trimAll trims and drops empty strings from a slice.
func trimAll(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// relCountSuffix renders the optional relation-count clause for the reply.
func relCountSuffix(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("、%d 条关系", n)
}
