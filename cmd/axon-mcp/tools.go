package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"axon/internal/claudedata"
	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/retrieve"
	"axon/internal/secret"
)

// toolHandler owns everything a tool call needs: the data directory (where
// graphs/ and graphcache/ live), config + secrets to build an embedder, and a
// context for embedding calls. It is stateless across calls beyond that.
type toolHandler struct {
	ctx     context.Context
	dataDir string
	cfg     *config.Manager
	secrets secret.Store
	// probe memoizes cloud-embedder availability so recall doesn't re-probe the
	// network on every call.
	probe *embed.ProbeCache
}

// toolDef is one entry in the tools/list response.
type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// list returns the tool catalog advertised to the client.
func (h *toolHandler) list() map[string]interface{} {
	strProp := func(desc string) map[string]interface{} {
		return map[string]interface{}{"type": "string", "description": desc}
	}
	return map[string]interface{}{
		"tools": []toolDef{
			{
				Name:        "search_knowledge",
				Description: "查询某个项目沉淀的业务知识图谱：给一段自然语言 query，返回相关的实体+事实(结构)与原文片段(内容)，带来源标注。用于回忆这个项目以前的设计决策、踩过的坑、约束、接口约定等。project 可省略——省略时自动按当前工作目录定位项目，通常不用先调 list_projects。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug；省略则自动用当前工作目录定位。跨项目查询才需显式传（用 list_projects 获取）"),
						"query":   strProp("自然语言查询，例如“支付回调怎么做幂等”"),
					},
					"required": []string{"query"},
				},
			},
			{
				Name:        "project_overview",
				Description: "一次拿到当前项目的知识骨架：核心模块/服务、关键设计决策与约束、以及最常被提及的实体。冷启动首选——开工前先调它建立整体认知，再按需 search_knowledge 深挖。project 可省略（自动用当前工作目录定位）。纯本地、不调模型。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug；省略则自动用当前工作目录定位"),
					},
				},
			},
			{
				Name:        "list_projects",
				Description: "列出所有已经建过知识图谱的项目（slug + 可读路径 + 实体数），用于知道有哪些项目可查。",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				Name:        "get_entity",
				Description: "查看某项目里一个实体的全部信息：observations(事实) + 关系 + 别名。名字支持别名与大小写不敏感匹配。project 可省略——省略时自动按当前工作目录定位项目。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug；省略则自动用当前工作目录定位"),
						"name":    strProp("实体名或其别名"),
					},
					"required": []string{"name"},
				},
			},
			{
				Name: "remember_knowledge",
				Description: "把本次对话里学到的、以后还用得上的持久业务知识写回该项目的知识图谱（让它越用越懂）。" +
					"只记录持久知识：设计决策及理由、约束/坑、接口约定、模块职责与关系；不要记临时调试、寒暄或一次性操作。" +
					"通过别名归一，新知识会自动并入已有的同名实体。写入后立即对后续 search_knowledge / get_entity 生效。project 可省略——省略时自动按当前工作目录定位项目（新项目也会据此自动建图）。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug；省略则自动用当前工作目录定位/新建"),
						"entities": map[string]interface{}{
							"type":        "array",
							"description": "要记住的实体列表。每个实体：name(实体名) + type(module|service|concept|decision|constraint) + observations(关于它的事实，一句话一条) + 可选 aliases(别名/中英文/简称)。",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name":         strProp("实体名"),
									"type":         strProp("module|service|concept|decision|constraint"),
									"observations": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "关于它的持久事实，一句话一条"},
									"aliases":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "别名（同一事物的其他叫法），没有就省略"},
								},
								"required": []string{"name", "observations"},
							},
						},
						"relations": map[string]interface{}{
							"type":        "array",
							"description": "可选：实体间的关系。每条：from(实体A) + to(实体B) + label(关系，如 依赖/调用/属于)。",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"from":  strProp("实体A"),
									"to":    strProp("实体B"),
									"label": strProp("关系，如 依赖/调用/属于"),
								},
								"required": []string{"from", "to", "label"},
							},
						},
					},
					"required": []string{"entities"},
				},
			},
		},
	}
}

