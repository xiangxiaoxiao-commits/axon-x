// Package orchestr is the stateful task-orchestration engine behind the
// "describe roughly → enrich into a spec → confirm → execute → review → iterate"
// loop. It is deliberately free of any dependency on the desktop app (package
// main) so the same engine can be driven by the Wails UI, a CLI, or an MCP
// server.
//
// Everything only the host knows how to do is injected:
//   - ProviderFactory: build a streaming model client for a task
//   - Recaller:        recall project background for enrichment
//   - Writebacker:     distill an accepted task back into the knowledge graph
//   - Emitter:         forward lifecycle events to the host's UI
package orchestr

import (
	"context"

	"axon/internal/provider"
	"axon/internal/task"
)

// ProviderFactory builds a live streaming client for a task. providerName and
// model may be empty, in which case the implementation is expected to fall back
// to its configured defaults and return the model it actually resolved.
type ProviderFactory func(ctx context.Context, providerName, model string) (provider.Provider, string, error)

// Knowledge is the project background recalled for one enrichment, plus enough
// provenance for the host to show the user what the model actually referenced.
type Knowledge struct {
	Context string   // rendered background block injected into the prompt
	Names   []string // matched business entity names
	Sources []string // originating session/record titles
	Method  string   // "semantic" | "hybrid" | "keyword" | "none"
	Local   bool     // recall used the local lexical fallback, not a real embedder
}

// Recaller recalls project knowledge for a task.
//
// Implementations must degrade gracefully: when the project has no graph yet
// (the common case for a freshly installed instance), return an empty Knowledge
// and a nil error. Recall must never block a task from being enriched.
type Recaller interface {
	Recall(ctx context.Context, projectSlug, query string) (Knowledge, error)
}

// Writebacker distills an accepted task into the project's knowledge graph so
// the graph grows as work is completed. It is best-effort and asynchronous: a
// failure must not affect the acceptance itself.
type Writebacker interface {
	WriteBack(ctx context.Context, t task.Task, result string) error
}

// Emitter forwards lifecycle events to the host. The payload values are the
// event structs defined in this package (TaskStatusEvent and friends); every
// payload carries the task id so a host running several tasks at once can route
// each event to the right card.
type Emitter func(event string, data ...interface{})

// nopEmitter discards events, for CLI use or tests.
func nopEmitter(string, ...interface{}) {}
