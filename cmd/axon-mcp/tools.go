package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"axon/internal/config"
	"axon/internal/embed"
	"axon/internal/graph"
	"axon/internal/retrieve"
	"axon/internal/secret"
)

// toolHandler owns everything a tool call needs: the data directory (where
// graphs/ and graphcache/ live), config + secrets to build an embedder, and a
// context for embedding calls.
type toolHandler struct {
	ctx     context.Context
	dataDir string
	cfg     *config.Manager
	secrets secret.Store
	// probe memoizes cloud-embedder availability so recall doesn't re-probe the
	// network on every call.
	probe *embed.ProbeCache
	// activeScope holds the session-level namespace scope. When non-empty,
	// search/remember/get_entity are restricted to these namespaces only.
	// Set by set_scope, cleared by clear_scope.
	activeScope []string
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
				Name: "search_knowledge",
				Description: "查询项目沉淀的业务知识图谱：给一段自然语言 query，返回相关的实体+事实(结构)与原文片段(内容)，带来源标注。" +
					"用于回忆设计决策、踩过的坑、约束、接口约定等。" +
					"project 省略时默认搜索所有命名空间，确保信息完整不遗漏。" +
					"涉及跨服务交互时可传逗号分隔的多命名空间（如 \"gaia,gaiac\"）同时搜索；传 \"*\" 搜全部。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("可选，限定搜索范围: 单个(\"gaia\")、逗号分隔多个(\"gaia,gaiac\")；省略则搜索所有命名空间"),
						"query":   strProp("自然语言查询"),
					},
					"required": []string{"query"},
				},
			},
			{
				Name:        "project_overview",
				Description: "一次拿到项目的知识骨架：核心模块/服务、关键设计决策与约束、以及最常被提及的实体。冷启动首选——开工前先调它建立整体认知，再按需 search_knowledge 深挖。project 省略时从 .axon-project 自动读取。纯本地、不调模型。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目命名空间；省略则从 .axon-project 自动读取"),
					},
				},
			},
			{
				Name:        "list_projects",
				Description: "列出所有已建知识图谱的命名空间（名称 + 实体数），用于知道有哪些项目可查。",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				Name:        "get_entity",
				Description: "查看某项目里一个实体的全部信息：observations(事实) + 关系 + 别名。名字支持别名与大小写不敏感匹配。project 省略时从 .axon-project 自动读取。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目命名空间；省略则从 .axon-project 自动读取"),
						"name":    strProp("实体名或其别名"),
					},
					"required": []string{"name"},
				},
			},
			{
				Name: "remember_knowledge",
				Description: "把本次对话里学到的、以后还用得上的持久业务知识写回该项目的知识图谱（让它越用越懂）。" +
					"只记录持久知识：设计决策及理由、约束/坑、接口约定、模块职责与关系；不要记临时调试、寒暄或一次性操作。" +
					"通过别名归一，新知识会自动并入已有的同名实体。写入后立即对后续 search_knowledge / get_entity 生效。" +
					"project 省略时从 .axon-project 自动读取命名空间（新项目也会据此自动建图）。" +
					"注意：知识有归属方向，必须写入正确的命名空间——A 项目的知识不要写到 B 项目。跨项目通用知识写入 _global_。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目命名空间（如 gaia、glite、axon、_global_）；省略则从 .axon-project 自动读取"),
						"entities": map[string]interface{}{
							"type":        "array",
							"description": "要记住的实体列表。每个实体：name(实体名) + type(module|service|concept|decision|constraint) + observations(关于它的事实，一句话一条) + 可选 aliases(别名/中英文/简称)。",
							"items": map[string]interface{}{
								"type": "object",
								"properties": map[string]interface{}{
									"name":         strProp("实体名"),
									"type":         strProp("module|service|concept|decision|constraint"),
									"observations": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "关于它的明确事实陈述，一句话一条。要求：用确定性语言（'已决定X'/'必须Y'/'不能Z'），避免模糊词（可能/大概/也许）；如果是决策写明理由；如果是约束写明原因。"},
									"aliases":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "别名（同一事物的其他叫法），没有就省略"},
								},
								"required": []string{"name", "observations"},
							},
						},
						"relations": map[string]interface{}{
							"type":        "array",
							"description": "可选：实体间的有向关系。每条：from(主语) + to(宾语) + label(动词/关系)。方向必须清晰：from 是动作发起方，to 是接收方。例：A —调用→ B（A调B）、A —依赖→ B（A靠B）、A —包含→ B（A里有B）、A —属于→ B（A是B的一部分）、A —部署于→ B（A跑在B上）。",
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
			{
				Name:        "delete_entity",
				Description: "从项目知识图谱中删除一个实体及其所有关系。用于清理噪音、过期或错误的实体。project 省略时从 .axon-project 自动读取。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"project": strProp("项目命名空间；省略则从 .axon-project 自动读取"),
						"name":    strProp("要删除的实体名（大小写不敏感，支持别名匹配）"),
					},
					"required": []string{"name"},
				},
			},
			{
				Name: "set_scope",
				Description: "锁定本次会话的知识图谱范围。设置后，search_knowledge/remember_knowledge/get_entity 只在指定命名空间内操作。" +
					"用于聚焦特定项目避免干扰。用 clear_scope 恢复全量搜索。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"projects": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "要激活的命名空间列表，如 [\"gaia\", \"gaiac\"]",
						},
					},
					"required": []string{"projects"},
				},
			},
			{
				Name:        "clear_scope",
				Description: "清除会话范围锁定，恢复为默认搜索全部命名空间。",
				InputSchema: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				Name: "move_entity",
				Description: "把一个实体从源命名空间移动到目标命名空间。用于纠正实体归属——比如把误写到 gaia 的实体移到 gaiac。",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": strProp("要移动的实体名"),
						"from": strProp("源命名空间"),
						"to":   strProp("目标命名空间"),
					},
					"required": []string{"name", "from", "to"},
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
	case "delete_entity":
		return h.deleteEntity(p.Arguments)
	case "set_scope":
		return h.setScope(p.Arguments)
	case "clear_scope":
		return h.clearScope()
	case "move_entity":
		return h.moveEntity(p.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool %q", p.Name)
	}
}

