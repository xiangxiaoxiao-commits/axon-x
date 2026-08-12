package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"axon/internal/claudedata"
	"axon/internal/db"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/provider"
)

// graphModel is the model used to distill knowledge. gpt-5.6-sol has the
// strongest comprehension on the user's gateway; extraction needs judgement.
const graphModel = "gpt-5.6-sol"

// HybridRAG retrieval tuning for MatchKnowledge. Named so they are easy to
// adjust as the graph grows.
const (
	// knowledgeSeedMinScore is the cosine-similarity floor for an entity to be
	// picked as a semantic seed. Below it the entity is considered unrelated.
	knowledgeSeedMinScore = 0.35
	// knowledgeSeedTopK caps how many semantic seed entities are taken (the
	// closest ones), before relation expansion.
	knowledgeSeedTopK = 5
	// knowledgeExpandHops is how many relation hops to walk out from the seeds
	// (undirected). 1 keeps the injected context tight; bump to 2 for wider recall.
	knowledgeExpandHops = 1
)

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
		Model: graphModel,
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
// keeping token cost predictable (newest content matters most for knowledge).
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
		return fmt.Errorf("需要一个 OpenAI 协议的 provider 来调用 %s（去设置配一个）", graphModel)
	}
	pc, _ := a.cfg.Provider(name)
	prov, err := a.newProvider(pc)
	if err != nil {
		return err
	}
	sessions, err := claudedata.ListSessions(projectSlug)
	if err != nil {
		return err
	}

	// Build an embedder for semantic (HybridRAG) retrieval. Optional: if none is
	// available, indexing proceeds without vectors and MatchKnowledge falls back
	// to substring matching.
	emb, embErr := a.newEmbedder()
	if embErr != nil {
		emb = nil
	}

	newly := 0
	for i, s := range sessions {
		if _, ok := graph.LoadCache(dataDir, projectSlug, s.ID, s.UpdatedAt); ok {
			continue // fresh cache, skip
		}
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "current": i + 1, "total": len(sessions),
			"title": s.Title, "phase": "index",
		})
		msgs, err := claudedata.ReadSession(projectSlug, s.ID)
		if err != nil {
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
		// Attach embeddings so this session's entities are semantically searchable.
		if emb != nil {
			a.embedEntities(emb, ex.Entities)
		}
		_ = graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: s.ID, Mtime: s.UpdatedAt, Entities: ex.Entities, Relations: ex.Relations,
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
	if term != "" {
		g = filterGraph(g, term)
	}
	g.UpdatedAt = time.Now().UnixMilli()
	_ = graph.Save(dataDir, g)
	return g, nil
}

// buildTranscript joins user/assistant text into a bounded transcript.
func buildTranscript(msgs []claudedata.SessionMessage) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Text)
		b.WriteString("\n\n")
		if b.Len() > maxTranscriptChars {
			break
		}
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
		return "", fmt.Errorf("需要一个 OpenAI 协议的 provider 来调用 %s", graphModel)
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
		Model:       graphModel,
		Messages:    []provider.ChatMessage{{Role: provider.RoleSystem, Content: articlePrompt}, {Role: provider.RoleUser, Content: b.String()}},
		Temperature: 0.3, MaxTokens: 3000,
	})
}

// KnowledgeMatch is what the chat injects: the entities whose names appear in
// the user's message, a readable context block, and the matched names for the
// UI to show ("injected: X, Y").
type KnowledgeMatch struct {
	Names   []string `json:"names"`
	Context string   `json:"context"`
	// Sources are the distinct session titles the injected observations came
	// from, so the UI can show "这条知识来自 xxx 会话". Best-effort: empty when
	// the graph carries no provenance yet (older caches) or titles can't be
	// resolved. Session ids that have no known title fall back to the raw id.
	Sources []string `json:"sources,omitempty"`
}

