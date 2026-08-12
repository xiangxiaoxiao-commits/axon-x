package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"axon/internal/provider"
	"axon/internal/task"
)

// Task orchestration event names emitted to the frontend. Every payload carries
// taskId so the frontend can route it to the right task card (mirrors chat:*).
const (
	EventTaskSpec   = "task:spec"   // enrichment produced a spec (render editable form)
	EventTaskDelta  = "task:delta"  // execution streaming increment
	EventTaskDone   = "task:done"   // execution finished -> review_result
	EventTaskError  = "task:error"  // enrichment or execution failed
	EventTaskStatus = "task:status" // any lifecycle transition
)

// defaultTaskConcurrency caps how many tasks execute at once; extra submissions
// queue (status queued) until a slot frees. Keeps the app from hammering vendor
// rate limits when many tasks are submitted together.
const defaultTaskConcurrency = 3

// taskSpecEvent carries the enriched spec to the frontend.
type taskSpecEvent struct {
	TaskID string    `json:"taskId"`
	Spec   task.Spec `json:"spec"`
}

// taskDeltaEvent is one streamed chunk of a run's output.
type taskDeltaEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId"`
	Delta  string `json:"delta"`
}

// taskDoneEvent signals a run completed.
type taskDoneEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId"`
}

// taskErrorEvent reports a failure (enrichment or execution).
type taskErrorEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId,omitempty"`
	Stage  string `json:"stage"`
	Error  string `json:"error"`
}

// taskStatusEvent announces a lifecycle transition.
type taskStatusEvent struct {
	TaskID string `json:"taskId"`
	Status string `json:"status"`
}

// enrichPrompt asks the model to expand a rough task into a structured, editable
// spec, explicitly filling in what the user forgot (implicit constraints,
// acceptance criteria, pitfalls). Output must be a single JSON object.
const enrichPrompt = `你是资深工程师。用户会给一段潦草的任务描述，可能还带项目背景知识。
把它补全成一份结构完整、可直接派给执行者的规格。重点补齐用户没写、但执行者必须知道的信息：
隐含约束、验收标准、容易遗漏或踩坑的点。

只输出一个 JSON 对象，不要任何解释文字、不要代码围栏。字段：
- goal: 目标，一句话说清要达成什么
- background: 背景/上下文，结合给出的项目知识
- constraints: 约束数组（技术栈、兼容性、性能、安全等）
- scope: 涉及的文件或范围数组
- acceptCriteria: 验收标准数组，可检验
- missedPoints: 用户容易遗漏的点/易踩坑点数组
- steps: 建议执行步骤数组，有序

推断出来但不确定的条目，在该条目文本开头加 "(推断)"，让用户 review 时一眼看到哪些是模型补的。`

// execSystemPrompt frames the execution call: produce the result strictly per
// the spec. MVP execution is text output only (no disk writes).
const execSystemPrompt = `你是资深工程师，按给定规格产出结果（方案/代码/答案）。严格满足所有验收标准，注意规格里列出的约束和易踩坑点。`

// --- Task lifecycle API (Wails-bound) ---

// CreateTask persists a draft task from a rough input. The title is derived from
// the first line of the input when possible. providerName/model may be empty to
// fall back to configured defaults at enrich/run time. projectSlug is optional
// ("" = no knowledge-graph enrichment).
func (a *App) CreateTask(input, providerName, model, projectSlug string) (task.Task, error) {
	if strings.TrimSpace(input) == "" {
		return task.Task{}, fmt.Errorf("task input is required")
	}
	return a.taskStore.CreateTask(task.Task{
		Title:       titleFromInput(input),
		Input:       input,
		Status:      task.StatusDraft,
		Provider:    providerName,
		Model:       model,
		ProjectSlug: projectSlug,
	})
}

// ListTasks returns all tasks, newest activity first.
func (a *App) ListTasks() ([]task.Task, error) {
	return a.taskStore.ListTasks()
}

