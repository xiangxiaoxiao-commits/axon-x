// TaskStore is the SQLite-backed implementation of task.Store (migration 0003).
// It reuses the same *sql.DB opened by the db package (WAL, foreign keys on) as
// the conversation Store, but is a separate type: tasks are mutable, stateful
// entities with a lifecycle, unlike the append-only conversation archive.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"axon/internal/task"
)

// TaskStore persists tasks and their execution runs.
type TaskStore struct {
	db *sql.DB
}

// compile-time check that *TaskStore satisfies the interface.
var _ task.Store = (*TaskStore)(nil)

// NewTaskStore wraps an already-opened database in a TaskStore.
func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
}

// bg returns a background context for the interface methods, which are
// context-free by design (App owns cancellation via its own contexts).
func bg() context.Context { return context.Background() }

// CreateTask inserts a new task, generating a UUID v4 when the id is empty and
// stamping created_at/updated_at with the current unix millis. The spec is
// stored as JSON in the spec column.
func (s *TaskStore) CreateTask(t task.Task) (task.Task, error) {
	if t.ID == "" {
		t.ID = uuid.NewString()
	}
	if t.Status == "" {
		t.Status = task.StatusDraft
	}
	now := time.Now().UnixMilli()
	t.CreatedAt = now
	t.UpdatedAt = now

	specJSON, err := json.Marshal(t.Spec)
	if err != nil {
		return task.Task{}, fmt.Errorf("marshal spec: %w", err)
	}

	_, err = s.db.ExecContext(bg(),
		`INSERT INTO tasks
		 (id, title, input, spec, status, failed_stage, provider, model, project_slug, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.Title, t.Input, string(specJSON), string(t.Status), t.FailedStage,
		t.Provider, t.Model, t.ProjectSlug, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return task.Task{}, fmt.Errorf("insert task: %w", err)
	}

	return t, nil
}

// GetTask returns a single task by id. A missing row surfaces as an error
// wrapping sql.ErrNoRows.
func (s *TaskStore) GetTask(id string) (task.Task, error) {
	row := s.db.QueryRowContext(bg(),
		`SELECT id, title, input, spec, status, failed_stage, provider, model, project_slug, created_at, updated_at
		 FROM tasks WHERE id = ?`,
		id,
	)
	t, err := scanTask(row)
	if err != nil {
		return task.Task{}, fmt.Errorf("get task %q: %w", id, err)
	}
	return t, nil
}

// ListTasks returns all tasks ordered by updated_at DESC (newest activity first).
func (s *TaskStore) ListTasks() ([]task.Task, error) {
	rows, err := s.db.QueryContext(bg(),
		`SELECT id, title, input, spec, status, failed_stage, provider, model, project_slug, created_at, updated_at
		 FROM tasks ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]task.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return tasks, nil
}

// UpdateTask writes a full task update (status/spec/model/etc.) and bumps
// updated_at so the list reorders on activity. created_at is preserved.
func (s *TaskStore) UpdateTask(t task.Task) error {
	specJSON, err := json.Marshal(t.Spec)
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	res, err := s.db.ExecContext(bg(),
		`UPDATE tasks
		 SET title = ?, input = ?, spec = ?, status = ?, failed_stage = ?,
		     provider = ?, model = ?, project_slug = ?, updated_at = ?
		 WHERE id = ?`,
		t.Title, t.Input, string(specJSON), string(t.Status), t.FailedStage,
		t.Provider, t.Model, t.ProjectSlug, time.Now().UnixMilli(), t.ID,
	)
	if err != nil {
		return fmt.Errorf("update task %q: %w", t.ID, err)
	}
	return requireAffected(res, "update task", t.ID)
}

