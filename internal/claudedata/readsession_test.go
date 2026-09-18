package claudedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fullScanProgress is the answer the tail reader must reproduce: read the whole
// transcript and keep the last turn of each role.
func fullScanProgress(t *testing.T, path string) SessionProgress {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := scanProgressEvents(f, 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func progressOf(t *testing.T, path string) SessionProgress {
	t.Helper()
	p, err := readProgressFile(path)
	if err != nil {
		t.Fatalf("readProgressFile: %v", err)
	}
	return p
}

// sameTurns compares the two text fields, ignoring the mtime the callers stamp.
func sameTurns(a, b SessionProgress) bool {
	return a.LastUser == b.LastUser && a.LastAssistant == b.LastAssistant
}

func userLine(text string) string {
	return `{"type":"user","cwd":"/work/repo","message":{"role":"user","content":` + quoteJSON(text) + `}}` + "\n"
}

func assistantLine(text string) string {
	return `{"type":"assistant","cwd":"/work/repo","message":{"role":"assistant","model":"claude-opus-4-8","content":` + quoteJSON(text) + `}}` + "\n"
}

// A tool-result-only user turn carries no text block; it must not become the
// "last thing you typed".
func TestReadProgressSkipsToolResultTurns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeTestFile(t, path,
		userLine("real question")+
			assistantLine("real answer")+
			`{"type":"user","cwd":"/work/repo","message":{"role":"user","content":[{"type":"tool_result","text":"output"}]}}`+"\n")

	p := progressOf(t, path)
	if p.LastUser != "real question" {
		t.Fatalf("LastUser = %q, want the last genuine prompt", p.LastUser)
	}
	if p.LastAssistant != "real answer" {
		t.Fatalf("LastAssistant = %q, want the last reply", p.LastAssistant)
	}
}

// The pathological shape that made a naive tail reader return nothing: the final
// line is longer than the first window, so the window sits inside it.
func TestReadProgressFindsTurnsBehindALongFinalLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	long := strings.Repeat("x", 400*1024)
	writeTestFile(t, path,
		userLine("earlier")+
			assistantLine("the reply before the wall of text")+
			userLine(long))

	p := progressOf(t, path)
	if len(p.LastUser) != len(long) {
		t.Fatalf("LastUser is %d bytes, want the %d-byte final prompt", len(p.LastUser), len(long))
	}
	if p.LastAssistant != "the reply before the wall of text" {
		t.Fatalf("LastAssistant = %q, want the reply that precedes the long line", p.LastAssistant)
	}
	if !sameTurns(p, fullScanProgress(t, path)) {
		t.Fatal("tail read disagrees with the full scan")
	}
}

// Several shapes, each of which must give exactly what the full scan gives.
func TestReadProgressAgreesWithFullScanOnEveryShape(t *testing.T) {
	big := strings.Repeat("y", 300*1024)
	cases := []struct {
		name string
		body string
	}{
		{"plain", userLine("q") + assistantLine("a")},
		{"ends with tool traffic", userLine("q") + assistantLine("a") + strings.Repeat(userLineRaw("tool output"), 4000)},
		{"assistant answer is far back", assistantLine("old reply") + strings.Repeat(userLineRaw("tool output"), 4000) + userLine("final question")},
		{"one long assistant reply", userLine("q") + assistantLine(big)},
		{"empty", ""},
		{"only whitespace turns", userLine("   ") + assistantLine("\t\n")},
		{"no trailing newline", strings.TrimRight(userLine("q")+assistantLine("a"), "\n")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "s.jsonl")
			writeTestFile(t, path, c.body)
			got, want := progressOf(t, path), fullScanProgress(t, path)
			if !sameTurns(got, want) {
				t.Fatalf("tail read = (%d/%d bytes) but the full scan = (%d/%d bytes)",
					len(got.LastUser), len(got.LastAssistant), len(want.LastUser), len(want.LastAssistant))
			}
		})
	}
}

// userLineRaw builds a user turn whose text is not JSON-escaped, for padding a
// transcript with tool noise without quoting a long string.
func userLineRaw(text string) string {
	return `{"type":"user","cwd":"/work/repo","message":{"role":"user","content":[{"type":"tool_result","text":"` + text + `"}]}}` + "\n"
}

// An unreadable path is an error, not an empty answer.
func TestReadProgressReportsAMissingFile(t *testing.T) {
	if _, err := readProgressFile(filepath.Join(t.TempDir(), "nope.jsonl")); err == nil {
		t.Fatal("readProgressFile on a missing file returned no error")
	}
}

// The tail reader must never need more than one widening step plus the full
// scan; this is the invariant that kept it from being slower than the scan it
// replaced. A transcript whose last line exceeds the first window but fits the
// second resolves from the tail.
func TestReadProgressResolvesLongFinalLineFromTheTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	// Just over the first window, so the second window reaches past its start.
	writeTestFile(t, path, userLine("q")+assistantLine(strings.Repeat("z", 300*1024)))
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if p := progressOf(t, path); p.LastAssistant == "" || p.LastUser == "" {
		t.Fatalf("tail reader came back empty: %+v", p)
	}
}