// GetTask returns a single task by id.
func (a *App) GetTask(id string) (task.Task, error) {
	return a.taskStore.GetTask(id)
}

// ListTaskRuns returns a task's execution history, oldest iteration first.
func (a *App) ListTaskRuns(taskID string) ([]task.Run, error) {
	return a.taskStore.ListRuns(taskID)
}

// DeleteTask removes a task (and its runs). Any in-flight work is cancelled first.
func (a *App) DeleteTask(id string) error {
	a.cancelTask(id)
	return a.taskStore.DeleteTask(id)
}

// UpdateSpec saves a user-edited spec while keeping the task in review_spec, so
// the version that runs is exactly the one the user confirmed.
func (a *App) UpdateSpec(taskID string, spec task.Spec) error {
	t, err := a.taskStore.GetTask(taskID)
	if err != nil {
		return err
	}
	// Preserve enrichment provenance: it's metadata recorded at enrich time, not
	// part of the user-editable form, so a spec save must not drop it.
	spec.InjectedKnowledge = t.Spec.InjectedKnowledge
	spec.KnowledgeSources = t.Spec.KnowledgeSources
	spec.RecallMethod = t.Spec.RecallMethod
	spec.RecallLocal = t.Spec.RecallLocal
	t.Spec = spec
	t.Status = task.StatusReviewSpec
	t.FailedStage = ""
	if err := a.taskStore.UpdateTask(t); err != nil {
		return err
	}
	a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	return nil
}

// CancelTask cancels an in-flight enrichment or execution for a task, if any.
func (a *App) CancelTask(taskID string) {
	a.cancelTask(taskID)
}

// --- Enrichment ---

// EnrichTask asynchronously expands a task's rough input into a structured spec.
// It flips the task to enriching immediately, then runs the model call in a
// goroutine; on success it stores the spec, moves to review_spec and emits
// task:spec. A parse failure is not fatal: an empty spec is stored (failed_stage
// = "enrich") so the user can fill the form manually or re-enrich.
func (a *App) EnrichTask(taskID string) error {
	t, err := a.taskStore.GetTask(taskID)
	if err != nil {
		return err
	}
	t.Status = task.StatusEnriching
	t.FailedStage = ""
	if err := a.taskStore.UpdateTask(t); err != nil {
		return err
	}
	a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})

	ctx, cancel := a.registerCancel(taskID)
	go func() {
		defer a.releaseCancel(taskID)
		a.runEnrich(ctx, t)
	}()
	_ = cancel // cancellation handled via cancelTask
	return nil
}

