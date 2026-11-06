package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/orchestr"
	"axon/internal/retrieve"
	"axon/internal/secret"
	"axon/internal/task"
)

// graphRecaller renders project background for enrichment from an EXISTING
// knowledge graph. It is strictly read-only: a colleague's fresh install has an
// empty graph, and recall must degrade to "no background" rather than fail.
type graphRecaller struct {
	dataDir string
	cfg     *config.Manager
	secrets secret.Store
	probe   *embed.ProbeCache
}

func newGraphRecaller(dataDir string, _ envConfig) *graphRecaller {
	cfg, err := config.Load(dataDir)
	if err != nil {
		log.Printf("读取配置失败，知识召回将退化为本地关键词模式: %v", err)
		cfg = nil
	}
	return &graphRecaller{
		dataDir: dataDir,
		cfg:     cfg,
		secrets: secret.New(),
		probe:   embed.NewProbeCache(),
	}
}

func (r *graphRecaller) Recall(ctx context.Context, slug, query string) (orchestr.Knowledge, error) {
	g, err := retrieve.AssembleGraph(r.dataDir, slug)
	if err != nil {
		return orchestr.Knowledge{}, err
	}
	// Empty graph is the normal state on a fresh install: silently no background.
	if len(g.Entities) == 0 && len(retrieve.LoadChunks(r.dataDir, slug)) == 0 {
		return orchestr.Knowledge{}, nil
	}
	chunks := retrieve.LoadChunks(r.dataDir, slug)

	var qv []float32
	local := true
	if emb := r.embedder(ctx); emb != nil {
		local = emb.Model() == embed.LocalModelID
		if v, e := emb.Embed(ctx, query); e == nil {
			qv = v
		} else {
			log.Printf("召回向量化失败，本次退化为关键词匹配: %v", e)
		}
	}
	res := retrieve.RecallWithOpts(g, chunks, qv, query, retrieve.RecallOptsFor(local))
	if len(res.OrderedHits) == 0 && len(res.Chunks) == 0 {
		return orchestr.Knowledge{}, nil
	}

	method := "semantic"
	if local {
		method = "keyword"
	}
	return orchestr.Knowledge{
		Context: renderRecall(g, res),
		Names:   res.OrderedHits,
		Sources: chunkSources(res.Chunks),
		Method:  method,
		Local:   local,
	}, nil
}

// embedder mirrors the MCP server's policy: keyword mode (the default) uses the
// dependency-free local embedder; semantic mode uses the configured cloud model
// and falls back to local when it isn't usable, so a colleague never gets a
// hard failure over embedding configuration.
func (r *graphRecaller) embedder(ctx context.Context) embed.Embedder {
	if r.cfg == nil || r.cfg.Get().EmbeddingMode != config.EmbeddingModeSemantic {
		return embed.NewLocal()
	}
	cloud, key, err := buildCloudEmbedder(r.cfg, r.secrets)
	if err != nil || cloud == nil {
		return embed.NewLocal()
	}
	if usable, probed, _ := r.probe.Usable(ctx, cloud, key); !usable {
		if probed {
			log.Printf("云端 embedding 不可用，这次召回用本地关键词替代")
		}
		return embed.NewLocal()
	}
	return cloud
}

// buildCloudEmbedder resolves the cloud embedding client from config.
func buildCloudEmbedder(cfg *config.Manager, secrets secret.Store) (embed.Embedder, string, error) {
	c := cfg.Get()
	model := c.EmbeddingModel

	var pcName string
	if c.EmbeddingProvider != "" {
		pcName = c.EmbeddingProvider
	} else {
		for _, p := range c.Providers {
			if p.Protocol == "openai" {
				pcName = p.Name
				break
			}
		}
	}
	if pcName == "" {
		return nil, "", fmt.Errorf("没有可用的 embedding provider")
	}
	pc, ok := cfg.Provider(pcName)
	if !ok {
		return nil, "", fmt.Errorf("embedding provider %q 未配置", pcName)
	}
	key, err := secrets.Get(pc.KeyRef)
	if err != nil {
		return nil, "", err
	}
	return embed.NewOpenAI(pc.BaseURL, key, model), embed.ProbeKey(pc.BaseURL, model, pc.KeyRef), nil
}

