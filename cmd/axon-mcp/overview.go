package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"axon/internal/graph"
	"axon/internal/retrieve"
)

// project_overview answers the cold-start question every fresh agent (and every
// spawned subagent, which starts with zero inherited context) has: "what is this
// project, roughly?" — WITHOUT needing to guess a search query first. It renders
// the graph's skeleton locally (no model call): the decisions/constraints worth
// knowing up front, plus the most-connected entities. It is the intended first
// call, after which search_knowledge drills into specifics.

// overviewArgs is the project_overview argument shape. project is optional and
// auto-resolves from cwd like the other tools.
type overviewArgs struct {
	Project string `json:"project"`
}

// maxOverviewPer caps how many items each section lists, keeping the overview a
// scannable brief rather than a full graph dump.
const maxOverviewPer = 12

// projectOverview renders a project's knowledge skeleton: decisions & constraints
// first (the "why / what to watch out for" an agent most needs), then the
// most-connected entities (modules/services), each with a one-line fact.
func (h *toolHandler) projectOverview(raw json.RawMessage) (*toolResult, error) {
	var a overviewArgs
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("parse arguments: %w", err)
		}
	}
	slug, src := h.resolveSlug(a.Project)
	if slug == "" {
		return nil, fmt.Errorf("无法确定项目：请显式传 project，或用 list_projects 查看可用项目")
	}

	g, err := retrieve.AssembleGraph(h.dataDir, slug)
	if err != nil {
		return nil, fmt.Errorf("assemble graph: %w", err)
	}
	if len(g.Entities) == 0 {
		return textResult(noGraphMsg(slug, src)), nil
	}

	degree := entityDegree(g)
	var decisions, constraints, core []graph.Entity
	for _, e := range g.Entities {
		switch strings.ToLower(strings.TrimSpace(e.Type)) {
		case "decision":
			decisions = append(decisions, e)
		case "constraint":
			constraints = append(constraints, e)
		default:
			core = append(core, e)
		}
	}
	// Core entities: most-connected first, ties broken by fact count, so the
	// structural hubs surface at the top.
	sort.SliceStable(core, func(i, j int) bool {
		di, dj := degree[normEntKey(core[i])], degree[normEntKey(core[j])]
		if di != dj {
			return di > dj
		}
		return len(core[i].Observations) > len(core[j].Observations)
	})

	var b strings.Builder
	fmt.Fprintf(&b, "# 项目 `%s` 速览\n\n实体 %d · 关系 %d\n", slug, len(g.Entities), len(g.Relations))
	writeOverviewSection(&b, "关键决策", decisions)
	writeOverviewSection(&b, "约束与坑", constraints)
	writeOverviewSection(&b, "核心实体", core)
	b.WriteString("\n> 需要某个主题的细节，用 search_knowledge 深挖；想看某个实体全部事实与关系，用 get_entity。\n")
	return textResult(b.String()), nil
}

// entityDegree counts how many relations touch each entity (by normalized name),
// a cheap structural proxy for "how central is this to the project."
func entityDegree(g *graph.Graph) map[string]int {
	d := make(map[string]int, len(g.Entities))
	for _, r := range g.Relations {
		d[graph.NormName(r.From)]++
		d[graph.NormName(r.To)]++
	}
	return d
}

// normEntKey is the degree-map key for an entity: its normalized name.
func normEntKey(e graph.Entity) string { return graph.NormName(e.Name) }

// writeOverviewSection renders up to maxOverviewPer entities under a heading,
// each as name + first fact. Empty sections are skipped.
func writeOverviewSection(b *strings.Builder, title string, ents []graph.Entity) {
	if len(ents) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for i, e := range ents {
		if i >= maxOverviewPer {
			fmt.Fprintf(b, "- …还有 %d 个（用 search_knowledge 深挖）\n", len(ents)-maxOverviewPer)
			break
		}
		fact := ""
		if len(e.Observations) > 0 {
			fact = "：" + e.Observations[0]
		}
		fmt.Fprintf(b, "- **%s**%s\n", e.Name, fact)
	}
}
