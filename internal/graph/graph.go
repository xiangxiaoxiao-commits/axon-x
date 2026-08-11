// Package graph is a per-project knowledge graph distilled from Claude Code
// conversations: entities (projects/modules/concepts), relations between them,
// and observations (durable facts). Stored as human-readable JSON, one file per
// project, and merged incrementally as new sessions are processed.
package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Entity is a node: a named thing worth remembering about the project.
type Entity struct {
	Name         string   `json:"name"`         // unique key within a graph
	Type         string   `json:"type"`         // module | service | concept | decision | ...
	Observations []string `json:"observations"` // durable facts about it
	// Embedding is the dense vector of name+observations, used for semantic
	// (HybridRAG) retrieval. Optional: empty when no embedder was available at
	// index time, in which case matching falls back to substring lookup.
	Embedding []float32 `json:"embedding,omitempty"`
}

// Relation is a directed edge between two entities.
type Relation struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"` // e.g. "依赖", "调用", "属于"
}

// Graph is one project's knowledge graph.
type Graph struct {
	ProjectSlug string     `json:"projectSlug"`
	Entities    []Entity   `json:"entities"`
	Relations   []Relation `json:"relations"`
	UpdatedAt   int64      `json:"updatedAt"`
	// SourceSessions records which session ids have already been absorbed, so a
	// re-run skips them (incremental optimization).
	SourceSessions []string `json:"sourceSessions"`
}

// Dir returns the graphs directory under the app data dir.
func Dir(dataDir string) string { return filepath.Join(dataDir, "graphs") }

func pathFor(dataDir, slug string) string {
	return filepath.Join(Dir(dataDir), filepath.Base(slug)+".json")
}

// Load reads a project's graph, or an empty graph if none exists yet.
func Load(dataDir, slug string) (*Graph, error) {
	p := pathFor(dataDir, slug)
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &Graph{ProjectSlug: slug, Entities: []Entity{}, Relations: []Relation{}, SourceSessions: []string{}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read graph: %w", err)
	}
	var g Graph
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("parse graph: %w", err)
	}
	return &g, nil
}

// Save writes a project's graph atomically.
func Save(dataDir string, g *Graph) error {
	if err := os.MkdirAll(Dir(dataDir), 0o755); err != nil {
		return fmt.Errorf("create graphs dir: %w", err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("encode graph: %w", err)
	}
	p := pathFor(dataDir, g.ProjectSlug)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write graph: %w", err)
	}
	return os.Rename(tmp, p)
}

// normKey lowercases and trims a name for case-insensitive de-duplication.
func normKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
