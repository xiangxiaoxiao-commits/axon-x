package sqlite_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"axon/internal/db"
	"axon/internal/store/sqlite"
	"axon/internal/task"
)

// newTaskStore opens a fresh, isolated database under t.TempDir() and returns a
// TaskStore plus the raw *sql.DB for direct schema assertions.
func newTaskStore(t *testing.T) (*sqlite.TaskStore, *sql.DB) {
	t.Helper()
	sqlDB, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return sqlite.NewTaskStore(sqlDB), sqlDB
}

func mustCreateTask(t *testing.T, s *sqlite.TaskStore, title string) task.Task {
	t.Helper()
	created, err := s.CreateTask(task.Task{
		Title: title,
		Input: "rough input for " + title,
		Spec:  task.Spec{Goal: "goal " + title, Constraints: []string{"c1", "c2"}},
	})
	if err != nil {
		t.Fatalf("CreateTask(%q): %v", title, err)
	}
	return created
}

// TestCreateTask verifies UUID generation, default status and timestamps.
func TestCreateTask(t *testing.T) {
	s, _ := newTaskStore(t)

	created, err := s.CreateTask(task.Task{Input: "do the thing"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(created.ID) != 36 {
		t.Errorf("expected 36-char UUID, got %q", created.ID)
	}
	if created.Status != task.StatusDraft {
		t.Errorf("expected default status %q, got %q", task.StatusDraft, created.Status)
	}
	if created.CreatedAt == 0 || created.CreatedAt != created.UpdatedAt {
		t.Errorf("timestamps: created=%d updated=%d", created.CreatedAt, created.UpdatedAt)
	}
}

// TestGetTaskRoundTrip verifies the spec JSON round-trips and not-found reports
// sql.ErrNoRows.
func TestGetTaskRoundTrip(t *testing.T) {
	s, _ := newTaskStore(t)

	created := mustCreateTask(t, s, "alpha")
	got, err := s.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Spec.Goal != "goal alpha" {
		t.Errorf("spec goal round-trip: got %q", got.Spec.Goal)
	}
	if len(got.Spec.Constraints) != 2 || got.Spec.Constraints[0] != "c1" {
		t.Errorf("spec constraints round-trip: got %+v", got.Spec.Constraints)
	}

	if _, err := s.GetTask("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestListTasksOrder verifies tasks come back updated_at DESC.
func TestListTasksOrder(t *testing.T) {
	s, _ := newTaskStore(t)

	first := mustCreateTask(t, s, "first")
	time.Sleep(5 * time.Millisecond)
	second := mustCreateTask(t, s, "second")
	time.Sleep(5 * time.Millisecond)
	third := mustCreateTask(t, s, "third")

	list, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(list))
	}
	want := []string{third.ID, second.ID, first.ID}
	for i, id := range want {
		if list[i].ID != id {
			t.Errorf("position %d: got %q, want %q", i, list[i].ID, id)
		}
	}
}

// TestUpdateTask verifies a full update persists spec/status and bumps updated_at.
func TestUpdateTask(t *testing.T) {
	s, _ := newTaskStore(t)

	created := mustCreateTask(t, s, "beta")
	time.Sleep(5 * time.Millisecond)

	created.Status = task.StatusReviewSpec
	created.Spec.Goal = "new goal"
	created.FailedStage = ""
	if err := s.UpdateTask(created); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	got, err := s.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != task.StatusReviewSpec {
		t.Errorf("status not updated: got %q", got.Status)
	}
	if got.Spec.Goal != "new goal" {
		t.Errorf("spec not updated: got %q", got.Spec.Goal)
	}
	if got.UpdatedAt <= created.UpdatedAt {
		t.Errorf("updated_at did not advance: before=%d after=%d", created.UpdatedAt, got.UpdatedAt)
	}

	missing := task.Task{ID: "nope"}
	if err := s.UpdateTask(missing); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("update missing: expected sql.ErrNoRows, got %v", err)
	}
}

// TestRunsAutoSeqAndOrder verifies AddRun auto-increments seq and ListRuns
// returns runs by seq ASC.
func TestRunsAutoSeqAndOrder(t *testing.T) {
	s, _ := newTaskStore(t)

	tk := mustCreateTask(t, s, "runs")
	r1, err := s.AddRun(task.Run{TaskID: tk.ID, Model: "m1"})
	if err != nil {
		t.Fatalf("AddRun 1: %v", err)
	}
	if r1.Seq != 1 {
		t.Errorf("first run seq: got %d, want 1", r1.Seq)
	}
	if r1.ID == 0 {
		t.Error("expected non-zero run id")
	}
	r2, err := s.AddRun(task.Run{TaskID: tk.ID, Model: "m2", Feedback: "fix it"})
	if err != nil {
		t.Fatalf("AddRun 2: %v", err)
	}
	if r2.Seq != 2 {
		t.Errorf("second run seq: got %d, want 2", r2.Seq)
	}

	// Finalize a run.
	r1.Result = "output text"
	r1.Status = "done"
	if err := s.UpdateRun(r1); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	runs, err := s.ListRuns(tk.ID)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].Seq != 1 || runs[1].Seq != 2 {
		t.Errorf("runs not ordered by seq asc: %d, %d", runs[0].Seq, runs[1].Seq)
	}
	if runs[0].Result != "output text" || runs[0].Status != "done" {
		t.Errorf("run 1 not finalized: %+v", runs[0])
	}
	if runs[1].Feedback != "fix it" {
		t.Errorf("run 2 feedback: got %q", runs[1].Feedback)
	}
}

// TestDeleteTaskCascade verifies deleting a task removes its runs via
// ON DELETE CASCADE (confirms foreign_keys=ON is in effect).
func TestDeleteTaskCascade(t *testing.T) {
	s, sqlDB := newTaskStore(t)

	tk := mustCreateTask(t, s, "cascade")
	for i := 0; i < 3; i++ {
		if _, err := s.AddRun(task.Run{TaskID: tk.ID}); err != nil {
			t.Fatalf("AddRun: %v", err)
		}
	}

	var before int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM task_runs WHERE task_id = ?`, tk.ID).Scan(&before); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if before != 3 {
		t.Fatalf("expected 3 runs before delete, got %d", before)
	}

	if err := s.DeleteTask(tk.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	var after int
	if err := sqlDB.QueryRow(`SELECT COUNT(*) FROM task_runs WHERE task_id = ?`, tk.ID).Scan(&after); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if after != 0 {
		t.Errorf("expected 0 runs after cascade delete, got %d (foreign_keys likely OFF)", after)
	}

	if err := s.DeleteTask("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("delete missing: expected sql.ErrNoRows, got %v", err)
	}
}
