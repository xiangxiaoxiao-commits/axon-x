package main

import (
	"fmt"
	"strings"

	"axon/internal/claudedata"
	"axon/internal/graph"
)

// Injection budgets, mirroring the App so MCP output stays a comparable size.
const (
	structBudgetChars = 1600
	chunkBudgetChars  = 4000
)

// writeStructure renders the matched entities (with per-fact provenance) and the
// relations among them, into b, bounded by structBudgetChars.
func writeStructure(b *strings.Builder, g *graph.Graph, hit map[string]bool, titles map[string]string) {
	wrote := false
	var body strings.Builder
	for _, e := range g.Entities {
		if !hit[strings.ToLower(strings.TrimSpace(e.Name))] {
			continue
		}
		if body.Len() >= structBudgetChars {
			break
		}
		wrote = true
		fmt.Fprintf(&body, "\n### %s（%s）\n", e.Name, e.Type)
		for i, o := range e.Observations {
			if src := obsSourceAt(e.ObsSources, i); src != "" {
				fmt.Fprintf(&body, "- %s （来源：%s）\n", o, renderSource(src, titles))
			} else {
				fmt.Fprintf(&body, "- %s\n", o)
			}
		}
	}
	var rels []string
	for _, r := range g.Relations {
		if hit[strings.ToLower(r.From)] && hit[strings.ToLower(r.To)] {
			rels = append(rels, fmt.Sprintf("%s —%s→ %s", r.From, r.Label, r.To))
		}
	}
	if !wrote && len(rels) == 0 {
		return
	}
	b.WriteString("\n## 结构（实体与关系）\n")
	b.WriteString(body.String())
	if len(rels) > 0 {
		b.WriteString("\n关系：\n")
		for _, r := range rels {
			b.WriteString("- " + r + "\n")
		}
	}
}

// writeChunks renders the recalled verbatim fragments, each tagged with its
// source, bounded by chunkBudgetChars (always keeping at least one).
func writeChunks(b *strings.Builder, chunks []graph.Chunk, titles map[string]string) {
	if len(chunks) == 0 {
		return
	}
	b.WriteString("\n## 相关原文片段（细节以原文为准）\n")
	budget := chunkBudgetChars
	written := 0
	for _, ch := range chunks {
		if budget-len(ch.Text) < 0 && written > 0 {
			break
		}
		fmt.Fprintf(b, "\n〔来源：%s〕\n%s\n\n---\n", renderSource(ch.Source, titles), ch.Text)
		budget -= len(ch.Text)
		written++
	}
}

// sessionTitleMap builds a session-id -> title map for a project (best-effort;
// empty on error or when the namespace doesn't correspond to a Claude Code
// project directory, in which case source ids render raw).
func sessionTitleMap(projectSlug string) map[string]string {
	out := map[string]string{}
	// Only attempt title lookup if the slug looks like a Claude Code path-encoded
	// slug (starts with '-'). Named namespaces (gaia, glite, etc.) don't map to
	// Claude Code session directories.
	if !strings.HasPrefix(projectSlug, "-") {
		return out
	}
	if sessions, err := claudedata.ListSessions(projectSlug); err == nil {
		for _, s := range sessions {
			out[s.ID] = s.Title
		}
	}
	return out
}

// renderSource resolves a chunk/observation source id to a display label,
// mirroring the App's provenance rendering for code/task/obsidian prefixes and
// session titles.
func renderSource(id string, titles map[string]string) string {
	switch {
	case strings.HasPrefix(id, "code:"):
		return "代码 " + strings.TrimPrefix(id, "code:")
	case strings.HasPrefix(id, "task:"):
		return "任务 " + strings.TrimPrefix(id, "task:")
	case strings.HasPrefix(id, "obsidian:"):
		return "笔记 " + strings.TrimPrefix(id, "obsidian:")
	}
	if t := strings.TrimSpace(titles[id]); t != "" {
		return t
	}
	return id
}

// obsSourceAt returns src[i] or "" when out of range (parallel-array safety for
// older caches).
func obsSourceAt(src []string, i int) string {
	if i < len(src) {
		return src[i]
	}
	return ""
}
