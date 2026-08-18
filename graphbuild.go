package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"axon/internal/claudedata"
	"axon/internal/db"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/provider"
	"axon/internal/retrieve"
)

// graphModelDefault is the fallback when no GraphModel is configured. It
// matches the user's private gateway (gpt-5.6-sol), but any OpenAI-compatible
// model works — a different provider simply configures its preferred model in
// settings.
const graphModelDefault = "gpt-5.6-sol"

// graphModel returns the model used for knowledge distillation, preferring the
// config value when set, then the default.
func (a *App) graphModel() string {
	if m := a.cfg.GraphModel(); m != "" {
		return m
	}
	return graphModelDefault
}

// Graph build events for the frontend.
const (
	EventGraphProgress = "graph:progress"
	EventGraphDone     = "graph:done"
)

// extractPrompt tells the model to keep only durable, reusable project
// knowledge and to drop noise (greetings, tool logs, one-off asks, abandoned
// ideas). Output is strict JSON so it can be merged programmatically.
const extractPrompt = `你是一个知识提炼器。从下面的对话中，只提炼「下次处理这个项目还用得上」的持久知识，构建知识图谱。

【只保留有价值的】
- 项目/模块/服务/概念，及它们之间的关系（依赖、调用、属于等）
- 关键决策及其理由（为什么这么设计/选型）
- 踩过的坑、约束、教训（如"这里不能用X，因为Y"）
- 稳定事实：接口约定、数据结构、依赖关系

【坚决丢弃的噪音】
- 寒暄、"好的/继续/谢谢"等无信息量的话
- 工具调用过程、构建日志、报错堆栈等临时内容
- 一次性、不影响后续的操作
- 探索中被否定/放弃的方案（除非"为什么放弃"是有价值的教训）

宁可少而准，不要多而杂。若这段对话没有值得入图的知识，返回空数组。

【实体别名】
- 为每个实体给出 aliases：同一事物的其他叫法（全称/简称/中英文名）。
- 例如「支付服务」的 aliases 可能是 ["PaymentService","payment"]。
- 目的是让同一事物在图谱里只保留一个节点，不要因为叫法不同而拆成多个。
- 没有别名就给空数组 []。

只输出如下 JSON，不要任何解释：
{"entities":[{"name":"实体名","type":"module|service|concept|decision|constraint","aliases":["别名"],"observations":["关于它的事实"]}],"relations":[{"from":"实体A","to":"实体B","label":"关系"}]}`

// Namespace is one named knowledge-graph namespace returned by ListNamespaces.
type Namespace struct {
	Name     string `json:"name"`
	Entities int    `json:"entities"`
}

// ListNamespaces returns all named namespaces that have a graphcache directory,
// with their entity counts. Used by the frontend project selector.
func (a *App) ListNamespaces() ([]Namespace, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	entries, readErr := os.ReadDir(filepath.Join(dataDir, "graphcache"))
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return []Namespace{}, nil
		}
		return nil, readErr
	}
	var out []Namespace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		g, err := retrieve.AssembleGraph(dataDir, e.Name())
		if err != nil {
			continue
		}
		out = append(out, Namespace{Name: e.Name(), Entities: len(g.Entities)})
	}
	return out, nil
}

// BuildMultiGraph assembles and merges graphs from multiple namespaces into one.
// Entities sharing names/aliases across namespaces are automatically merged via
// graph.Merge, making cross-namespace relationships visible.
func (a *App) BuildMultiGraph(slugs []string) (*graph.Graph, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	merged := &graph.Graph{ProjectSlug: strings.Join(slugs, "+"), Entities: []graph.Entity{}, Relations: []graph.Relation{}}
	for _, slug := range slugs {
		caches, cErr := graph.LoadAllCache(dataDir, slug)
		if cErr != nil {
			continue
		}
		for _, c := range caches {
			merged.Merge(c.Entities, c.Relations)
		}
		// Apply exclusions per namespace.
		if ex, exErr := graph.LoadExclusions(dataDir, slug); exErr == nil {
			graph.FilterExcluded(merged, ex)
		}
	}
	merged.UpdatedAt = time.Now().UnixMilli()
	return merged, nil
}

// GetGraph returns the stored knowledge graph for a project.
func (a *App) GetGraph(projectSlug string) (*graph.Graph, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	return graph.Load(dataDir, projectSlug)
}