// listProjects returns every namespace that has a built graph cache, with its
// entity count, so the client knows what it can query.
func (h *toolHandler) listProjects() (*toolResult, error) {
	namespaces := h.namespaceDirs()
	current, _ := h.resolveSlug("")

	var b strings.Builder
	b.WriteString("已建知识图谱的命名空间：\n\n")
	found := 0
	for _, ns := range namespaces {
		g, err := retrieve.AssembleGraph(h.dataDir, ns)
		if err != nil || len(g.Entities) == 0 {
			continue
		}
		found++
		marker := ""
		if ns == current {
			marker = "  ← 当前项目（省略 project 即用它）"
		}
		fmt.Fprintf(&b, "- `%s`%s — 实体数: %d\n", ns, marker, len(g.Entities))
	}
	if found == 0 {
		return textResult("还没有任何已建索引的命名空间。用 remember_knowledge 写入第一条知识即可自动创建。"), nil
	}
	return textResult(b.String()), nil
}

// searchKnowledgeArgs is the search_knowledge argument shape.
type searchKnowledgeArgs struct {
	Project string `json:"project"`
	Query   string `json:"query"`
}

// searchKnowledge runs the shared HybridRAG recall for one or more namespaces
// and renders the matched structure + verbatim chunks as markdown.
func (h *toolHandler) searchKnowledge(raw json.RawMessage) (*toolResult, error) {
	var a searchKnowledgeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse arguments: %w", err)
	}
	a.Query = strings.TrimSpace(a.Query)
	if a.Query == "" {
		return nil, fmt.Errorf("query 必填")
	}

	// Resolve target namespaces.
	slugs := h.resolveSearchSlugs(a.Project)
	if len(slugs) == 0 {
		return nil, fmt.Errorf("无法确定项目：当前目录没有 .axon-project 文件，请显式传 project 参数或创建 .axon-project")
	}

	// Embed the query once (shared across all namespace searches).
	var qv []float32
	emb := newEmbedder(h.ctx, h.cfg, h.secrets, h.probe)
	local := emb != nil && emb.Model() == embed.LocalModelID
	semanticUnavailable := emb == nil
	if emb != nil {
		if v, e := emb.Embed(h.ctx, a.Query); e == nil {
			qv = v
		}
	}

	var b strings.Builder
	b.WriteString("> 以下知识来自历史对话蒸馏和手动记录，仅供参考。涉及具体实现细节时以代码为准；标注⚠️的条目超过90天可能已过时。\n\n")
	anyHit := false
	const maxTotalChars = 8000 // cap total output to avoid overwhelming the agent

	for _, slug := range slugs {
		if b.Len() >= maxTotalChars {
			b.WriteString("\n> （结果已截断，如需更多请指定具体 project 参数缩小范围）\n")
			break
		}
		g, err := retrieve.AssembleGraph(h.dataDir, slug)
		if err != nil || len(g.Entities) == 0 {
			continue
		}
		chunks := retrieve.LoadChunks(h.dataDir, slug)
		res := retrieve.RecallWithOpts(g, chunks, qv, a.Query, retrieve.RecallOptsFor(local))
		if len(res.Hit) == 0 && len(res.Chunks) == 0 {
			continue
		}
		anyHit = true
		titles := sessionTitleMap(slug)
		fmt.Fprintf(&b, "# 项目 `%s` 与「%s」相关的业务知识\n", slug, a.Query)
		writeStructure(&b, g, res.Hit, titles)
		writeChunks(&b, res.Chunks, titles)
		b.WriteString("\n---\n\n")
	}

	if !anyHit {
		msg := fmt.Sprintf("在命名空间 %v 里没有查到和「%s」相关的知识。", slugs, a.Query)
		if semanticUnavailable {
			msg += "\n\n> 注：已开启语义模式，但云端 embedding 当前不可用，本次仅按关键词召回。"
		}
		return textResult(msg), nil
	}

	if local {
		b.WriteString("\n> 注：当前为关键词模式（本地词面向量），语义召回为字面近似，精度有限。\n")
	}
	if semanticUnavailable {
		b.WriteString("\n> 注：已开启语义模式，但云端 embedding 当前不可用，本次仅按关键词召回（精度受限）。\n")
	}
	return textResult(b.String()), nil
}