// callParams is the tools/call request shape.
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// toolResult is the MCP tools/call result: a list of content blocks. isError
// flags a tool-level (not protocol-level) failure so the model sees the message.
type toolResult struct {
	Content []contentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func textResult(s string) *toolResult {
	return &toolResult{Content: []contentBlock{{Type: "text", Text: s}}}
}

// call dispatches a tools/call by tool name.
func (h *toolHandler) call(raw json.RawMessage) (*toolResult, error) {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parse tools/call params: %w", err)
	}
	switch p.Name {
	case "search_knowledge":
		return h.searchKnowledge(p.Arguments)
	case "project_overview":
		return h.projectOverview(p.Arguments)
	case "list_projects":
		return h.listProjects()
	case "get_entity":
		return h.getEntity(p.Arguments)
	case "remember_knowledge":
		return h.rememberKnowledge(p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool %q", p.Name)
	}
}

// listProjects returns every project that has a built graph cache, with its
// readable path and entity count, so the client knows what it can query.
func (h *toolHandler) listProjects() (*toolResult, error) {
	projects, err := claudedata.ListProjects()
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	// The slug auto-resolved from cwd, so the catalog can flag "← 当前目录" and
	// the agent knows it can just omit project on subsequent calls.
	current, _ := h.resolveSlug("")

	var b strings.Builder
	b.WriteString("已建知识图谱的项目：\n\n")
	found := 0
	for _, p := range projects {
		g, err := retrieve.AssembleGraph(h.dataDir, p.Slug)
		if err != nil || len(g.Entities) == 0 {
			continue // no cache yet: not queryable, skip
		}
		found++
		marker := ""
		if p.Slug == current {
			marker = "  ← 当前目录（省略 project 即用它）"
		}
		fmt.Fprintf(&b, "- slug: `%s`%s\n  路径: %s\n  实体数: %d\n", p.Slug, marker, p.Path, len(g.Entities))
	}
	if found == 0 {
		return textResult("还没有任何已建索引的项目。请先在 axon GUI 里对项目「建索引」。"), nil
	}
	return textResult(b.String()), nil
}

// searchKnowledgeArgs is the search_knowledge argument shape.
type searchKnowledgeArgs struct {
	Project string `json:"project"`
	Query   string `json:"query"`
}

// searchKnowledge runs the shared HybridRAG recall for a project and renders the
// matched structure + verbatim chunks as markdown for the model to read.
func (h *toolHandler) searchKnowledge(raw json.RawMessage) (*toolResult, error) {
	var a searchKnowledgeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	a.Query = strings.TrimSpace(a.Query)
	if a.Query == "" {
		return nil, fmt.Errorf("query 必填")
	}
	slug, src := h.resolveSlug(a.Project)
	if slug == "" {
		return nil, fmt.Errorf("无法确定项目：请显式传 project，或用 list_projects 查看可用项目")
	}
	a.Project = slug

	g, err := retrieve.AssembleGraph(h.dataDir, a.Project)
	if err != nil {
		return nil, fmt.Errorf("assemble graph: %w", err)
	}
	if len(g.Entities) == 0 {
		return textResult(noGraphMsg(a.Project, src)), nil
	}

	// Embed the query per the configured mode. A nil embedder means semantic
	// mode was on but cloud is unavailable: run keyword-only (no query vector),
	// never a local vector masquerading as semantic.
	var qv []float32
	emb := newEmbedder(h.ctx, h.cfg, h.secrets, h.probe)
	local := emb != nil && emb.Model() == embed.LocalModelID
	semanticUnavailable := emb == nil
	if emb != nil {
		if v, e := emb.Embed(h.ctx, a.Query); e == nil {
			qv = v
		}
	}

	chunks := retrieve.LoadChunks(h.dataDir, a.Project)
	res := retrieve.RecallWithOpts(g, chunks, qv, a.Query, retrieve.RecallOptsFor(local))
	if len(res.Hit) == 0 && len(res.Chunks) == 0 {
		msg := fmt.Sprintf("在项目 `%s` 里没有查到和「%s」相关的知识。", a.Project, a.Query)
		if semanticUnavailable {
			msg += "\n\n> 注：已开启语义模式，但云端 embedding 当前不可用，本次仅按关键词召回。请在 axon 设置里检查 embedding 配置或改用关键词模式。"
		}
		return textResult(msg), nil
	}

	titles := sessionTitleMap(a.Project)
	var b strings.Builder
	fmt.Fprintf(&b, "# 项目 `%s` 与「%s」相关的业务知识\n", a.Project, a.Query)
	if local {
		b.WriteString("\n> 注：当前为关键词模式（本地词面向量），语义召回为字面近似，精度有限。\n")
	}
	if semanticUnavailable {
		b.WriteString("\n> 注：已开启语义模式，但云端 embedding 当前不可用，本次仅按关键词召回（精度受限）。\n")
	}
	writeStructure(&b, g, res.Hit, titles)
	writeChunks(&b, res.Chunks, titles)
	return textResult(b.String()), nil
}

// getEntityArgs is the get_entity argument shape.
type getEntityArgs struct {
	Project string `json:"project"`
	Name    string `json:"name"`
}

// getEntity returns one entity's full observations, relations and aliases.
// Lookup is case-insensitive and matches on the entity name or any alias.
func (h *toolHandler) getEntity(raw json.RawMessage) (*toolResult, error) {
	var a getEntityArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return nil, fmt.Errorf("name 必填")
	}
	slug, src := h.resolveSlug(a.Project)
	if slug == "" {
		return nil, fmt.Errorf("无法确定项目：请显式传 project，或用 list_projects 查看可用项目")
	}
	a.Project = slug
	g, err := retrieve.AssembleGraph(h.dataDir, a.Project)
	if err != nil {
		return nil, fmt.Errorf("assemble graph: %w", err)
	}
	if len(g.Entities) == 0 {
		return textResult(noGraphMsg(a.Project, src)), nil
	}
	e, ok := findEntity(g, a.Name)
	if !ok {
		return textResult(fmt.Sprintf("项目 `%s` 里没有找到实体「%s」。", a.Project, a.Name)), nil
	}

	titles := sessionTitleMap(a.Project)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s（%s）\n", e.Name, e.Type)
	if len(e.Aliases) > 0 {
		fmt.Fprintf(&b, "\n别名：%s\n", strings.Join(e.Aliases, "、"))
	}
	if len(e.Observations) > 0 {
		b.WriteString("\n## 事实\n")
		for i, o := range e.Observations {
			if src := obsSourceAt(e.ObsSources, i); src != "" {
				fmt.Fprintf(&b, "- %s （来源：%s）\n", o, renderSource(src, titles))
			} else {
				fmt.Fprintf(&b, "- %s\n", o)
			}
		}
	}
	rels := relationsOf(g, e.Name)
	if len(rels) > 0 {
		b.WriteString("\n## 关系\n")
		for _, r := range rels {
			fmt.Fprintf(&b, "- %s —%s→ %s\n", r.From, r.Label, r.To)
		}
	}
	return textResult(b.String()), nil
}