// renderRecall flattens matched entities and verbatim chunks into the background
// block injected into the enrichment prompt.
func renderRecall(g *graph.Graph, res retrieve.Result) string {
	byName := make(map[string]graph.Entity, len(g.Entities))
	for _, e := range g.Entities {
		byName[e.Name] = e
	}
	var b strings.Builder
	b.WriteString("以下是该项目相关的历史背景知识（来自过去工作的蒸馏，可能不完整，仅供参考）：")

	shown := 0
	for _, name := range res.OrderedHits {
		e, ok := byName[name]
		if !ok {
			continue
		}
		if shown >= 12 {
			break
		}
		shown++
		fmt.Fprintf(&b, "\n\n- %s", name)
		if e.Type != "" {
			fmt.Fprintf(&b, "（%s）", e.Type)
		}
		for i, o := range e.Observations {
			if i >= 3 {
				break
			}
			fmt.Fprintf(&b, "\n  · %s", o)
		}
	}

	if len(res.Chunks) > 0 {
		b.WriteString("\n\n相关原文片段：")
		for i, c := range res.Chunks {
			if i >= 3 {
				break
			}
			fmt.Fprintf(&b, "\n[%d] %s", i+1, truncateRunes(strings.TrimSpace(c.Text), 600))
		}
	}
	return b.String()
}

// chunkSources lists the distinct provenance tags behind the recalled chunks.
func chunkSources(chunks []graph.Chunk) []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range chunks {
		if c.Source == "" || seen[c.Source] {
			continue
		}
		seen[c.Source] = true
		out = append(out, c.Source)
	}
	return out
}

// truncateRunes cuts a string to n runes so multi-byte text is never split
// mid-character.
func truncateRunes(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

// --- writeback ---

// graphWritebacker folds an accepted task into the project's graph so the graph
// grows from use. On a fresh install this is what bootstraps the very first
// knowledge.
//
// It stores the task transcript as a verbatim chunk (the raw-context channel)
// rather than distilling entities: distillation needs an extra LLM extraction
// pass, which the desktop app already does. Chunks are enough for the next
// similar task to recall what was decided here.
type graphWritebacker struct {
	dataDir string
	cfg     *config.Manager
	secrets secret.Store
	probe   *embed.ProbeCache
}

func newGraphWritebacker(dataDir string, _ envConfig) *graphWritebacker {
	cfg, err := config.Load(dataDir)
	if err != nil {
		cfg = nil
	}
	return &graphWritebacker{
		dataDir: dataDir,
		cfg:     cfg,
		secrets: secret.New(),
		probe:   embed.NewProbeCache(),
	}
}

// embedder picks the embedder whose vector space matches the one the graph was
// indexed with. Mixing the local lexical embedder into a semantic graph would
// corrupt recall (two incompatible spaces in one index), so when semantic mode
// is on and the cloud model is unusable, writeback is skipped entirely.
func (w *graphWritebacker) embedder(ctx context.Context) embed.Embedder {
	if w.cfg == nil || w.cfg.Get().EmbeddingMode != config.EmbeddingModeSemantic {
		return embed.NewLocal()
	}
	cloud, key, err := buildCloudEmbedder(w.cfg, w.secrets)
	if err != nil || cloud == nil {
		return nil
	}
	if usable, _, _ := w.probe.Usable(ctx, cloud, key); !usable {
		return nil
	}
	return cloud
}

func (w *graphWritebacker) WriteBack(ctx context.Context, t task.Task, result string) error {
	slug := strings.TrimSpace(t.ProjectSlug)
	if slug == "" {
		return nil
	}

	text := buildTranscript(t, result)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	emb := w.embedder(ctx)
	if emb == nil {
		log.Printf("云端 embedding 不可用，本次采纳未回写图谱（避免混用两套向量空间）")
		return nil
	}
	source := "task:" + t.ID
	v, err := emb.Embed(ctx, text)
	if err != nil {
		return fmt.Errorf("向量化任务文本失败: %w", err)
	}
	cache := &graph.SessionCache{
		SessionID: source,
		Mtime:     time.Now().UnixMilli(),
		Schema:    graph.CacheSchema,
		Chunks: []graph.Chunk{{
			ID:        source + "#0",
			Text:      text,
			Source:    source,
			Kind:      "task",
			Embedding: v,
		}},
	}
	return graph.SaveCache(w.dataDir, slug, cache)
}

// buildTranscript assembles what gets distilled: the rough input, the confirmed
// spec, and the accepted result.
func buildTranscript(t task.Task, result string) string {
	var b strings.Builder
	if s := strings.TrimSpace(t.Input); s != "" {
		b.WriteString("任务输入:\n")
		b.WriteString(s)
		b.WriteString("\n\n")
	}
	if spec := strings.TrimSpace(orchestr.RenderSpec(t.Spec)); spec != "" {
		b.WriteString("任务规格:\n")
		b.WriteString(spec)
		b.WriteString("\n\n")
	}
	if r := strings.TrimSpace(result); r != "" {
		b.WriteString("最终采纳的产出:\n")
		b.WriteString(r)
	}
	return b.String()
}
