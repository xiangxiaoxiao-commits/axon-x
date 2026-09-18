package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// indexFixture builds a throwaway world of four agents' session stores plus two
// marked repositories, and points every scanner at it via the same environment
// variables the real app respects.
type indexFixture struct {
	repoA     string // marked "nsA"
	repoB     string // marked "nsB"
	work      string // no marker
	claudeDir string
	codexDir  string
}

func newIndexFixture(t *testing.T) indexFixture {
	t.Helper()
	home := t.TempDir()
	claudeDir := filepath.Join(home, ".claude")
	codexDir := filepath.Join(home, ".codex")
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)
	t.Setenv("CODEX_HOME", codexDir)

	repos := t.TempDir()
	f := indexFixture{
		repoA: filepath.Join(repos, "repo-a"),
		repoB: filepath.Join(repos, "repo-b"),
		work:  repos,
	}
	for _, m := range []struct{ dir, ns string }{{f.repoA, "nsA"}, {f.repoB, "nsB"}} {
		if err := os.MkdirAll(m.dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, filepath.Join(m.dir, ".axon-project"), m.ns+"\n")
	}
	f.claudeDir = claudeDir
	f.codexDir = codexDir
	return f
}

func writeFixtureFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeClaudeSession creates ~/.claude/projects/<slug>/<id>.jsonl running in cwd.
func writeClaudeSession(t *testing.T, claudeDir, slug, id, cwd string) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(claudeDir, "projects", slug, id+".jsonl"),
		`{"type":"user","cwd":"`+cwd+`","message":{"role":"user","content":"work on it"}}`+"\n")
}

func writeCodexSession(t *testing.T, codexDir, name, id, cwd string) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(codexDir, "sessions", "2026", "01", "01", name),
		`{"type":"session_meta","payload":{"id":"`+id+`","cwd":"`+cwd+`"}}`+"\n"+
			`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"hello"}]}}`+"\n")
}

func writeBuddySession(t *testing.T, home, agent, id, cwd string) {
	t.Helper()
	writeFixtureFile(t, filepath.Join(home, agent, "projects", "-tmp-x", id+".jsonl"),
		`{"type":"message","role":"user","sessionId":"`+id+`","cwd":"`+cwd+`","content":[{"type":"input_text","text":"hi"}]}`+"\n")
}

func agentCounts(plan indexPlan) (claude, codex, buddy int) {
	return plan.Claude, plan.Codex, plan.Buddy
}

// The bug this guards: selecting a namespace (what the graph tags and the
// project dropdown actually hold) must sweep every agent's store for sessions
// whose recorded cwd belongs to that namespace, instead of looking for a Claude
// Code directory named after the namespace.
func TestPlanIndexSweepsAllAgentsByNamespace(t *testing.T) {
	f := newIndexFixture(t)
	writeClaudeSession(t, f.claudeDir, "-tmp-repo-a", "aaaa", f.repoA)
	writeClaudeSession(t, f.claudeDir, "-tmp-repo-b", "bbbb", f.repoB)
	writeClaudeSession(t, f.claudeDir, "-tmp-scratch", "cccc", f.work)
	writeCodexSession(t, f.codexDir, "rollout-2026-01-01T00-00-aaaaaaaa-1111-2222-3333-444444444444.jsonl", "cx1", f.repoA)
	writeBuddySession(t, os.Getenv("HOME"), ".workbuddy", "wb1", f.repoA)

	plan, err := planIndex("nsA")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Namespace != "nsA" {
		t.Fatalf("Namespace = %q, want nsA", plan.Namespace)
	}
	claude, codex, buddy := agentCounts(plan)
	if claude != 1 || codex != 1 || buddy != 1 {
		t.Fatalf("plan = claude:%d codex:%d buddy:%d, want 1/1/1 (sessions: %d)", claude, codex, buddy, len(plan.Sources))
	}
	if len(plan.Sources) != 3 {
		t.Fatalf("len(Sources) = %d, want 3", len(plan.Sources))
	}
	for _, s := range plan.Sources {
		if s.Meta.File == "" {
			t.Fatalf("source %s has no transcript path", s.Meta.ID)
		}
	}
	// The scratch session belongs to no namespace and must not be pulled in, but
	// it must be counted so the UI can explain the shortfall.
	if plan.Unmanaged != 1 {
		t.Fatalf("Unmanaged = %d, want 1", plan.Unmanaged)
	}
}

// A namespace that happens to share its name with a Claude Code directory is a
// red herring; and selecting a bare namespace must never be confused with an
// empty selection.
func TestPlanIndexRejectsEmptySelection(t *testing.T) {
	newIndexFixture(t)
	if _, err := planIndex("   "); err == nil {
		t.Fatal("planIndex(\"\") = nil error, want a selection error")
	}
	plan, err := planIndex("does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Sources) != 0 {
		t.Fatalf("unknown namespace produced %d sources, want 0", len(plan.Sources))
	}
}

// Selecting an unmarked Claude Code directory still works, filing knowledge under
// the directory name (the pre-namespace behaviour, kept for compatibility).
func TestPlanIndexFallsBackToDirectoryNameWhenUnmarked(t *testing.T) {
	f := newIndexFixture(t)
	slug := "-tmp-scratch"
	writeClaudeSession(t, f.claudeDir, slug, "cccc", f.work)

	plan, err := planIndex(slug)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Namespace != slug {
		t.Fatalf("Namespace = %q, want the directory name %q", plan.Namespace, slug)
	}
	if len(plan.Sources) != 1 || plan.Claude != 1 {
		t.Fatalf("plan = %+v, want exactly the directory's one transcript", plan)
	}
}

// Selecting a marked Claude Code directory files its knowledge under the
// namespace the directory's work belongs to, and takes the rest of that
// namespace's sessions (other repos, other agents) along with it.
func TestPlanIndexMapsClaudeDirectoryToItsNamespace(t *testing.T) {
	f := newIndexFixture(t)
	dirSlug := strings.ReplaceAll(strings.TrimPrefix(f.repoA, "/"), "/", "-")
	dirSlug = "-" + dirSlug
	writeClaudeSession(t, f.claudeDir, dirSlug, "aaaa", f.repoA)
	writeClaudeSession(t, f.claudeDir, "-tmp-repo-b-extra", "dddd", f.repoA)

	plan, err := planIndex(dirSlug)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Namespace != "nsA" {
		t.Fatalf("Namespace = %q, want nsA", plan.Namespace)
	}
	if len(plan.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2 (the directory plus the namespace's other dir)", len(plan.Sources))
	}
}