// noGraphMsg explains an empty-graph result differently by how the slug was
// resolved: an auto-derived slug that has no cache usually means "this project
// isn't indexed yet" rather than "you passed the wrong slug".
func noGraphMsg(slug string, src slugSource) string {
	switch src {
	case slugMatched, slugExplicit:
		return fmt.Sprintf("项目 `%s` 还没有知识图谱（未建索引或 slug 不对，用 list_projects 确认）。", slug)
	default: // slugDerived: cwd had no matching cache dir
		return fmt.Sprintf("当前目录还没有建过知识图谱（自动定位到 slug `%s`）。先在 axon GUI 里对本项目「建索引」，或用 list_projects 看有哪些已建项目、再显式传 project。", slug)
	}
}

// findEntity looks an entity up by name or alias, case-insensitively.
func findEntity(g *graph.Graph, name string) (graph.Entity, bool) {
	ln := strings.ToLower(strings.TrimSpace(name))
	for _, e := range g.Entities {
		if strings.ToLower(strings.TrimSpace(e.Name)) == ln {
			return e, true
		}
		for _, al := range e.Aliases {
			if strings.ToLower(strings.TrimSpace(al)) == ln {
				return e, true
			}
		}
	}
	return graph.Entity{}, false
}

// relationsOf returns relations that touch the given entity name (either end).
func relationsOf(g *graph.Graph, name string) []graph.Relation {
	ln := strings.ToLower(name)
	var out []graph.Relation
	for _, r := range g.Relations {
		if strings.ToLower(r.From) == ln || strings.ToLower(r.To) == ln {
			out = append(out, r)
		}
	}
	return out
}
