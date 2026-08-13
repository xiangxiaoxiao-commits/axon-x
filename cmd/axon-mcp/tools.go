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
				Description: "查询某个项目沉淀的业务知识图谱：给一段自然语言 query，返回相关的实体+事实(结构)与原文片段(内容)，带来源标注。用于回忆这个项目以前的设计决策、踩过的坑、约束、接口约定等。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug（用 list_projects 获取）"),
						"query":   strProp("自然语言查询，例如“支付回调怎么做幂等”"),
					},
					"required": []string{"project", "query"},
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
				Description: "查看某项目里一个实体的全部信息：observations(事实) + 关系 + 别名。名字支持别名与大小写不敏感匹配。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目 slug"),
						"name":    strProp("实体名或其别名"),
					},
					"required": []string{"project", "name"},
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
	case "list_projects":
		return h.listProjects()
	case "get_entity":
		return h.getEntity(p.Arguments)
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
	var b strings.Builder
	b.WriteString("已建知识图谱的项目：\n\n")
	found := 0
	for _, p := range projects {
		g, err := retrieve.AssembleGraph(h.dataDir, p.Slug)
		if err != nil || len(g.Entities) == 0 {
			continue // no cache yet: not queryable, skip
		}
		found++
		fmt.Fprintf(&b, "- slug: `%s`\n  路径: %s\n  实体数: %d\n", p.Slug, p.Path, len(g.Entities))
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
	a.Project, a.Query = strings.TrimSpace(a.Project), strings.TrimSpace(a.Query)
	if a.Project == "" || a.Query == "" {
		return nil, fmt.Errorf("project 和 query 都必填")
	}

	g, err := retrieve.AssembleGraph(h.dataDir, a.Project)
	if err != nil {
		return nil, fmt.Errorf("assemble graph: %w", err)
	}
	if len(g.Entities) == 0 {
		return textResult(fmt.Sprintf("项目 `%s` 还没有知识图谱（未建索引或 slug 不对，用 list_projects 确认）。", a.Project)), nil
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
	a.Project, a.Name = strings.TrimSpace(a.Project), strings.TrimSpace(a.Name)
	if a.Project == "" || a.Name == "" {
		return nil, fmt.Errorf("project 和 name 都必填")
	}
	g, err := retrieve.AssembleGraph(h.dataDir, a.Project)
	if err != nil {
		return nil, fmt.Errorf("assemble graph: %w", err)
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
