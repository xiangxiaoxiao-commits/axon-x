package orchestr

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"axon/internal/provider"
	"axon/internal/task"
)

// Event names emitted to the host. Every payload carries the task id so a host
// running several tasks at once can route each event to the right card.
const (
	EventTaskSpec   = "task:spec"   // enrichment produced a spec (render editable form)
	EventTaskDelta  = "task:delta"  // execution streaming increment
	EventTaskDone   = "task:done"   // execution finished -> review result
	EventTaskError  = "task:error"  // enrichment or execution failed
	EventTaskStatus = "task:status" // any lifecycle transition
)

// DefaultConcurrency caps how many tasks execute at once; extra submissions
// queue (status queued) until a slot frees. Keeps the host from hammering
// vendor rate limits when many tasks are submitted together.
const DefaultConcurrency = 3

// TaskSpecEvent carries the enriched spec to the host.
type TaskSpecEvent struct {
	TaskID string    `json:"taskId"`
	Spec   task.Spec `json:"spec"`
}

// TaskDeltaEvent is one streamed chunk of a run's output.
type TaskDeltaEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId"`
	Delta  string `json:"delta"`
}

// TaskDoneEvent signals a run completed.
type TaskDoneEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId"`
}

// TaskErrorEvent reports a failure (enrichment or execution).
type TaskErrorEvent struct {
	TaskID string `json:"taskId"`
	RunID  int64  `json:"runId,omitempty"`
	Stage  string `json:"stage"`
	Error  string `json:"error"`
}

// TaskStatusEvent announces a lifecycle transition.
type TaskStatusEvent struct {
	TaskID string `json:"taskId"`
	Status string `json:"status"`
}

// Settings tunes the two model calls and the execution concurrency.
type Settings struct {
	Concurrency       int     // 0 -> DefaultConcurrency
	EnrichTemperature float64 // 0 -> 0.2
	EnrichMaxTokens   int     // 0 -> 2000
	ExecTemperature   float64 // 0 -> 0.3
	ExecMaxTokens     int     // 0 -> 4000
}

func (s Settings) withDefaults() Settings {
	if s.Concurrency <= 0 {
		s.Concurrency = DefaultConcurrency
	}
	if s.EnrichTemperature == 0 {
		s.EnrichTemperature = 0.2
	}
	if s.EnrichMaxTokens == 0 {
		s.EnrichMaxTokens = 2000
	}
	if s.ExecTemperature == 0 {
		s.ExecTemperature = 0.3
	}
	if s.ExecMaxTokens == 0 {
		s.ExecMaxTokens = 4000
	}
	return s
}

// Deps is everything an Engine needs from its host.
type Deps struct {
	Ctx       context.Context // root for background work; nil -> Background
	Store     task.Store
	Provider  ProviderFactory
	Recall    Recaller    // optional: nil disables knowledge injection
	Writeback Writebacker // optional: nil disables acceptance writeback
	Emit      Emitter     // optional: nil discards events
	Settings  Settings
}

// Engine runs the orchestration loop. It is safe for concurrent use: tasks
// execute in their own goroutines, capped by a semaphore, and each task's
// in-flight work can be cancelled independently.
type Engine struct {
	ctx       context.Context
	store     task.Store
	resolve   ProviderFactory
	recall    Recaller
	writeback Writebacker
	emit      Emitter
	set       Settings

	sem     chan struct{}
	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	// bg tracks fire-and-forget work (knowledge writeback) so a short-lived host
	// such as the CLI can wait for it before exiting. A GUI never needs this, but
	// without it an accepted task's writeback dies with the process.
	bg sync.WaitGroup
}