// extracted is the model's JSON output shape.
type extracted struct {
	Entities  []graph.Entity   `json:"entities"`
	Relations []graph.Relation `json:"relations"`
}

// extractFromText runs one distillation call over a transcript chunk.
func (a *App) extractFromText(ctx context.Context, prov provider.Provider, transcript string) (extracted, error) {
	reply, err := collectReply(ctx, prov, provider.ChatRequest{
		Model: a.graphModel(),
		Messages: []provider.ChatMessage{
			{Role: provider.RoleSystem, Content: extractPrompt},
			{Role: provider.RoleUser, Content: transcript},
		},
		Temperature: 0.1,
		MaxTokens:   2000,
	})
	if err != nil {
		return extracted{}, err
	}
	return parseExtracted(reply), nil
}

// parseExtracted pulls the JSON object out of the model reply (tolerates code
// fences or surrounding prose).
func parseExtracted(reply string) extracted {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var ex extracted
	if err := json.Unmarshal([]byte(s), &ex); err != nil {
		return extracted{} // treat unparseable as "nothing valuable"
	}
	return ex
}

// maxTranscriptChars bounds how much of one session is sent per extraction,
// keeping token cost predictable. When a session exceeds it we keep the NEWEST
// content (later turns usually carry the settled decisions), matching the intent
// that "newest content matters most for knowledge".
const maxTranscriptChars = 16000

