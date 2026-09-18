package claudedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionFileNameMatchesClassifiesExactAndLoose(t *testing.T) {
	id := "019e0000-0000-7000-8000-000000000001"
	cases := []struct {
		base         string
		exact, loose bool
		why          string
	}{
		{id + ".jsonl", true, true, "the id is the file's own name"},
		{"rollout-2026-09-01T10-00-00-" + id + ".jsonl", true, true, "the id is the name's trailing component"},
		{"019e0000-0000-7000-8000-0000000000010.jsonl", false, true, "the id only appears inside a longer name"},
		{"other.jsonl", false, false, "unrelated"},
		{id + ".txt", false, false, "not a transcript"},
		{".jsonl", false, false, "empty name"},
	}
	for _, c := range cases {
		exact, loose := sessionFileNameMatches(c.base, id)
		if exact != c.exact || loose != c.loose {
			t.Errorf("sessionFileNameMatches(%q) = (%v, %v), want (%v, %v) — %s",
				c.base, exact, loose, c.exact, c.loose, c.why)
		}
	}
}

// A session id that is a prefix of another session's id must not resolve to the
// wrong file. Loose matching alone used to do exactly that.
func TestFindSessionFilePrefersTheExactName(t *testing.T) {
	dir := t.TempDir()
	id := "019e0000-0000-7000-8000-000000000001"
	wrong := filepath.Join(dir, "a-"+id+"0.jsonl")
	right := filepath.Join(dir, "b-"+id+".jsonl")
	writeTestFile(t, wrong, "{}\n")
	writeTestFile(t, right, "{}\n")

	if got := findSessionFileUnder(id, dir); got != right {
		t.Fatalf("findSessionFileUnder = %q, want the exact match %q", got, right)
	}
}

func TestFindSessionFileFallsBackToALooseMatch(t *testing.T) {
	dir := t.TempDir()
	id := "019e0000-0000-7000-8000-000000000001"
	odd := filepath.Join(dir, "notes-"+id+"-extra.jsonl")
	writeTestFile(t, odd, "{}\n")

	if got := findSessionFileUnder(id, dir); got != odd {
		t.Fatalf("findSessionFileUnder = %q, want the loose match %q", got, odd)
	}
}

// The locate cache must re-walk when its remembered path disappears: a moved
// transcript has to be re-found, not reported at a stale location.
func TestLocateCacheDropsADeletedPath(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old", "s.jsonl")
	writeTestFile(t, old, "{}\n")
	rememberLocate(locateKindClaude, "sess-1", old)
	if got := locateCached(locateKindClaude, "sess-1"); got != old {
		t.Fatalf("locateCached = %q, want the remembered path", got)
	}

	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if got := locateCached(locateKindClaude, "sess-1"); got != "" {
		t.Fatalf("locateCached = %q after the file was deleted, want \"\"", got)
	}
}

// A cached path is also dropped when it now names a directory.
func TestLocateCacheRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "moved")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	rememberLocate(locateKindCodex, "sess-2", sub)
	if got := locateCached(locateKindCodex, "sess-2"); got != "" {
		t.Fatalf("locateCached = %q for a directory, want \"\"", got)
	}
}

// Agents derive identity differently, so the same id string must not resolve
// across them.
func TestLocateCacheIsKeyedPerAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	writeTestFile(t, path, "{}\n")
	rememberLocate(locateKindClaude, "same-id", path)

	if got := locateCached(locateKindCodex, "same-id"); got != "" {
		t.Fatalf("locateCached(codex) = %q, want \"\" — the cache leaked across agents", got)
	}
}