// resolveSearchSlugs interprets the project parameter for search_knowledge:
//   - empty: current project + _global_
//   - "*": all namespaces
//   - comma-separated: each named namespace
//   - single string: just that namespace
func (h *toolHandler) resolveSearchSlugs(project string) []string {
	project = strings.TrimSpace(project)
	if project == "" {
		// If a scope is active, use it; otherwise search all.
		if len(h.activeScope) > 0 {
			return h.activeScope
		}
		return h.namespaceDirs()
	}
	if project == "*" {
		return h.namespaceDirs()
	}
	parts := strings.Split(project, ",")
	var out []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
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
		return nil, fmt.Errorf("无法确定项目：当前目录没有 .axon-project 文件，请显式传 project 参数或创建 .axon-project")
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
// resolved: an explicit or mapped slug that has no cache usually means "this
// namespace isn't indexed yet" rather than "you passed the wrong name".
func noGraphMsg(slug string, src slugSource) string {
	switch src {
	case slugMapped, slugExplicit:
		return fmt.Sprintf("命名空间 `%s` 还没有知识图谱（用 list_projects 确认可用命名空间，或用 remember_knowledge 写入第一条知识）。", slug)
	default: // slugUnknown
		return "无法确定项目：当前目录没有 .axon-project 文件。请在项目根目录创建 .axon-project（内容为命名空间名），或显式传 project 参数。"
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