// IndexProject builds the per-session distillation cache: for each session,
// if there's no fresh cache it calls the model once and stores the result.
// This is the slow, one-time pass; afterwards focus/full assembly is instant
// and free. Re-running only processes new/changed sessions (incremental).
func (a *App) IndexProject(projectSlug string) error {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return fmt.Errorf("需要一个 OpenAI 协议的 provider 来调用 %s（去设置配一个）", a.graphModel())
	}
	pc, _ := a.cfg.Provider(name)
	prov, err := a.newProvider(pc)
	if err != nil {
		return err
	}
	sessions, err := claudedata.ListSessions(projectSlug)
	if err != nil {
		// Named namespaces (like _global_, gaia, etc.) don't have a matching
		// Claude Code project directory — that's fine, just no Claude sessions.
		sessions = nil
	}

	// Build an embedder per the user's mode. Keyword mode always returns the
	// local lexical embedder. Semantic mode returns the cloud model, or an error
	// if it's unavailable — in which case we do NOT fall back: indexing proceeds
	// without vectors and the failure is surfaced to the UI.
	emb, embErr := a.newEmbedder()
	if embErr != nil {
		emb = nil
	}

	if emb == nil {
		// Semantic mode with a broken cloud endpoint: skip vectors this run and
		// tell the frontend exactly why, instead of silently degrading.
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "phase": "index",
			"warning": fmt.Sprintf("本次不生成向量与原文块：%v", embErr),
		})
	}

	newly := 0
	for i, s := range sessions {
		if _, ok := graph.LoadCache(dataDir, projectSlug, s.ID, s.UpdatedAt); ok {
			continue // fresh cache at current schema, skip entirely
		}
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "current": i + 1, "total": len(sessions),
			"title": s.Title, "phase": "index",
		})
		msgs, err := claudedata.ReadSession(projectSlug, s.ID)
		if err != nil {
			continue
		}

		// Incremental back-fill: if a same-mtime cache already exists but is only
		// stale due to a schema bump, reuse its distilled entities/relations (skip
		// the LLM) and just add the new chunk field. This makes upgrades cost only
		// embedding calls, never re-distillation.
		if prev, ok := graph.LoadCacheRaw(dataDir, projectSlug, s.ID); ok && prev.Mtime == s.UpdatedAt {
			chunks := chunkTranscript(s.ID, msgs)
			a.embedChunks(emb, chunks)
			_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
				SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
				Entities: prev.Entities, Relations: prev.Relations, Chunks: chunks,
			})
			newly++
			continue
		}

		ex := extracted{}
		transcript := buildTranscript(msgs)
		if strings.TrimSpace(transcript) != "" {
			ex, err = a.extractFromText(a.ctx, prov, transcript)
			if err != nil {
				a.emit(EventGraphProgress, map[string]any{
					"projectSlug": projectSlug, "current": i + 1, "total": len(sessions),
					"title": s.Title, "error": err.Error(),
				})
				continue // skip; will retry next index run
			}
		}
		// Stamp provenance: every observation distilled from this session is
		// sourced to this session id, so merged knowledge stays traceable.
		stampObsSources(ex.Entities, s.ID)
		// Raw-context channel: chunk the FULL session (not the bounded transcript)
		// and embed the blocks so they are semantically recallable alongside
		// entities.
		chunks := chunkTranscript(s.ID, msgs)
		// Attach embeddings so this session's entities and chunks are searchable.
		if emb != nil {
			a.embedEntities(emb, ex.Entities)
			a.embedChunks(emb, chunks)
		}
		_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
			Entities: ex.Entities, Relations: ex.Relations, Chunks: chunks,
		})
		newly++
	}

	// --- Index Codex sessions that belong to this project ---
	codexSessions, _ := claudedata.ListCodexSessions(projectSlug)
	for i, s := range codexSessions {
		if _, ok := graph.LoadCache(dataDir, projectSlug, s.ID, s.UpdatedAt); ok {
			continue
		}
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "current": i + 1, "total": len(codexSessions),
			"title": s.Title, "phase": "codex",
		})
		sessionFile := claudedata.FindCodexSessionFile(strings.TrimPrefix(s.ID, "codex:"))
		if sessionFile == "" {
			continue
		}
		msgs, err := claudedata.ReadCodexSession(sessionFile)
		if err != nil || len(msgs) == 0 {
			continue
		}

		// Incremental back-fill (same as Claude sessions).
		if prev, ok := graph.LoadCacheRaw(dataDir, projectSlug, s.ID); ok && prev.Mtime == s.UpdatedAt {
			chunks := chunkTranscript(s.ID, msgs)
			a.embedChunks(emb, chunks)
			_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
				SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
				Entities: prev.Entities, Relations: prev.Relations, Chunks: chunks,
			})
			newly++
			continue
		}

		ex := extracted{}
		transcript := buildTranscript(msgs)
		if strings.TrimSpace(transcript) != "" {
			ex, err = a.extractFromText(a.ctx, prov, transcript)
			if err != nil {
				a.emit(EventGraphProgress, map[string]any{
					"projectSlug": projectSlug, "current": i + 1, "total": len(codexSessions),
					"title": s.Title, "error": err.Error(), "phase": "codex",
				})
				continue
			}
		}
		stampObsSources(ex.Entities, s.ID)
		chunks := chunkTranscript(s.ID, msgs)
		if emb != nil {
			a.embedEntities(emb, ex.Entities)
			a.embedChunks(emb, chunks)
		}
		_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
			Entities: ex.Entities, Relations: ex.Relations, Chunks: chunks,
		})
		newly++
	}

	// --- Index WorkBuddy + CodeBuddy sessions that belong to this project ---
	buddySessions, _ := claudedata.ListBuddySessions(projectSlug)
	for i, s := range buddySessions {
		if _, ok := graph.LoadCache(dataDir, projectSlug, s.ID, s.UpdatedAt); ok {
			continue
		}
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "current": i + 1, "total": len(buddySessions),
			"title": s.Title, "phase": "buddy",
		})
		sessionFile := claudedata.FindBuddySessionFile(strings.TrimPrefix(strings.TrimPrefix(s.ID, "workbuddy:"), "codebuddy:"))
		if sessionFile == "" {
			continue
		}
		msgs, err := claudedata.ReadBuddySession(sessionFile)
		if err != nil || len(msgs) == 0 {
			continue
		}

		if prev, ok := graph.LoadCacheRaw(dataDir, projectSlug, s.ID); ok && prev.Mtime == s.UpdatedAt {
			chunks := chunkTranscript(s.ID, msgs)
			a.embedChunks(emb, chunks)
			_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
				SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
				Entities: prev.Entities, Relations: prev.Relations, Chunks: chunks,
			})
			newly++
			continue
		}

		ex := extracted{}
		transcript := buildTranscript(msgs)
		if strings.TrimSpace(transcript) != "" {
			ex, err = a.extractFromText(a.ctx, prov, transcript)
			if err != nil {
				a.emit(EventGraphProgress, map[string]any{
					"projectSlug": projectSlug, "current": i + 1, "total": len(buddySessions),
					"title": s.Title, "error": err.Error(), "phase": "buddy",
				})
				continue
			}
		}
		stampObsSources(ex.Entities, s.ID)
		chunks := chunkTranscript(s.ID, msgs)
		if emb != nil {
			a.embedEntities(emb, ex.Entities)
			a.embedChunks(emb, chunks)
		}
		_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: s.ID, Mtime: s.UpdatedAt, Schema: graph.CacheSchema,
			Entities: ex.Entities, Relations: ex.Relations, Chunks: chunks,
		})
		newly++
	}

	a.emit(EventGraphDone, map[string]any{
		"projectSlug": projectSlug, "processed": newly, "phase": "index",
	})
	return nil
}