// New builds an Engine from its host dependencies.
func New(d Deps) (*Engine, error) {
	if d.Store == nil {
		return nil, fmt.Errorf("orchestr: store is required")
	}
	if d.Provider == nil {
		return nil, fmt.Errorf("orchestr: provider factory is required")
	}
	ctx := d.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	emit := d.Emit
	if emit == nil {
		emit = nopEmitter
	}
	set := d.Settings.withDefaults()
	return &Engine{
		ctx:       ctx,
		store:     d.Store,
		resolve:   d.Provider,
		recall:    d.Recall,
		writeback: d.Writeback,
		emit:      emit,
		set:       set,
		sem:       make(chan struct{}, set.Concurrency),
		cancels:   make(map[string]context.CancelFunc),
	}, nil
}

// --- Task lifecycle ---

// CreateTask persists a draft task from a rough input. The title is derived from
// the first line of the input when possible. providerName/model may be empty to
// fall back to the host's configured defaults at enrich/run time. projectSlug is
// optional ("" = no knowledge-graph enrichment).
func (e *Engine) CreateTask(input, providerName, model, projectSlug string) (task.Task, error) {
	if strings.TrimSpace(input) == "" {
		return task.Task{}, fmt.Errorf("task input is required")
	}
	return e.store.CreateTask(task.Task{
		Title:       TitleFromInput(input),
		Input:       input,
		Status:      task.StatusDraft,
		Provider:    providerName,
		Model:       model,
		ProjectSlug: projectSlug,
	})
}

// ListTasks returns all tasks, newest activity first.
func (e *Engine) ListTasks() ([]task.Task, error) { return e.store.ListTasks() }

// GetTask returns a single task by id.
func (e *Engine) GetTask(id string) (task.Task, error) { return e.store.GetTask(id) }

// ListRuns returns a task's execution history, oldest iteration first.
func (e *Engine) ListRuns(taskID string) ([]task.Run, error) { return e.store.ListRuns(taskID) }

// DeleteTask removes a task (and its runs). Any in-flight work is cancelled first.
func (e *Engine) DeleteTask(id string) error {
	e.CancelTask(id)
	return e.store.DeleteTask(id)
}

// UpdateSpec saves a user-edited spec while keeping the task in review_spec, so
// the version that runs is exactly the one the user confirmed.
func (e *Engine) UpdateSpec(taskID string, spec task.Spec) error {
	t, err := e.store.GetTask(taskID)
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
	if err := e.store.UpdateTask(t); err != nil {
		return err
	}
	e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	return nil
}

// CancelTask cancels an in-flight enrichment or execution for a task, if any.
func (e *Engine) CancelTask(taskID string) {
	e.mu.Lock()
	cancel := e.cancels[taskID]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// --- Enrichment ---

// EnrichTask asynchronously expands a task's rough input into a structured spec.
// It flips the task to enriching immediately, then runs the model call in a
// goroutine; on success it stores the spec, moves to review_spec and emits
// task:spec. A parse failure is not fatal: an empty spec is stored
// (failed_stage = "enrich") so the user can fill the form manually or re-enrich.
func (e *Engine) EnrichTask(taskID string) error {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return err
	}
	t.Status = task.StatusEnriching
	t.FailedStage = ""
	if err := e.store.UpdateTask(t); err != nil {
		return err
	}
	e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})

	ctx, _ := e.registerCancel(taskID)
	go func() {
		defer e.releaseCancel(taskID)
		e.runEnrich(ctx, t)
	}()
	return nil
}