// runEnrich performs the enrichment model call and persists the outcome.
func (a *App) runEnrich(ctx context.Context, t task.Task) {
	prov, model, err := a.providerForTask(t)
	if err != nil {
		a.failEnrich(t.ID, err)
		return
	}

	// Optional knowledge-graph background. Empty projectSlug skips recall; any
	// recall error degrades silently to no background (never fatal). We also keep
	// the matched entity names and source session titles to record on the spec, so
	// the user can see exactly what business knowledge the AI referenced.
	var background string
	var injected, sources []string
	var recallMethod string
	var recallLocal bool
	if strings.TrimSpace(t.ProjectSlug) != "" {
		// Recall query: the rough input, plus any scope paths/modules already on
		// the spec (present on a re-enrich) so code-sourced entities get recalled.
		// On a first enrich the spec has no scope yet, so this degrades to input.
		if km, mErr := a.MatchKnowledge(t.ProjectSlug, enrichQuery(t)); mErr == nil {
			background = km.Context
			injected = km.Names
			sources = km.Sources
			recallMethod = km.Method
			recallLocal = km.Local
		}
	}

	reply, err := collectReply(ctx, prov, provider.ChatRequest{
		Model:       model,
		Messages:    buildEnrichMessages(t.Input, background),
		Temperature: 0.2,
		MaxTokens:   2000,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// User cancelled: revert to draft, no error surfaced.
			a.revertEnrichCancelled(t.ID)
			return
		}
		a.failEnrich(t.ID, err)
		return
	}

	spec, ok := parseSpec(reply)
	// Record what business knowledge was injected this enrichment (empty when no
	// project was bound or the graph recalled nothing), so it persists with the
	// spec and the frontend can show provenance even after a manual re-edit.
	spec.InjectedKnowledge = injected
	spec.KnowledgeSources = sources
	spec.RecallMethod = recallMethod
	spec.RecallLocal = recallLocal

	fresh, err := a.taskStore.GetTask(t.ID)
	if err != nil {
		log.Printf("axon: reload task %s after enrich: %v", t.ID, err)
		return
	}
	fresh.Spec = spec
	fresh.Status = task.StatusReviewSpec
	if ok {
		fresh.FailedStage = ""
	} else {
		// Degraded: empty/partial spec + marker so the frontend can prompt a
		// manual fill or re-enrich, without auto-retrying.
		fresh.FailedStage = "enrich"
	}
	if err := a.taskStore.UpdateTask(fresh); err != nil {
		log.Printf("axon: persist enriched task %s: %v", t.ID, err)
		return
	}
	a.emit(EventTaskSpec, taskSpecEvent{TaskID: t.ID, Spec: spec})
	a.emit(EventTaskStatus, taskStatusEvent{TaskID: t.ID, Status: string(fresh.Status)})
}

// enrichQuery builds the knowledge-recall query for enrichment: the rough input
// plus, when a spec already carries a scope (re-enrich), its files/modules and
// their base names — so code-sourced file/function entities are recalled just
// like buildKnowledgeQuery does for commit diffs. Falls back to input alone.
func enrichQuery(t task.Task) string {
	scope := t.Spec.Scope
	if len(scope) == 0 {
		return t.Input
	}
	var b strings.Builder
	b.WriteString(t.Input)
	for _, s := range scope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(s)
		if strings.Contains(s, "/") {
			base := s[strings.LastIndex(s, "/")+1:]
			b.WriteString(" ")
			b.WriteString(strings.TrimSuffix(base, filepath.Ext(base)))
		}
	}
	return b.String()
}

// failEnrich marks a task failed at the enrich stage and emits task:error.
func (a *App) failEnrich(taskID string, cause error) {
	if t, err := a.taskStore.GetTask(taskID); err == nil {
		t.Status = task.StatusFailed
		t.FailedStage = "enrich"
		_ = a.taskStore.UpdateTask(t)
	}
	log.Printf("axon: enrich task %s failed: %v", taskID, cause)
	a.emit(EventTaskError, taskErrorEvent{TaskID: taskID, Stage: "enrich", Error: cause.Error()})
}

// revertEnrichCancelled returns a cancelled enrichment to draft.
func (a *App) revertEnrichCancelled(taskID string) {
	if t, err := a.taskStore.GetTask(taskID); err == nil {
		t.Status = task.StatusDraft
		_ = a.taskStore.UpdateTask(t)
		a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}
}

// --- Execution (parallel, semaphore-limited) ---

// RunTask confirms the current spec and submits the task for execution. If no
// concurrency slot is free the task shows as queued; once a slot is acquired it
// flips to executing and streams the result. First run carries no feedback.
func (a *App) RunTask(taskID string) error {
	return a.submitRun(taskID, "")
}