// stampObsSources sets each entity's ObsSources parallel to its Observations,
// marking every fact as originating from sessionID. Best-effort provenance for
// later traceability; the array stays aligned one-to-one with Observations.
func stampObsSources(entities []graph.Entity, sessionID string) {
	for i := range entities {
		src := make([]string, len(entities[i].Observations))
		for j := range src {
			src[j] = sessionID
		}
		entities[i].ObsSources = src
	}
}

// entityEmbedText builds the text embedded for an entity: its name plus its
// observations, so the vector reflects both the label and its facts.
func entityEmbedText(e graph.Entity) string {
	parts := append([]string{e.Name}, e.Observations...)
	return strings.TrimSpace(strings.Join(parts, " "))
}

// embedEntities fills in each entity's Embedding in place. Best-effort: a single
// failure is logged and skipped (that entity simply won't be semantically
// searchable and falls back to substring matching), so one bad call does not
// abort indexing.
func (a *App) embedEntities(emb embed.Embedder, entities []graph.Entity) {
	for i := range entities {
		text := entityEmbedText(entities[i])
		if text == "" {
			continue
		}
		vec, err := emb.Embed(a.ctx, text)
		if err != nil {
			log.Printf("axon: embed entity %q failed: %v", entities[i].Name, err)
			continue
		}
		entities[i].Embedding = vec
	}
}

// BuildGraph assembles the full graph from cached sessions (instant, no model
// calls). Callers should run IndexProject first to populate the cache.
func (a *App) BuildGraph(projectSlug string) (*graph.Graph, error) {
	return a.assembleGraph(projectSlug, "")
}

// assembleGraph merges cached session knowledge into one graph. If term != "",
// only entities/relations related to the term (by substring on name/type/
// observations/relation label) are kept — a focused, local, zero-token view.
func (a *App) assembleGraph(projectSlug, term string) (*graph.Graph, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	caches, err := graph.LoadAllCache(dataDir, projectSlug)
	if err != nil {
		return nil, err
	}
	g := &graph.Graph{ProjectSlug: projectSlug, Entities: []graph.Entity{}, Relations: []graph.Relation{}}
	for _, c := range caches {
		g.Merge(c.Entities, c.Relations)
	}
	// Apply the user's exclusion list AFTER merging, so facts the user deleted
	// stay gone even though the source caches still contain them. This is what
	// makes deletions survive re-indexing.
	if ex, exErr := graph.LoadExclusions(dataDir, projectSlug); exErr == nil {
		graph.FilterExcluded(g, ex)
	}
	if term != "" {
		g = filterGraph(g, term)
	}
	g.UpdatedAt = time.Now().UnixMilli()
	_ = graph.Save(dataDir, g)
	return g, nil
}

// buildTranscript joins user/assistant text into a transcript bounded to
// maxTranscriptChars. When the session is longer than the cap it keeps the
// NEWEST turns: it walks messages from the end, accumulating until the budget is
// hit, then emits them back in chronological order. (The previous version broke
// out of a forward loop and thus kept the OLDEST content — the opposite of the
// documented intent that recent turns matter most.)
func buildTranscript(msgs []claudedata.SessionMessage) string {
	// Render each message once, then select a newest-first suffix within budget.
	rendered := make([]string, len(msgs))
	for i, m := range msgs {
		rendered[i] = m.Role + ": " + m.Text + "\n\n"
	}
	total := 0
	start := len(rendered) // index of the first kept message
	for i := len(rendered) - 1; i >= 0; i-- {
		if total+len(rendered[i]) > maxTranscriptChars && start != len(rendered) {
			break // keep at least one message even if it alone exceeds the cap
		}
		total += len(rendered[i])
		start = i
	}
	var b strings.Builder
	for i := start; i < len(rendered); i++ {
		b.WriteString(rendered[i])
	}
	return b.String()
}