// runEnrich performs the enrichment model call and persists the outcome.
func (e *Engine) runEnrich(ctx context.Context, t task.Task) {
	prov, model, err := e.providerFor(t)
	if err != nil {
		e.failEnrich(t.ID, err)
		return
	}

	// Optional knowledge-graph background. Empty projectSlug skips recall; any
	// recall error degrades silently to no background (never fatal). We also keep
	// the matched entity names and source titles on the spec so the host can show
	// exactly what business knowledge the model referenced.
	var background string
	var injected, sources []string
	var recallMethod string
	var recallLocal bool
	if strings.TrimSpace(t.ProjectSlug) != "" && e.recall != nil {
		if km, mErr := e.recall.Recall(ctx, t.ProjectSlug, enrichQuery(t)); mErr == nil {
			background = km.Context
			injected = km.Names
			sources = km.Sources
			recallMethod = km.Method
			recallLocal = km.Local
		}
	}

	reply, err := collectReply(ctx, prov, provider.ChatRequest{
		Model:       model,
		Messages:    BuildEnrichMessages(t.Input, background),
		Temperature: e.set.EnrichTemperature,
		MaxTokens:   e.set.EnrichMaxTokens,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			e.revertEnrichCancelled(t.ID)
			return
		}
		e.failEnrich(t.ID, err)
		return
	}

	spec, ok := ParseSpec(reply)
	spec.InjectedKnowledge = injected
	spec.KnowledgeSources = sources
	spec.RecallMethod = recallMethod
	spec.RecallLocal = recallLocal

	fresh, err := e.store.GetTask(t.ID)
	if err != nil {
		log.Printf("orchestr: reload task %s after enrich: %v", t.ID, err)
		return
	}
	fresh.Spec = spec
	fresh.Status = task.StatusReviewSpec
	if ok {
		fresh.FailedStage = ""
	} else {
		// Degraded: empty/partial spec + marker so the host can prompt a manual
		// fill or re-enrich, without auto-retrying.
		fresh.FailedStage = "enrich"
	}
	if err := e.store.UpdateTask(fresh); err != nil {
		log.Printf("orchestr: persist enriched task %s: %v", t.ID, err)
		return
	}
	e.emit(EventTaskSpec, TaskSpecEvent{TaskID: t.ID, Spec: spec})
	e.emit(EventTaskStatus, TaskStatusEvent{TaskID: t.ID, Status: string(fresh.Status)})
}

// failEnrich marks a task failed at the enrich stage and emits task:error.
func (e *Engine) failEnrich(taskID string, cause error) {
	if t, err := e.store.GetTask(taskID); err == nil {
		t.Status = task.StatusFailed
		t.FailedStage = "enrich"
		_ = e.store.UpdateTask(t)
	}
	log.Printf("orchestr: enrich task %s failed: %v", taskID, cause)
	e.emit(EventTaskError, TaskErrorEvent{TaskID: taskID, Stage: "enrich", Error: cause.Error()})
}

// revertEnrichCancelled returns a cancelled enrichment to draft.
func (e *Engine) revertEnrichCancelled(taskID string) {
	if t, err := e.store.GetTask(taskID); err == nil {
		t.Status = task.StatusDraft
		_ = e.store.UpdateTask(t)
		e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}
}

// --- Execution (parallel, semaphore-limited) ---

// RunTask confirms the current spec and submits the task for execution. If no
// concurrency slot is free the task shows as queued; once a slot is acquired it
// flips to executing and streams the result. First run carries no feedback.
func (e *Engine) RunTask(taskID string) error { return e.submitRun(taskID, "") }

// ReviewTask records the reviewer's decision. accept -> accepted (terminal).
// reject -> a new run carrying the feedback plus the previous result, re-running
// the task, keeping the iteration history.
func (e *Engine) ReviewTask(taskID, decision, feedback string) error {
	switch decision {
	case "accept":
		t, err := e.store.GetTask(taskID)
		if err != nil {
			return err
		}
		t.Status = task.StatusAccepted
		t.FailedStage = ""
		if err := e.store.UpdateTask(t); err != nil {
			return err
		}
		e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})

		// Writeback: fold the business knowledge learned from this accepted task
		// into the project's knowledge graph, so it grows with use. Async and
		// best-effort — acceptance already succeeded and must not depend on it.
		if strings.TrimSpace(t.ProjectSlug) != "" && e.writeback != nil {
			result := latestRunResult(e.store, taskID)
			e.bg.Add(1)
			go func() {
				defer e.bg.Done()
				if err := e.writeback.WriteBack(e.ctx, t, result); err != nil {
					log.Printf("orchestr: writeback task %s: %v", t.ID, err)
				}
			}()
		}
		return nil
	case "reject":
		return e.submitRun(taskID, feedback)
	default:
		return fmt.Errorf("unknown review decision %q (want accept|reject)", decision)
	}
}