// MatchKnowledge finds the graph knowledge relevant to the user's message using
// HybridRAG and builds an injectable context block. Retrieval combines:
//  1. semantic seeds — entities whose embedding is closest to the message
//     (cosine >= threshold, top-K), when an embedder and entity vectors exist;
//  2. relation expansion — neighbors of the seeds walked out a few hops
//     (undirected), so linked knowledge comes along;
//  3. substring fallback — entities whose name literally appears in the message
//     (the original behavior), always applied and merged in.
//
// When no embedder is configured (e.g. no OpenAI provider) or the graph has no
// vectors yet, it degrades gracefully to pure substring matching. Assembled from
// cache (instant). Empty match => Names is empty and Context "".
func (a *App) MatchKnowledge(projectSlug, text string) (KnowledgeMatch, error) {
	if strings.TrimSpace(text) == "" || strings.TrimSpace(projectSlug) == "" {
		return KnowledgeMatch{Names: []string{}}, nil
	}
	g, err := a.assembleGraph(projectSlug, "")
	if err != nil {
		return KnowledgeMatch{Names: []string{}}, err
	}

	// Index entities by normalized name for O(1) lookup during expansion.
	byName := make(map[string]graph.Entity, len(g.Entities))
	for _, e := range g.Entities {
		if n := strings.ToLower(strings.TrimSpace(e.Name)); n != "" {
			byName[n] = e
		}
	}

	hit := map[string]bool{} // normalized name -> in the hit set

	// (1) Semantic seeds via embeddings, then (2) expand along relations.
	if emb, embErr := a.newEmbedder(); embErr == nil {
		seeds := a.semanticSeeds(emb, g, text)
		for _, s := range seeds {
			hit[strings.ToLower(s)] = true
		}
		if len(seeds) > 0 {
			expandAlongRelations(g, hit, knowledgeExpandHops)
		}
	}

	// (3) Substring fallback: any entity whose name — or any of its aliases —
	// appears in the message. Always applied and merged, so this both complements
	// the vector seeds and preserves the original behavior when embeddings are
	// unavailable. Matching an alias hits the entity under its canonical name.
	lt := strings.ToLower(text)
	for _, e := range g.Entities {
		n := strings.TrimSpace(e.Name)
		if n == "" {
			continue
		}
		matched := strings.Contains(lt, strings.ToLower(n))
		if !matched {
			for _, al := range e.Aliases {
				al = strings.TrimSpace(al)
				if al != "" && strings.Contains(lt, strings.ToLower(al)) {
					matched = true
					break
				}
			}
		}
		if matched {
			hit[strings.ToLower(n)] = true
		}
	}

	if len(hit) == 0 {
		return KnowledgeMatch{Names: []string{}}, nil
	}

	// Collect matched entities in the graph's original order for stable output.
	// srcSeen tracks the distinct session ids the injected observations came from.
	names := make([]string, 0, len(hit))
	srcSeen := map[string]bool{}
	var srcIDs []string
	var b strings.Builder
	b.WriteString("以下是该项目的相关背景知识（来自你以往的对话，供参考）：\n")
	for _, e := range g.Entities {
		if !hit[strings.ToLower(strings.TrimSpace(e.Name))] {
			continue
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
	return KnowledgeMatch{Names: names, Context: b.String(), Sources: resolveSessionTitles(projectSlug, srcIDs)}, nil
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
		if t := strings.TrimSpace(titleByID[id]); t != "" {
			out = append(out, t)
		} else {
			out = append(out, id)
		}
	}
	return out
}

// semanticSeeds embeds the message and returns the names of the top-K entities
// whose embedding is most similar (cosine >= knowledgeSeedMinScore). Returns nil
// when the message can't be embedded or no entity carries a vector, so the
// caller falls back to substring matching without erroring.
func (a *App) semanticSeeds(emb embed.Embedder, g *graph.Graph, text string) []string {
	qv, err := emb.Embed(a.ctx, text)
	if err != nil {
		log.Printf("axon: knowledge query embed failed: %v", err)
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
		if score < knowledgeSeedMinScore {
			continue
		}
		cands = append(cands, scored{name: e.Name, score: score})
	}
	if len(cands) == 0 {
		return nil
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].score > cands[j].score })
	if len(cands) > knowledgeSeedTopK {
		cands = cands[:knowledgeSeedTopK]
	}
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.name
	}
	return out
}

// expandAlongRelations grows the hit set by walking `hops` relation edges out
// from the currently-hit entities, treating relations as undirected so both
// upstream and downstream neighbors are pulled in. Only names present in the
// graph are added.
func expandAlongRelations(g *graph.Graph, hit map[string]bool, hops int) {
	for h := 0; h < hops; h++ {
		frontier := map[string]bool{}
		for _, r := range g.Relations {
			from, to := strings.ToLower(r.From), strings.ToLower(r.To)
			if hit[from] && !hit[to] {
				frontier[to] = true
			}
			if hit[to] && !hit[from] {
				frontier[from] = true
			}
		}
		if len(frontier) == 0 {
			break // nothing new to add
		}
		for n := range frontier {
			hit[n] = true
		}
	}
}