// focusPrompt extracts only knowledge related to a given focus term, keeping
// the graph small and readable so the user can verify it.
func focusPrompt(term string) string {
	return `你是一个知识提炼器。用户关心的主题是：「` + term + `」。

只提炼与这个主题【直接相关】的持久知识，构建一张聚焦的小图。与主题无关的一律不要。

保留：与「` + term + `」相关的模块/概念/服务、它们的关系、关键决策及理由、踩过的坑与约束。
丢弃：寒暄、日志、报错堆栈、一次性操作、与主题无关的内容。

宁可少而准。若这段对话与「` + term + `」无关，返回空数组。

只输出 JSON，不要解释：
{"entities":[{"name":"实体名","type":"module|service|concept|decision|constraint","observations":["事实"]}],"relations":[{"from":"A","to":"B","label":"关系"}]}`
}

// BuildGraphFocused returns a term-focused subgraph assembled from the cache —
// instant and free. Requires the project to have been indexed first.
func (a *App) BuildGraphFocused(projectSlug, term string) (*graph.Graph, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil, fmt.Errorf("请先输入一个关注的词")
	}
	return a.assembleGraph(projectSlug, term)
}

// filterGraph keeps entities matching the term (name/type/observation contains
// it) plus their directly linked neighbors, and the relations among the kept
// set. Case-insensitive substring match.
func filterGraph(g *graph.Graph, term string) *graph.Graph {
	lt := strings.ToLower(term)
	keep := map[string]bool{}
	for _, e := range g.Entities {
		if entityMatches(e, lt) {
			keep[strings.ToLower(e.Name)] = true
		}
	}
	// Pull in neighbors linked to a matched entity.
	for _, r := range g.Relations {
		lf, lto := strings.ToLower(r.From), strings.ToLower(r.To)
		if keep[lf] {
			keep[lto] = true
		}
		if keep[lto] {
			keep[lf] = true
		}
	}
	out := &graph.Graph{ProjectSlug: g.ProjectSlug, Entities: []graph.Entity{}, Relations: []graph.Relation{}}
	for _, e := range g.Entities {
		if keep[strings.ToLower(e.Name)] {
			out.Entities = append(out.Entities, e)
		}
	}
	for _, r := range g.Relations {
		if keep[strings.ToLower(r.From)] && keep[strings.ToLower(r.To)] {
			out.Relations = append(out.Relations, r)
		}
	}
	return out
}

func entityMatches(e graph.Entity, lt string) bool {
	if strings.Contains(strings.ToLower(e.Name), lt) || strings.Contains(strings.ToLower(e.Type), lt) {
		return true
	}
	for _, o := range e.Observations {
		if strings.Contains(strings.ToLower(o), lt) {
			return true
		}
	}
	return false
}

// articlePrompt asks the model to weave the graph facts into a readable,
// well-structured article (not a list dump) so the user can read the project
// knowledge like prose.
const articlePrompt = `下面是从一个项目的历史对话里提炼出的结构化知识（实体、关系、事实）。请把它组织成一篇【条理清晰、可顺畅阅读】的中文文章，像给新接手的人讲清楚这个项目。

要求：
- 用小标题分段（## 概览、## 核心模块、## 关键决策与理由、## 踩过的坑与约束 等，按实际内容调整）
- 把零散事实串成连贯的叙述，说明模块之间的关系
- 忠于给定知识，不要编造没有的内容
- 用 Markdown 输出，正文为主，避免大段罗列

只输出文章正文。`

// GenerateArticle turns the (optionally term-focused) knowledge graph into a
// readable Markdown article via the model. Assembled from cache, one model call.
func (a *App) GenerateArticle(projectSlug, term string) (string, error) {
	g, err := a.assembleGraph(projectSlug, strings.TrimSpace(term))
	if err != nil {
		return "", err
	}
	if len(g.Entities) == 0 {
		return "", fmt.Errorf("还没有可用的知识，请先「建索引」")
	}
	name, ok := a.providerForProtocol("openai")
	if !ok {
		return "", fmt.Errorf("需要一个 OpenAI 协议的 provider 来调用 %s", a.graphModel())
	}
	pc, _ := a.cfg.Provider(name)
	prov, err := a.newProvider(pc)
	if err != nil {
		return "", err
	}

	// Serialize the graph knowledge as the source material.
	var b strings.Builder
	if term != "" {
		fmt.Fprintf(&b, "关注主题：%s\n\n", term)
	}
	b.WriteString("实体与事实：\n")
	for _, e := range g.Entities {
		fmt.Fprintf(&b, "- %s（%s）\n", e.Name, e.Type)
		for _, o := range e.Observations {
			fmt.Fprintf(&b, "  · %s\n", o)
		}
	}
	if len(g.Relations) > 0 {
		b.WriteString("\n关系：\n")
		for _, r := range g.Relations {
			fmt.Fprintf(&b, "- %s —%s→ %s\n", r.From, r.Label, r.To)
		}
	}

	return collectReply(a.ctx, prov, provider.ChatRequest{
		Model:       a.graphModel(),
		Messages:    []provider.ChatMessage{{Role: provider.RoleSystem, Content: articlePrompt}, {Role: provider.RoleUser, Content: b.String()}},
		Temperature: 0.3, MaxTokens: 3000,
	})
}

