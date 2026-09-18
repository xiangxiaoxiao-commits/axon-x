package claudedata

import (
	"fmt"
	"path/filepath"
	"testing"
)

// scanFiles must keep the caller's order: the list is sorted afterwards, but a
// stable base order is what makes that sort deterministic.
func TestScanFilesPreservesOrder(t *testing.T) {
	paths := []string{"a", "b", "c", "d", "e"}
	got := scanFiles(paths, func(p string) SessionMeta { return SessionMeta{ID: p} })
	if len(got) != len(paths) {
		t.Fatalf("scanFiles returned %d results, want %d", len(got), len(paths))
	}
	for i, p := range paths {
		if got[i].ID != p {
			t.Fatalf("scanFiles[%d] = %q, want %q (order not preserved)", i, got[i].ID, p)
		}
	}
}

func TestScanFilesHandlesEmptyInput(t *testing.T) {
	if got := scanFiles(nil, func(string) SessionMeta { return SessionMeta{ID: "x"} }); got != nil {
		t.Fatalf("scanFiles(nil) = %v, want nil", got)
	}
}

// The metadata cache must never serve a stale answer: a transcript that grew
// gets re-read. Getting this wrong would quietly hide new sessions and
// misreport message counts.
func TestScanCacheRereadsAChangedTranscript(t *testing.T) {
	setConfigDir(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	writeTestFile(t, path, `{"type":"user","cwd":"/work/repo","message":{"role":"user","content":"one"}}`+"\n")

	first := scanClaudeMeta(path, "proj")
	if first.MessageCount != 1 {
		t.Fatalf("first scan MessageCount = %d, want 1", first.MessageCount)
	}
	// A second scan of an untouched file may come from the cache and must match.
	if again := scanClaudeMeta(path, "proj"); again.MessageCount != 1 || again.Title != first.Title {
		t.Fatalf("cached scan = %+v, want the same result as %+v", again, first)
	}

	writeTestFile(t, path, `{"type":"user","cwd":"/work/repo","message":{"role":"user","content":"one"}}`+"\n"+
		`{"type":"assistant","cwd":"/work/repo","message":{"role":"assistant","model":"claude-opus-4-8","content":"two"}}`+"\n")

	after := scanClaudeMeta(path, "proj")
	if after.MessageCount != 2 {
		t.Fatalf("after appending, MessageCount = %d, want 2 (stale cache)", after.MessageCount)
	}
	if after.Model != "claude-opus-4-8" {
		t.Fatalf("after appending, Model = %q, want claude-opus-4-8 (stale cache)", after.Model)
	}
}

// Codex and Claude key the cache separately: a path is scanned by more than one
// scanner in the same process and their ids are derived differently.
func TestScanCacheKeysPerScanner(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.jsonl")
	writeTestFile(t, path, `{"type":"session_meta","payload":{"id":"c1","session_id":"legacy","cwd":"/work/repo"}}`+"\n"+
		`{"type":"user","cwd":"/work/repo","message":{"role":"user","content":"hi"}}`+"\n")

	codex := scanCodexMeta(path)
	if codex.ID != "codex:c1" {
		t.Fatalf("codex id = %q, want codex:c1", codex.ID)
	}
	claude := scanClaudeMeta(path, "proj")
	if claude.ID != "shared" {
		t.Fatalf("claude id = %q, want the file name", claude.ID)
	}
	if claude.Model == "codex" {
		t.Fatalf("claude scan picked up the codex plane: %+v", claude)
	}
}

// A large store of sessions is read through a worker pool; the cache behind it
// is shared, so this test exists to be run under -race.
func TestParallelScanIsRaceFree(t *testing.T) {
	dir := t.TempDir()
	paths := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		p := filepath.Join(dir, fmt.Sprintf("s%02d.jsonl", i))
		writeTestFile(t, p, fmt.Sprintf(`{"type":"user","cwd":"/work/repo","message":{"role":"user","content":"m%d"}}`, i)+"\n")
		paths = append(paths, p)
	}
	got := scanFiles(paths, func(p string) SessionMeta { return scanClaudeMeta(p, "proj") })
	if len(got) != len(paths) {
		t.Fatalf("got %d results, want %d", len(got), len(paths))
	}
	for i := range got {
		if got[i].MessageCount != 1 {
			t.Fatalf("result %d = %+v, want one message", i, got[i])
		}
	}
	// Second pass: all from cache, still correct.
	again := scanFiles(paths, func(p string) SessionMeta { return scanClaudeMeta(p, "proj") })
	for i := range again {
		if again[i].ID != got[i].ID || again[i].MessageCount != got[i].MessageCount {
			t.Fatalf("cached pass %d differs: %+v vs %+v", i, again[i], got[i])
		}
	}
}
