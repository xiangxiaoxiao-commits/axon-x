package main

import (
	"fmt"
	"strings"
	"time"

	"axon/internal/claudedata"
	"axon/internal/graph"
	"axon/internal/retrieve"
)

// Injection budgets, mirroring the App so MCP output stays a comparable size.
const (
	structBudgetChars = 1600
	chunkBudgetChars  = 4000
)

// writeStructure renders the matched entities (with per-fact provenance) and the
// relations among them, into b, bounded by structBudgetChars.
//
// Direct matches and inferred neighbours are rendered differently on purpose. A
// direct match is the answer to the query, so it gets its facts in full. An
// expanded neighbour is a lead: on a real graph one seed fans out to a dozen
// nodes (a file pulls in every function it contains), and given a character
// budget, printing their facts would crowd out the assertions the query was
// actually asking for. Leads are therefore listed by name only — enough to know
// what to look up next, cheap enough that the answer still fits.
func writeStructure(b *strings.Builder, g *graph.Graph, hit map[string]bool, derived map[string]retrieve.Derivation, titles map[string]string) {
	wrote := false
	var body strings.Builder

	// Direct matches first: these answer the query.
	for _, e := range g.Entities {
		key := strings.ToLower(strings.TrimSpace(e.Name))
		if !hit[key] {
			continue
		}
		if _, inferred := derived[key]; inferred {
			continue
		}
		if body.Len() >= structBudgetChars {
			break
		}
		wrote = true
		fmt.Fprintf(&body, "\n### %s（%s）\n", e.Name, e.Type)
		for _, assertion := range graph.UsableAssertions(e, time.Now()) {
			fmt.Fprintf(&body, "- %s（置信度 %.0f%%", assertion.Text, assertion.Confidence*100)
			if len(assertion.Sources) > 0 {
				fmt.Fprintf(&body, "，来源：%s", renderSource(assertion.Sources[0], titles))
			}
			body.WriteString("）\n")
		}
	}

	var rels []string
	for _, r := range g.Relations {
		if graph.RelationUsableForRecall(r) && hit[strings.ToLower(r.From)] && hit[strings.ToLower(r.To)] {
			line := fmt.Sprintf("%s —%s→ %s（置信度 %.0f%%", r.From, r.Label, r.To, r.Confidence*100)
			if len(r.Sources) > 0 {
				// Provenance matters most for the trust split: 100% from the code
				// graph is a machine-checked structural fact, while 85% from a
				// session is a model's reading and worth re-checking.
				line += "，来源：" + renderSource(r.Sources[0], titles)
			}
			rels = append(rels, line+"）")
		}
	}

	// Leads last, by name only.
	var leads []string
	for _, e := range g.Entities {
		key := strings.ToLower(strings.TrimSpace(e.Name))
		if !hit[key] {
			continue
		}
		d, inferred := derived[key]
		if !inferred {
			continue
		}
		note := ""
		if d.Reverse {
			// The edge is stored as Via —Label→ this entity, but the query hit
			// this entity first, so the walk went against the arrow.
			note = "，反向"
		}
		leads = append(leads, fmt.Sprintf("%s〔经 %s —%s→%s〕", e.Name, d.Via, d.Label, note))
	}

	if !wrote && len(rels) == 0 && len(leads) == 0 {
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
	if len(leads) > 0 {
		b.WriteString("\n关联线索（通过关系扩展加入，非直接命中；需要时再单独查询）：\n")
		for _, l := range leads {
			b.WriteString("- " + l + "\n")
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
// session titles. For MCP-written knowledge (mcp:<timestamp>-<rand>), shows
// the age in days so the reader can judge timeliness.
func renderSource(id string, titles map[string]string) string {
	switch {
	case strings.HasPrefix(id, "code:"):
		// The bare "graph" key covers the whole repository skeleton (relations
		// carry it); anything else is a specific source file.
		name := strings.TrimPrefix(id, "code:")
		if name == "graph" {
			return "代码仓库"
		}
		return "代码 " + name
	case strings.HasPrefix(id, graph.ImportSourcePrefix):
		return "知识包 " + strings.TrimPrefix(id, graph.ImportSourcePrefix)
	case strings.HasPrefix(id, "task:"):
		return "任务 " + strings.TrimPrefix(id, "task:")
	case strings.HasPrefix(id, "obsidian:"):
		return "笔记 " + strings.TrimPrefix(id, "obsidian:")
	case strings.HasPrefix(id, "mcp:"):
		return "MCP写入" + ageFromMCPSource(id)
	case strings.HasPrefix(id, "moved:"):
		return "迁移"
	}
	if t := strings.TrimSpace(titles[id]); t != "" {
		return t
	}
	return id
}

// ageFromMCPSource parses the timestamp from "mcp:<millis>-<rand>" and returns
// a human-readable age string like " (3天前)" or " (>90天⚠️)".
func ageFromMCPSource(id string) string {
	// Format: mcp:1786602946922-0d231da5
	parts := strings.SplitN(strings.TrimPrefix(id, "mcp:"), "-", 2)
	if len(parts) == 0 {
		return ""
	}
	ts := int64(0)
	for _, c := range parts[0] {
		if c >= '0' && c <= '9' {
			ts = ts*10 + int64(c-'0')
		} else {
			break
		}
	}
	if ts == 0 {
		return ""
	}
	days := (time.Now().UnixMilli() - ts) / 86400000
	if days <= 0 {
		return " (今天)"
	}
	if days > 90 {
		return fmt.Sprintf(" (%d天前⚠️)", days)
	}
	return fmt.Sprintf(" (%d天前)", days)
}

// obsSourceAt returns src[i] or "" when out of range (parallel-array safety for
// older caches).
func obsSourceAt(src []string, i int) string {
	if i < len(src) {
		return src[i]
	}
	return ""
}
