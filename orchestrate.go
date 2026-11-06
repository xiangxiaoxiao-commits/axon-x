package main

import (
	"context"
	"fmt"

	"axon/internal/orchestr"
	"axon/internal/provider"
	"axon/internal/task"
)

// The orchestration loop itself lives in internal/orchestr so the CLI and MCP
// entry points can drive the exact same code path without Wails. This file is
// only the desktop app's adapter: it forwards the Wails-bound API to the engine
// and supplies the three host-specific dependencies (provider resolution,
// knowledge recall, acceptance writeback).
//
// Behaviour is unchanged from the pre-extraction implementation: the engine is a
// lift-and-shift of the previous logic with a.* replaced by injected deps.

// --- Task lifecycle API (Wails-bound) ---

// CreateTask persists a draft task from a rough input.
func (a *App) CreateTask(input, providerName, model, projectSlug string) (task.Task, error) {
	return a.orch.CreateTask(input, providerName, model, projectSlug)
}

// ListTasks returns all tasks, newest activity first.
func (a *App) ListTasks() ([]task.Task, error) { return a.orch.ListTasks() }

// GetTask returns a single task by id.
func (a *App) GetTask(id string) (task.Task, error) { return a.orch.GetTask(id) }

// ListTaskRuns returns a task's execution history, oldest iteration first.
func (a *App) ListTaskRuns(taskID string) ([]task.Run, error) { return a.orch.ListRuns(taskID) }

// DeleteTask removes a task (and its runs). Any in-flight work is cancelled first.
func (a *App) DeleteTask(id string) error { return a.orch.DeleteTask(id) }

// UpdateSpec saves a user-edited spec while keeping the task in review_spec, so
// the version that runs is exactly the one the user confirmed.
func (a *App) UpdateSpec(taskID string, spec task.Spec) error {
	return a.orch.UpdateSpec(taskID, spec)
}

// CancelTask cancels an in-flight enrichment or execution for a task, if any.
func (a *App) CancelTask(taskID string) { a.orch.CancelTask(taskID) }

// EnrichTask asynchronously expands a task's rough input into a structured spec.
func (a *App) EnrichTask(taskID string) error { return a.orch.EnrichTask(taskID) }

// RunTask confirms the current spec and submits the task for execution.
func (a *App) RunTask(taskID string) error { return a.orch.RunTask(taskID) }

// ReviewTask records the reviewer's decision (accept, or reject with feedback).
func (a *App) ReviewTask(taskID, decision, feedback string) error {
	return a.orch.ReviewTask(taskID, decision, feedback)
}

// --- Host adapters ---

// buildProvider resolves a live provider and the model id for a task, falling
// back to the configured defaults when the task left either empty.
func (a *App) buildProvider(_ context.Context, providerName, model string) (provider.Provider, string, error) {
	cfg := a.cfg.Get()
	name := providerName
	if name == "" {
		name = cfg.DefaultProvider
	}
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

// appRecaller adapts App.MatchKnowledge to orchestr.Recaller.
type appRecaller struct{ app *App }

func (r appRecaller) Recall(_ context.Context, projectSlug, query string) (orchestr.Knowledge, error) {
	km, err := r.app.MatchKnowledge(projectSlug, query)
	if err != nil {
		return orchestr.Knowledge{}, err
	}
	return orchestr.Knowledge{
		Context: km.Context,
		Names:   km.Names,
		Sources: km.Sources,
		Method:  km.Method,
		Local:   km.Local,
	}, nil
}

// appWritebacker adapts App.writeBackTaskKnowledge to orchestr.Writebacker.
type appWritebacker struct{ app *App }

func (w appWritebacker) WriteBack(_ context.Context, t task.Task, result string) error {
	w.app.writeBackTaskKnowledge(t, result)
	return nil
}
