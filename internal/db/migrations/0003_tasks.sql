-- Phase orchestration: multi-task parallel execution + review.
-- `tasks` is the mutable task entity (status/spec change over its lifecycle);
-- `task_runs` is the append-only execution history — reject-and-iterate adds a
-- new run (feedback + result) instead of overwriting, so iterations are kept.

CREATE TABLE tasks (
    id           TEXT PRIMARY KEY,              -- UUID v4
    title        TEXT NOT NULL DEFAULT '',      -- short label (first line of input)
    input        TEXT NOT NULL DEFAULT '',      -- user's rough description
    spec         TEXT NOT NULL DEFAULT '',      -- enriched Spec as JSON (user-editable)
    status       TEXT NOT NULL DEFAULT 'draft', -- lifecycle state (see internal/task)
    failed_stage TEXT NOT NULL DEFAULT '',      -- where it failed, for retry ("enrich"|"execute")
    provider     TEXT NOT NULL DEFAULT '',      -- selected provider instance name ('' = default)
    model        TEXT NOT NULL DEFAULT '',      -- selected model id ('' = default)
    project_slug TEXT NOT NULL DEFAULT '',      -- optional knowledge-graph project for enrichment
    created_at   INTEGER NOT NULL,              -- unix epoch millis
    updated_at   INTEGER NOT NULL               -- unix epoch millis; drives list ordering
);

CREATE INDEX idx_tasks_updated_at ON tasks (updated_at DESC);
CREATE INDEX idx_tasks_status ON tasks (status);

-- task_runs: one row per execution attempt, keyed by autoincrement id so a run
-- is referenced stably (= runId in stream events); seq is the 1-based iteration
-- number within a task.
CREATE TABLE task_runs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    seq        INTEGER NOT NULL,                 -- 1-based iteration number within a task
    provider   TEXT NOT NULL DEFAULT '',
    model      TEXT NOT NULL DEFAULT '',
    feedback   TEXT NOT NULL DEFAULT '',         -- reviewer feedback that triggered this run
    result     TEXT NOT NULL DEFAULT '',         -- model output (accumulated stream text)
    status     TEXT NOT NULL DEFAULT 'executing',-- executing | done | error
    error      TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE
);

CREATE INDEX idx_task_runs_task ON task_runs (task_id, seq);
