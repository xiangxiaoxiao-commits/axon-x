package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo creates a fresh git repo in a temp dir with deterministic identity
// and default branch, so tests don't depend on the runner's global git config.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runs := [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	}
	for _, args := range runs {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestStatusNonRepo(t *testing.T) {
	st, err := New().Status(t.TempDir())
	if err != nil {
		t.Fatalf("Status non-repo returned error: %v", err)
	}
	if st.IsRepo {
		t.Fatalf("expected IsRepo=false for a non-repo dir")
	}
}

func TestStatusAndStagedFlag(t *testing.T) {
	dir := initRepo(t)
	c := New()

	write(t, dir, "untracked.txt", "hello\n")
	st, err := c.Status(dir)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !st.IsRepo {
		t.Fatalf("expected IsRepo=true")
	}
	if st.Branch != "main" {
		t.Fatalf("expected branch main, got %q", st.Branch)
	}
	if st.HasStaged {
		t.Fatalf("expected HasStaged=false with only an untracked file")
	}
	if len(st.Changes) != 1 || st.Changes[0].Status != "?" || !st.Changes[0].Unstaged {
		t.Fatalf("unexpected changes: %+v", st.Changes)
	}

	// Stage it and re-check.
	mustGit(t, dir, "add", "untracked.txt")
	st, _ = c.Status(dir)
	if !st.HasStaged {
		t.Fatalf("expected HasStaged=true after add")
	}
	if !st.Changes[0].Staged {
		t.Fatalf("expected file to be staged: %+v", st.Changes[0])
	}
}

func TestDiffStagedAndFilter(t *testing.T) {
	dir := initRepo(t)
	c := New()

	// A normal source file plus a lockfile and a secret file, all staged.
	write(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	write(t, dir, "go.sum", "example.com/x v1.0.0 h1:abc=\n")
	write(t, dir, ".env", "API_KEY=super-secret\n")
	mustGit(t, dir, "add", "-A")

	diff, _, err := c.Diff(dir, ScopeStaged)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "main.go") {
		t.Fatalf("expected source file in diff:\n%s", diff)
	}
	if strings.Contains(diff, "super-secret") {
		t.Fatalf("secret content leaked into diff:\n%s", diff)
	}
	if strings.Contains(diff, "example.com/x v1.0.0") {
		t.Fatalf("lockfile content should be omitted:\n%s", diff)
	}
}

func TestCommit(t *testing.T) {
	dir := initRepo(t)
	c := New()

	write(t, dir, "a.txt", "content\n")
	mustGit(t, dir, "add", "-A")

	// Message with newlines and special characters exercises the -F - stdin path.
	msg := "feat(core): add a.txt\n\nBody line with \"quotes\" and $VAR and 中文.\n"
	hash, err := c.Commit(dir, msg, false)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if hash == "" {
		t.Fatalf("expected a non-empty short hash")
	}

	got := mustGit(t, dir, "log", "-1", "--pretty=%B")
	if !strings.Contains(got, "feat(core): add a.txt") || !strings.Contains(got, "中文") {
		t.Fatalf("commit message not stored verbatim: %q", got)
	}

	// Clean tree after commit.
	st, _ := c.Status(dir)
	if len(st.Changes) != 0 {
		t.Fatalf("expected clean tree after commit, got %+v", st.Changes)
	}
}

func TestCommitStageAll(t *testing.T) {
	dir := initRepo(t)
	c := New()
	write(t, dir, "b.txt", "x\n")
	// Not staged; stageAll=true should add it.
	if _, err := c.Commit(dir, "chore: add b", true); err != nil {
		t.Fatalf("Commit stageAll: %v", err)
	}
	st, _ := c.Status(dir)
	if len(st.Changes) != 0 {
		t.Fatalf("expected clean tree after stageAll commit, got %+v", st.Changes)
	}
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