// Recall method values reported on KnowledgeMatch.Method: which retrieval path
// actually produced the match, so the UI can show whether the AI recalled
// business knowledge by true semantics or degraded to literal matching.
const (
	RecallSemantic = "semantic" // vector seeds found (embedder + entity vectors present)
	RecallKeyword  = "keyword"  // only substring hits (no embedder, or graph has no vectors)
	RecallHybrid   = "hybrid"   // both semantic seeds and substring hits contributed
	RecallNone     = "none"     // nothing recalled
)

// InjectedChunk is one verbatim source fragment that was injected, exposed so
// the UI can show a trustworthy "original record" citation next to the answer.
type InjectedChunk struct {
	Text   string `json:"text"`
	Source string `json:"source"` // rendered provenance (session title / 代码 <path> / 任务 <id>)
}

// KnowledgeMatch is what the chat injects: the entities whose names appear in
// the user's message, a readable context block, and the matched names for the
// UI to show ("injected: X, Y").
type KnowledgeMatch struct {
	Names   []string `json:"names"`
	Context string   `json:"context"`
	// Chunks are the verbatim source fragments injected into the raw-context
	// section, with rendered provenance, so the UI can surface original-record
	// citations. Empty when no chunk was recalled (or no embedder).
	Chunks []InjectedChunk `json:"chunks,omitempty"`
	// ChunkHits is how many chunks were recalled by vector similarity (before the
	// injection cap), a signal that the raw-context channel actually fired.
	ChunkHits int `json:"chunkHits,omitempty"`
	// Sources are the distinct session titles the injected observations came
	// from, so the UI can show "这条知识来自 xxx 会话". Best-effort: empty when
	// the graph carries no provenance yet (older caches) or titles can't be
	// resolved. Session ids that have no known title fall back to the raw id.
	Sources []string `json:"sources,omitempty"`
	// Method reports which retrieval path produced this match — one of
	// RecallSemantic / RecallKeyword / RecallHybrid / RecallNone — so the UI can
	// surface whether recall used real semantics (vectors) or degraded to literal
	// keyword matching. This is the trust signal: "keyword" means the embedder
	// was unavailable or the graph carried no vectors, so recall fell back to
	// substring matching only.
	Method string `json:"method,omitempty"`
	// SemanticSeeds are the entity names recalled by vector similarity (the
	// closest seeds, before relation expansion); KeywordHits are the names matched
	// by literal substring on name/alias. Both optional, for a detailed breakdown.
	SemanticSeeds []string `json:"semanticSeeds,omitempty"`
	KeywordHits   []string `json:"keywordHits,omitempty"`
	// Local reports that the query was embedded by the pure-Go local fallback
	// (no cloud embedding configured). When true, any semantic recall is lexical
	// (fuzzy character/word overlap), not neural — the UI should temper trust
	// accordingly ("本地语义召回，精度有限") rather than claim full semantics.
	Local bool `json:"local,omitempty"`
}