// ReviewTask records the reviewer's decision. accept -> accepted (terminal).
// reject -> a new run carrying the feedback plus the previous result, re-running
// the task (executing/queued), keeping the iteration history.
func (a *App) ReviewTask(taskID, decision, feedback string) error {
	switch decision {
	case "accept":
		t, err := a.taskStore.GetTask(taskID)
		if err != nil {
			return err
		}
		t.Status = task.StatusAccepted
		t.FailedStage = ""
		if err := a.taskStore.UpdateTask(t); err != nil {
			return err
		}
		a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})

		// Writeback: fold the business knowledge learned from this accepted task
		// into the project's knowledge graph, so it grows with use. Async and
		// best-effort — acceptance already succeeded and must not depend on it.
		if strings.TrimSpace(t.ProjectSlug) != "" {
			result := latestRunResult(a.taskStore, taskID)
			go a.writeBackTaskKnowledge(t, result)
		}
		return nil
	case "reject":
		return a.submitRun(taskID, feedback)
	default:
		return fmt.Errorf("unknown review decision %q (want accept|reject)", decision)
	}
}

// submitRun creates a new run record and dispatches execution through the
// concurrency semaphore. It returns immediately; the goroutine blocks on the
// semaphore (forming the queue) then streams. feedback is non-empty on a
// reject-and-iterate, in which case the previous run's result is fed back too.
func (a *App) submitRun(taskID, feedback string) error {
	t, err := a.taskStore.GetTask(taskID)
	if err != nil {
		return err
	}
	_, model, err := a.providerForTask(t)
	if err != nil {
		return err
	}

	// Carry the last result into an iterate so the model knows what to improve.
	var prevResult string
	if strings.TrimSpace(feedback) != "" {
		if runs, rErr := a.taskStore.ListRuns(taskID); rErr == nil && len(runs) > 0 {
			prevResult = runs[len(runs)-1].Result
		}
	}

	run, err := a.taskStore.AddRun(task.Run{
		TaskID:   taskID,
		Provider: t.Provider,
		Model:    model,
		Feedback: feedback,
		Status:   "executing",
	})
	if err != nil {
		return fmt.Errorf("start run: %w", err)
	}

	t.Status = task.StatusQueued
	t.FailedStage = ""
	if err := a.taskStore.UpdateTask(t); err != nil {
		return err
	}
	a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})

	ctx, _ := a.registerCancel(taskID)
	spec := t.Spec
	go func() {
		defer a.releaseCancel(taskID)

		// Block for a concurrency slot; cancellation while queued aborts cleanly.
		select {
		case a.taskSem <- struct{}{}:
		case <-ctx.Done():
			a.abortQueuedRun(taskID, run.ID)
			return
		}
		defer func() { <-a.taskSem }()

		if ctx.Err() != nil {
			a.abortQueuedRun(taskID, run.ID)
			return
		}
		a.runExec(ctx, taskID, run, spec, feedback, prevResult)
	}()
	return nil
}

// runExec streams one execution, persisting the accumulated output even if
// cancelled mid-stream, then transitions the task by outcome.
func (a *App) runExec(ctx context.Context, taskID string, run task.Run, spec task.Spec, feedback, prevResult string) {
	if t, err := a.taskStore.GetTask(taskID); err == nil {
		t.Status = task.StatusExecuting
		_ = a.taskStore.UpdateTask(t)
		a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}

	prov, model, err := a.providerForTaskID(taskID)
	if err != nil {
		a.failRun(taskID, run, "", err)
		return
	}

	chunks, errs := prov.Chat(ctx, provider.ChatRequest{
		Model:       model,
		Messages:    buildExecMessages(spec, feedback, prevResult),
		Temperature: 0.3,
		MaxTokens:   4000,
	})

	var b strings.Builder
	for chunk := range chunks {
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
			a.emit(EventTaskDelta, taskDeltaEvent{TaskID: taskID, RunID: run.ID, Delta: chunk.Delta})
		}
	}
	var streamErr error
	select {
	case streamErr = <-errs:
	default:
	}

	result := b.String()
	switch {
	case streamErr != nil && errors.Is(streamErr, context.Canceled):
		// User cancelled: keep partial output on the run, revert task so it can
		// be resubmitted (no half-finished executing state left behind).
		run.Result = result
		run.Status = "error"
		run.Error = "cancelled"
		_ = a.taskStore.UpdateRun(run)
		if t, err := a.taskStore.GetTask(taskID); err == nil {
			t.Status = task.StatusReviewSpec
			_ = a.taskStore.UpdateTask(t)
			a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
		}
	case streamErr != nil:
		a.failRun(taskID, run, result, streamErr)
	default:
		run.Result = result
		run.Status = "done"
		run.Error = ""
		_ = a.taskStore.UpdateRun(run)
		if t, err := a.taskStore.GetTask(taskID); err == nil {
			t.Status = task.StatusReviewResult
			t.FailedStage = ""
			_ = a.taskStore.UpdateTask(t)
			a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
		}
		a.emit(EventTaskDone, taskDoneEvent{TaskID: taskID, RunID: run.ID})
	}
}