// submitRun creates a new run record and dispatches execution through the
// concurrency semaphore. It returns immediately; the goroutine blocks on the
// semaphore (forming the queue) then streams. feedback is non-empty on a
// reject-and-iterate, in which case the previous run's result is fed back too.
func (e *Engine) submitRun(taskID, feedback string) error {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return err
	}
	_, model, err := e.resolve(e.ctx, t.Provider, t.Model)
	if err != nil {
		return err
	}

	// Carry the last result into an iterate so the model knows what to improve.
	var prevResult string
	if strings.TrimSpace(feedback) != "" {
		if runs, rErr := e.store.ListRuns(taskID); rErr == nil && len(runs) > 0 {
			prevResult = runs[len(runs)-1].Result
		}
	}

	run, err := e.store.AddRun(task.Run{
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
	if err := e.store.UpdateTask(t); err != nil {
		return err
	}
	e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})

	ctx, _ := e.registerCancel(taskID)
	spec := t.Spec
	go func() {
		defer e.releaseCancel(taskID)

		// Block for a concurrency slot; cancellation while queued aborts cleanly.
		select {
		case e.sem <- struct{}{}:
		case <-ctx.Done():
			e.abortQueuedRun(taskID, run.ID)
			return
		}
		defer func() { <-e.sem }()

		if ctx.Err() != nil {
			e.abortQueuedRun(taskID, run.ID)
			return
		}
		e.runExec(ctx, taskID, run, spec, feedback, prevResult)
	}()
	return nil
}

// runExec streams one execution, persisting the accumulated output even if
// cancelled mid-stream, then transitions the task by outcome.
func (e *Engine) runExec(ctx context.Context, taskID string, run task.Run, spec task.Spec, feedback, prevResult string) {
	if t, err := e.store.GetTask(taskID); err == nil {
		t.Status = task.StatusExecuting
		_ = e.store.UpdateTask(t)
		e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}

	prov, model, err := e.providerForID(taskID)
	if err != nil {
		e.failRun(taskID, run, "", err)
		return
	}

	chunks, errs := prov.Chat(ctx, provider.ChatRequest{
		Model:       model,
		Messages:    BuildExecMessages(spec, feedback, prevResult),
		Temperature: e.set.ExecTemperature,
		MaxTokens:   e.set.ExecMaxTokens,
	})

	var b strings.Builder
	for chunk := range chunks {
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
			e.emit(EventTaskDelta, TaskDeltaEvent{TaskID: taskID, RunID: run.ID, Delta: chunk.Delta})
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
		_ = e.store.UpdateRun(run)
		if t, err := e.store.GetTask(taskID); err == nil {
			t.Status = task.StatusReviewSpec
			_ = e.store.UpdateTask(t)
			e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
		}
	case streamErr != nil:
		e.failRun(taskID, run, result, streamErr)
	default:
		run.Result = result
		run.Status = "done"
		run.Error = ""
		_ = e.store.UpdateRun(run)
		if t, err := e.store.GetTask(taskID); err == nil {
			t.Status = task.StatusReviewResult
			t.FailedStage = ""
			_ = e.store.UpdateTask(t)
			e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
		}
		e.emit(EventTaskDone, TaskDoneEvent{TaskID: taskID, RunID: run.ID})
	}
}