// MatchKnowledge finds the knowledge relevant to the user's message using two
// parallel channels and fuses them with Reciprocal Rank Fusion (RRF):
//
//	Structure channel (entities/relations):
//	  1. semantic seeds — entities whose embedding is closest to the query;
//	  2. substring hits — entities whose name/alias literally appears in the query;
//	     RRF-fused into a ranked entity set, then expanded 1 hop along relations.
//	Raw-context channel (verbatim chunks):
//	  3. vector recall — chunks whose embedding is closest to the query;
//	  4. substring hits — chunks whose text contains a query term;
//	     RRF-fused into a ranked chunk set.
//
// Injection is two-stage with independent budgets: a "structure" section
// (entities+relations, ~800 tokens) followed by a "raw record" section (top
// chunks, ~2000 tokens, each labelled with its source). When no embedder is
// configured, both vector paths go dark and it degrades to pure substring
// matching over entities (the original behavior). Empty match => Names empty,
// Context "".
func (a *App) MatchKnowledge(projectSlug, text string) (KnowledgeMatch, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(projectSlug) == "" {
		return KnowledgeMatch{Names: []string{}, Method: RecallNone}, nil
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return KnowledgeMatch{Names: []string{}, Method: RecallNone}, err
	}
	g, err := a.assembleGraph(projectSlug, "")
	if err != nil {
		return KnowledgeMatch{Names: []string{}, Method: RecallNone}, err
	}

	// Embed the query once (best-effort) and reuse the vector for both channels.
	// localEmb tracks whether the vector came from the local fallback, so the UI
	// can distinguish full-semantic recall from lexical local recall.
	var qv []float32
	var localEmb bool
	if emb, embErr := a.newEmbedder(); embErr == nil {
		localEmb = emb.Model() == embed.LocalModelID
		if v, eErr := emb.Embed(a.ctx, text); eErr == nil {
			qv = v
		} else {
			log.Printf("axon: knowledge query embed failed: %v", eErr)
		}
	}

	// Run the shared two-channel recall (structure + raw context). The App only
	// adds provenance rendering and the UI trust signals on top of this.
	res := retrieve.RecallWithOpts(g, a.loadChunks(dataDir, projectSlug), qv, text, retrieve.RecallOptsFor(localEmb))
	semanticSeeds := res.SemanticSeeds
	keywordHits := res.KeywordHits
	hit := res.Hit
	chunkRanked := res.Chunks

	if len(hit) == 0 && len(chunkRanked) == 0 {
		return KnowledgeMatch{Names: []string{}, Method: RecallNone}, nil
	}

	// --- Assemble two-stage injection ---
	names, structText, srcIDs := buildStructureSection(g, hit)
	chunkText, injChunks, chunkSrcIDs := buildRawSection(projectSlug, chunkRanked)

	var b strings.Builder
	b.WriteString("以下是该项目的相关背景知识（来自你以往的对话/代码/任务，供参考）：\n")
	if structText != "" {
		b.WriteString("\n## 结构（实体与关系）\n")
		b.WriteString(structText)
	}
	if chunkText != "" {
		b.WriteString("\n## 相关原文片段（细节以原文为准，优先据此判断）\n")
		b.WriteString(chunkText)
	}

	srcIDs = append(srcIDs, chunkSrcIDs...)
	return KnowledgeMatch{
		Names:         names,
		Context:       b.String(),
		Chunks:        injChunks,
		ChunkHits:     len(chunkRanked),
		Sources:       resolveSessionTitles(projectSlug, dedupStrings(srcIDs)),
		Method:        recallMethod(len(semanticSeeds) > 0 || len(chunkRanked) > 0, len(keywordHits) > 0),
		SemanticSeeds: semanticSeeds,
		KeywordHits:   keywordHits,
		// Only flag local when the vector actually contributed a semantic hit;
		// pure keyword matches aren't "local semantic" and shouldn't be tempered.
		Local: localEmb && (len(semanticSeeds) > 0 || len(chunkRanked) > 0),
	}, nil
}

// queryTerms delegates to the shared implementation; kept as a thin wrapper so
// existing package tests continue to exercise it here.
func queryTerms(text string) []string { return retrieve.QueryTerms(text) }

// buildStructureSection renders the entity/relation section within its char
// budget, returning the matched names (graph order), the rendered text, and the
// distinct session ids the observations came from.
func buildStructureSection(g *graph.Graph, hit map[string]bool) (names []string, text string, srcIDs []string) {
	srcSeen := map[string]bool{}
	var b strings.Builder
	for _, e := range g.Entities {
		if !hit[strings.ToLower(strings.TrimSpace(e.Name))] {
			continue
		}
		if b.Len() >= injectStructBudgetChars {
			break
		}
		names = append(names, e.Name)
		fmt.Fprintf(&b, "\n【%s】\n", e.Name)
		for i, o := range e.Observations {
			fmt.Fprintf(&b, "- %s\n", o)
			if sid := obsSourceAt(e.ObsSources, i); sid != "" && !srcSeen[sid] {
				srcSeen[sid] = true
				srcIDs = append(srcIDs, sid)
			}
		}
	}
	// Relations among matched entities add useful structure.
	var rels []string
	for _, r := range g.Relations {
		if hit[strings.ToLower(r.From)] && hit[strings.ToLower(r.To)] {
			rels = append(rels, fmt.Sprintf("%s —%s→ %s", r.From, r.Label, r.To))
		}
	}
	if len(rels) > 0 {
		b.WriteString("\n关系：\n")
		for _, r := range rels {
			b.WriteString("- " + r + "\n")
		}
	}
	return names, b.String(), srcIDs
}

