package claudedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFilesRecursiveIncludesSubagents(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "parent.jsonl"), "{}\n")
	writeTestFile(t, filepath.Join(dir, "parent", "subagents", "agent-one.jsonl"), "{}\n")
	writeTestFile(t, filepath.Join(dir, "parent", "subagents", "ignore.txt"), "")

	top, err := sessionFiles(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 {
		t.Fatalf("top-level scan returned %d files, want 1", len(top))
	}

	all, err := sessionFiles(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("recursive scan returned %d files, want 2", len(all))
	}
}

func TestFindClaudeSessionFileFindsNestedTranscript(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "projects", "project", "parent", "subagents", "agent-one.jsonl")
	writeTestFile(t, want, "{}\n")

	if got := findClaudeSessionFile(root, "project", "agent-one"); got != want {
		t.Fatalf("findClaudeSessionFile() = %q, want %q", got, want)
	}
}

func TestScanCodexMetaReadsCurrentIDField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeTestFile(t, path, `{"type":"session_meta","payload":{"id":"current-id","session_id":"legacy-id","cwd":"/tmp"}}`+"\n")

	meta := scanCodexMeta(path)
	if meta.ID != "codex:current-id" {
		t.Fatalf("scanCodexMeta().ID = %q, want codex:current-id", meta.ID)
	}
}

func TestScanTitleAndCountFallsBackToFirstUserMessage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-one.jsonl")
	writeTestFile(t, path, `{"type":"user","cwd":"/work/project","message":{"role":"user","content":"Investigate the missing sessions"}}`+"\n")

	title, count, cwd, _ := scanTitleAndCount(path)
	if title != "Investigate the missing sessions" {
		t.Fatalf("title = %q, want first user message", title)
	}
	if count != 1 || cwd != "/work/project" {
		t.Fatalf("count/cwd = %d/%q, want 1//work/project", count, cwd)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
