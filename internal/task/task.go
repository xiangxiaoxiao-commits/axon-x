// Package task is the domain model + persistence contract for the orchestration
// tool: a user writes a rough task, the model enriches it into a structured
// spec (filling in what the user left out), the user reviews/edits it, then it
// runs (possibly many in parallel), and the result is reviewed — accept, or
// reject-and-iterate (a new run carrying feedback).
package task

// Status is the task lifecycle state.
type Status string

const (
	StatusDraft        Status = "draft"         // just created, rough input only
	StatusEnriching    Status = "enriching"     // model is expanding it into a spec
	StatusReviewSpec   Status = "review_spec"   // spec ready, awaiting user confirm/edit
	StatusQueued       Status = "queued"        // confirmed, waiting for a run slot
	StatusExecuting    Status = "executing"     // a run is in progress
	StatusReviewResult Status = "review_result" // result ready, awaiting review
	StatusAccepted     Status = "accepted"      // terminal: user accepted the result
	StatusFailed       Status = "failed"        // errored; FailedStage says where; retryable
)

// Spec is the structured, enriched task specification. Every field is editable
// by the user before execution (the "fill in what you forgot" contract).
type Spec struct {
	Goal           string   `json:"goal"`           // 目标
	Background     string   `json:"background"`     // 背景/上下文
	Constraints    []string `json:"constraints"`    // 约束
	Scope          []string `json:"scope"`          // 涉及范围(文件/模块)
	AcceptCriteria []string `json:"acceptCriteria"` // 验收标准
	MissedPoints   []string `json:"missedPoints"`   // 易遗漏点(模型补的)
	Steps          []string `json:"steps"`          // 建议步骤

	// Enrichment provenance (not user-editable): what business knowledge the
	// knowledge graph injected into this enrichment and where it came from.
	// InjectedKnowledge holds the matched entity names, KnowledgeSources the
	// originating session titles. Both empty when no project was bound or the
	// graph recalled nothing — the UI surfaces this so users can trust what the
	// AI actually referenced.
	InjectedKnowledge []string `json:"injectedKnowledge"` // matched business entity names
	KnowledgeSources  []string `json:"knowledgeSources"`  // source session titles
	// RecallMethod records how the knowledge was recalled: "semantic"/"hybrid"
	// (true vector recall), "keyword" (degraded substring matching — embedder
	// unavailable or graph carried no vectors), or "none". The UI surfaces this
	// so users can trust whether the AI "understood" the business via real
	// semantics or merely literal matching.
	RecallMethod string `json:"recallMethod,omitempty"`
	// RecallLocal is true when the semantic recall used the local, dependency-free
	// embedding fallback (no cloud embedder configured). The recall is lexical, not
	// neural, so the UI should temper trust ("本地语义召回，精度有限").
	RecallLocal bool `json:"recallLocal,omitempty"`
}

// Run is one execution attempt. Append-only: reject-and-iterate adds a new Run
// (with feedback + a spec snapshot), never overwrites, so history is kept.
type Run struct {
	ID        int64  `json:"id"`
	TaskID    string `json:"taskId"`
	Seq       int    `json:"seq"` // 1-based iteration number
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Feedback  string `json:"feedback"` // reviewer feedback that triggered this run (empty for first)
	Result    string `json:"result"`   // model output
	Status    string `json:"status"`   // "executing" | "done" | "error"
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Task is the top-level unit.
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"` // short label (from first line of input)
	Input       string `json:"input"` // the user's rough description
	Spec        Spec   `json:"spec"`  // enriched, editable spec
	Status      Status `json:"status"`
	FailedStage string `json:"failedStage,omitempty"` // where it failed, for retry
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ProjectSlug string `json:"projectSlug"` // optional knowledge-graph project for enrichment ("" = none)
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Store persists tasks and their runs (SQLite, migration 0003).
type Store interface {
	CreateTask(t Task) (Task, error)
	GetTask(id string) (Task, error)
	ListTasks() ([]Task, error) // newest updated first
	UpdateTask(t Task) error    // full update (status/spec/model/etc.)
	DeleteTask(id string) error // cascades runs

	AddRun(r Run) (Run, error)
	UpdateRun(r Run) error
	ListRuns(taskID string) ([]Run, error) // by seq asc
}