// buildRawSection renders the verbatim-chunk section within its char budget,
// each block tagged with its rendered provenance and separated by a divider.
// Returns the rendered text, the injected chunks (for UI citations), and the
// distinct chunk source ids.
func buildRawSection(projectSlug string, chunks []graph.Chunk) (text string, injected []InjectedChunk, srcIDs []string) {
	if len(chunks) == 0 {
		return "", nil, nil
	}
	var b strings.Builder
	budget := injectChunkBudgetChars
	for _, ch := range chunks {
		if budget-len(ch.Text) < 0 && len(injected) > 0 {
			break // keep at least one; otherwise stop when budget is spent
		}
		src := renderChunkSource(projectSlug, ch.Source)
		fmt.Fprintf(&b, "\n〔来源：%s〕\n%s\n───\n", src, ch.Text)
		injected = append(injected, InjectedChunk{Text: ch.Text, Source: src})
		srcIDs = append(srcIDs, ch.Source)
		budget -= len(ch.Text)
	}
	return b.String(), injected, srcIDs
}

// renderChunkSource resolves one chunk source id to a display label, reusing the
// session/code/task rendering used for observation provenance.
func renderChunkSource(projectSlug, source string) string {
	if titles := resolveSessionTitles(projectSlug, []string{source}); len(titles) > 0 {
		return titles[0]
	}
	return source
}

// dedupStrings returns xs with duplicates removed, preserving first-seen order.
func dedupStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// recallMethod classifies which retrieval path produced the match: both paths
// contributing is "hybrid", vector seeds alone is "semantic", substring alone
// (the degraded path — no embedder or no vectors) is "keyword". Callers only
// reach here with at least one hit, so "none" is handled by the empty-hit guard.
func recallMethod(hasSemantic, hasKeyword bool) string {
	switch {
	case hasSemantic && hasKeyword:
		return RecallHybrid
	case hasSemantic:
		return RecallSemantic
	default:
		return RecallKeyword
	}
}

// obsSourceAt returns src[i] or "" when i is out of range, guarding the
// observation/source parallel arrays (older caches may carry no sources).
func obsSourceAt(src []string, i int) string {
	if i < len(src) {
		return src[i]
	}
	return ""
}

// resolveSessionTitles maps session ids to their human-readable titles for the
// UI. Best-effort: on any error (or a missing id) it falls back to the raw id
// so provenance is never lost, just less pretty. Returns nil for no ids.
func resolveSessionTitles(projectSlug string, ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	titleByID := map[string]string{}
	if sessions, err := claudedata.ListSessions(projectSlug); err == nil {
		for _, s := range sessions {
			titleByID[s.ID] = s.Title
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		// Code-sourced provenance ("code:<path>") is not a session id; render it
		// as "代码 <path>" so the UI shows it came from the repository, not a chat.
		if strings.HasPrefix(id, "code:") {
			out = append(out, "代码 "+strings.TrimPrefix(id, "code:"))
			continue
		}
		// Task-sourced provenance ("task:<taskID>") comes from an accepted task's
		// writeback; render it as "任务 <id>" so the UI shows the fact was learned
		// from a task, not a chat session.
		if strings.HasPrefix(id, "task:") {
			out = append(out, "任务 "+strings.TrimPrefix(id, "task:"))
			continue
		}
		// Obsidian-sourced provenance ("obsidian:<note>") comes from a vault note;
		// render it as "笔记 <名>" so the UI shows the knowledge came from a note.
		if strings.HasPrefix(id, "obsidian:") {
			out = append(out, "笔记 "+strings.TrimPrefix(id, "obsidian:"))
			continue
		}
		if t := strings.TrimSpace(titleByID[id]); t != "" {
			out = append(out, t)
		} else {
			out = append(out, id)
		}
	}
	return out
}