// DeleteTask removes a task; its runs cascade via the foreign key.
func (s *TaskStore) DeleteTask(id string) error {
	res, err := s.db.ExecContext(bg(), `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task %q: %w", id, err)
	}
	return requireAffected(res, "delete task", id)
}

// AddRun appends a new run to a task. When r.Seq is zero it is auto-assigned as
// max(seq)+1 for the task, so callers don't have to track iteration numbers.
// Returns the run with its generated id, seq and timestamps populated.
func (s *TaskStore) AddRun(r task.Run) (task.Run, error) {
	now := time.Now().UnixMilli()
	r.CreatedAt = now
	r.UpdatedAt = now
	if r.Status == "" {
		r.Status = "executing"
	}

	tx, err := s.db.BeginTx(bg(), nil)
	if err != nil {
		return task.Run{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if r.Seq == 0 {
		var maxSeq sql.NullInt64
		if err := tx.QueryRowContext(bg(),
			`SELECT MAX(seq) FROM task_runs WHERE task_id = ?`, r.TaskID,
		).Scan(&maxSeq); err != nil {
			return task.Run{}, fmt.Errorf("compute seq for task %q: %w", r.TaskID, err)
		}
		r.Seq = int(maxSeq.Int64) + 1
	}

	res, err := tx.ExecContext(bg(),
		`INSERT INTO task_runs
		 (task_id, seq, provider, model, feedback, result, status, error, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.TaskID, r.Seq, r.Provider, r.Model, r.Feedback, r.Result, r.Status, r.Error,
		r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return task.Run{}, fmt.Errorf("insert run for task %q: %w", r.TaskID, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return task.Run{}, fmt.Errorf("last insert id: %w", err)
	}
	r.ID = id

	if err := tx.Commit(); err != nil {
		return task.Run{}, fmt.Errorf("commit add run: %w", err)
	}
	return r, nil
}

// UpdateRun replaces the mutable fields of an existing run (result/status/error)
// and bumps updated_at, e.g. finalizing a streamed result.
func (s *TaskStore) UpdateRun(r task.Run) error {
	res, err := s.db.ExecContext(bg(),
		`UPDATE task_runs
		 SET provider = ?, model = ?, feedback = ?, result = ?, status = ?, error = ?, updated_at = ?
		 WHERE id = ?`,
		r.Provider, r.Model, r.Feedback, r.Result, r.Status, r.Error,
		time.Now().UnixMilli(), r.ID,
	)
	if err != nil {
		return fmt.Errorf("update run %d: %w", r.ID, err)
	}
	return requireAffectedID(res, "update run", r.ID)
}

// ListRuns returns all runs of a task ordered by seq ASC (oldest iteration
// first), so the review UI can walk the iteration history.
func (s *TaskStore) ListRuns(taskID string) ([]task.Run, error) {
	rows, err := s.db.QueryContext(bg(),
		`SELECT id, task_id, seq, provider, model, feedback, result, status, error, created_at, updated_at
		 FROM task_runs WHERE task_id = ? ORDER BY seq ASC`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("query runs for %q: %w", taskID, err)
	}
	defer rows.Close()

	runs := make([]task.Run, 0)
	for rows.Next() {
		var r task.Run
		if err := rows.Scan(
			&r.ID, &r.TaskID, &r.Seq, &r.Provider, &r.Model, &r.Feedback,
			&r.Result, &r.Status, &r.Error, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs: %w", err)
	}
	return runs, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows, so scanTask serves the
// single-row and list paths.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanTask reads one task row and unmarshals its spec JSON.
func scanTask(sc rowScanner) (task.Task, error) {
	var (
		t        task.Task
		specJSON string
		status   string
	)
	if err := sc.Scan(
		&t.ID, &t.Title, &t.Input, &specJSON, &status, &t.FailedStage,
		&t.Provider, &t.Model, &t.ProjectSlug, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return task.Task{}, err
	}
	t.Status = task.Status(status)
	if specJSON != "" {
		if err := json.Unmarshal([]byte(specJSON), &t.Spec); err != nil {
			return task.Task{}, fmt.Errorf("unmarshal spec: %w", err)
		}
	}
	return t, nil
}