// failRun marks the run and its task failed (stage execute) and emits task:error.
func (a *App) failRun(taskID string, run task.Run, partial string, cause error) {
	run.Result = partial
	run.Status = "error"
	run.Error = cause.Error()
	_ = a.taskStore.UpdateRun(run)
	if t, err := a.taskStore.GetTask(taskID); err == nil {
		t.Status = task.StatusFailed
		t.FailedStage = "execute"
		_ = a.taskStore.UpdateTask(t)
	}
	log.Printf("axon: run %d for task %s failed: %v", run.ID, taskID, cause)
	a.emit(EventTaskError, taskErrorEvent{TaskID: taskID, RunID: run.ID, Stage: "execute", Error: cause.Error()})
}

// abortQueuedRun handles a task cancelled while still queued (never streamed).
func (a *App) abortQueuedRun(taskID string, runID int64) {
	if t, err := a.taskStore.GetTask(taskID); err == nil {
		t.Status = task.StatusReviewSpec
		_ = a.taskStore.UpdateTask(t)
		a.emit(EventTaskStatus, taskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}
	runs, err := a.taskStore.ListRuns(taskID)
	if err != nil {
		return
	}
	for _, r := range runs {
		if r.ID == runID {
			r.Status = "error"
			r.Error = "cancelled"
			_ = a.taskStore.UpdateRun(r)
			break
		}
	}
}

// --- Cancellation registry (task-scoped, mirrors app.cancels) ---

// registerCancel creates a cancellable context for a task and records its cancel
// func, superseding any in-flight one for the same task.
func (a *App) registerCancel(taskID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(a.ctx)
	a.taskMu.Lock()
	if prev, ok := a.taskCancels[taskID]; ok {
		prev()
	}
	a.taskCancels[taskID] = cancel
	a.taskMu.Unlock()
	return ctx, cancel
}

// releaseCancel drops a task's cancel func once its goroutine exits.
func (a *App) releaseCancel(taskID string) {
	a.taskMu.Lock()
	delete(a.taskCancels, taskID)
	a.taskMu.Unlock()
}

// cancelTask cancels a task's in-flight work, if any.
func (a *App) cancelTask(taskID string) {
	a.taskMu.Lock()
	cancel := a.taskCancels[taskID]
	a.taskMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// --- Provider / model resolution ---

// providerForTask builds a live provider for a task, resolving provider/model
// from the task, falling back to the configured defaults when empty.
func (a *App) providerForTask(t task.Task) (provider.Provider, string, error) {
	cfg := a.cfg.Get()
	name := t.Provider
	if name == "" {
		name = cfg.DefaultProvider
	}
	model := t.Model
	if model == "" {
		model = cfg.DefaultModel
	}
	pc, ok := a.cfg.Provider(name)
	if !ok {
		return nil, "", fmt.Errorf("provider %q not configured", name)
	}
	prov, err := a.newProvider(pc)
	if err != nil {
		return nil, "", err
	}
	return prov, model, nil
}

// providerForTaskID is providerForTask keyed by id (reloads the task).
func (a *App) providerForTaskID(taskID string) (provider.Provider, string, error) {
	t, err := a.taskStore.GetTask(taskID)
	if err != nil {
		return nil, "", err
	}
	return a.providerForTask(t)
}

// --- Prompt building ---

// buildEnrichMessages assembles the enrichment prompt: system rules + optional
// project background + the rough task input.
func buildEnrichMessages(input, background string) []provider.ChatMessage {
	var u strings.Builder
	if strings.TrimSpace(background) != "" {
		u.WriteString(background)
		u.WriteString("\n\n---\n\n")
	}
	u.WriteString("任务描述:\n")
	u.WriteString(input)
	return []provider.ChatMessage{
		{Role: provider.RoleSystem, Content: enrichPrompt},
		{Role: provider.RoleUser, Content: u.String()},
	}
}

// buildExecMessages assembles the execution prompt from the spec, plus reviewer
// feedback and the previous result when this is a reject-and-iterate run.
func buildExecMessages(spec task.Spec, feedback, prevResult string) []provider.ChatMessage {
	var u strings.Builder
	u.WriteString(renderSpec(spec))
	if strings.TrimSpace(feedback) != "" {
		if strings.TrimSpace(prevResult) != "" {
			u.WriteString("\n\n---\n上一版产出:\n")
			u.WriteString(prevResult)
		}
		u.WriteString("\n\n---\n上一版被打回，请针对以下反馈重做:\n")
		u.WriteString(feedback)
	}
	return []provider.ChatMessage{
		{Role: provider.RoleSystem, Content: execSystemPrompt},
		{Role: provider.RoleUser, Content: u.String()},
	}
}

// renderSpec flattens a Spec into a readable prompt block.
func renderSpec(s task.Spec) string {
	var b strings.Builder
	if s.Goal != "" {
		fmt.Fprintf(&b, "目标: %s\n", s.Goal)
	}
	if s.Background != "" {
		fmt.Fprintf(&b, "\n背景:\n%s\n", s.Background)
	}
	writeList(&b, "约束", s.Constraints)
	writeList(&b, "涉及范围", s.Scope)
	writeList(&b, "验收标准", s.AcceptCriteria)
	writeList(&b, "易遗漏点", s.MissedPoints)
	writeList(&b, "建议步骤", s.Steps)
	return b.String()
}

// writeList appends a titled bullet list when items exist.
func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

// --- Parsing ---

// parseSpec extracts the JSON object from a model reply (tolerating code fences
// or surrounding prose) and unmarshals it into a Spec. Returns ok=false when the
// reply can't be parsed, so the caller can degrade to an empty, user-fillable
// form instead of retrying.
func parseSpec(reply string) (task.Spec, bool) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var spec task.Spec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return task.Spec{}, false
	}
	return spec, true
}

// titleFromInput derives a short title from the first non-empty line of input.
func titleFromInput(input string) string {
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 60 {
			return line[:60] + "..."
		}
		return line
	}
	return "Untitled task"
}

// --- Crash recovery ---

// recoverStaleTasks resets tasks left mid-execution by a crash/quit. Goroutines
// don't survive a process restart, so an executing/queued/enriching task is
// stale: executing/queued -> failed(execute) (retryable), enriching -> draft.
func (a *App) recoverStaleTasks() {
	tasks, err := a.taskStore.ListTasks()
	if err != nil {
		log.Printf("axon: recover stale tasks: %v", err)
		return
	}
	for _, t := range tasks {
		switch t.Status {
		case task.StatusExecuting, task.StatusQueued:
			t.Status = task.StatusFailed
			t.FailedStage = "execute"
			if err := a.taskStore.UpdateTask(t); err != nil {
				log.Printf("axon: recover task %s: %v", t.ID, err)
			}
		case task.StatusEnriching:
			t.Status = task.StatusDraft
			if err := a.taskStore.UpdateTask(t); err != nil {
				log.Printf("axon: recover task %s: %v", t.ID, err)
			}
		}
	}
}