// failRun marks the run and its task failed (stage execute) and emits task:error.
func (e *Engine) failRun(taskID string, run task.Run, partial string, cause error) {
	run.Result = partial
	run.Status = "error"
	run.Error = cause.Error()
	_ = e.store.UpdateRun(run)
	if t, err := e.store.GetTask(taskID); err == nil {
		t.Status = task.StatusFailed
		t.FailedStage = "execute"
		_ = e.store.UpdateTask(t)
	}
	log.Printf("orchestr: run %d for task %s failed: %v", run.ID, taskID, cause)
	e.emit(EventTaskError, TaskErrorEvent{TaskID: taskID, RunID: run.ID, Stage: "execute", Error: cause.Error()})
}

// abortQueuedRun handles a task cancelled while still queued (never streamed).
func (e *Engine) abortQueuedRun(taskID string, runID int64) {
	if t, err := e.store.GetTask(taskID); err == nil {
		t.Status = task.StatusReviewSpec
		_ = e.store.UpdateTask(t)
		e.emit(EventTaskStatus, TaskStatusEvent{TaskID: taskID, Status: string(t.Status)})
	}
	runs, err := e.store.ListRuns(taskID)
	if err != nil {
		return
	}
	for _, r := range runs {
		if r.ID == runID {
			r.Status = "error"
			r.Error = "cancelled"
			_ = e.store.UpdateRun(r)
			break
		}
	}
}

// --- Cancellation registry ---

// registerCancel creates a cancellable context for a task and records its cancel
// func, superseding any in-flight one for the same task.
func (e *Engine) registerCancel(taskID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(e.ctx)
	e.mu.Lock()
	if prev, ok := e.cancels[taskID]; ok {
		prev()
	}
	e.cancels[taskID] = cancel
	e.mu.Unlock()
	return ctx, cancel
}

// releaseCancel drops a task's cancel func once its goroutine exits.
func (e *Engine) releaseCancel(taskID string) {
	e.mu.Lock()
	delete(e.cancels, taskID)
	e.mu.Unlock()
}

// --- Provider resolution ---

func (e *Engine) providerFor(t task.Task) (provider.Provider, string, error) {
	return e.resolve(e.ctx, t.Provider, t.Model)
}

func (e *Engine) providerForID(taskID string) (provider.Provider, string, error) {
	t, err := e.store.GetTask(taskID)
	if err != nil {
		return nil, "", err
	}
	return e.providerFor(t)
}

// --- Crash recovery ---

// RecoverStaleTasks resets tasks left mid-execution by a crash/quit. Goroutines
// don't survive a process restart, so an executing/queued/enriching task is
// stale: executing/queued -> failed(execute) (retryable), enriching -> draft.
// Hosts should call this once at startup.
func (e *Engine) RecoverStaleTasks() {
	tasks, err := e.store.ListTasks()
	if err != nil {
		log.Printf("orchestr: recover stale tasks: %v", err)
		return
	}
	for _, t := range tasks {
		switch t.Status {
		case task.StatusExecuting, task.StatusQueued:
			t.Status = task.StatusFailed
			t.FailedStage = "execute"
			if err := e.store.UpdateTask(t); err != nil {
				log.Printf("orchestr: recover task %s: %v", t.ID, err)
			}
		case task.StatusEnriching:
			t.Status = task.StatusDraft
			if err := e.store.UpdateTask(t); err != nil {
				log.Printf("orchestr: recover task %s: %v", t.ID, err)
			}
		}
	}
}

// latestRunResult returns the most recent run's result for a task, or "" on any
// error or when there are no runs.
func latestRunResult(store task.Store, taskID string) string {
	runs, err := store.ListRuns(taskID)
	if err != nil || len(runs) == 0 {
		return ""
	}
	return runs[len(runs)-1].Result
}

// WaitBackground blocks until fire-and-forget work (knowledge writeback) drains,
// or the timeout elapses. A long-lived host can ignore it; a short-lived one such
// as the CLI must call it before exiting, otherwise an accepted task's writeback
// never lands.
func (e *Engine) WaitBackground(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		e.bg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}
