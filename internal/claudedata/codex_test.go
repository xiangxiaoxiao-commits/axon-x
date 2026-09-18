package claudedata

import (
	"path/filepath"
	"testing"
)

// codexTranscript writes a transcript whose session_meta records are the given
// (id, cwd) pairs, followed by one user turn so the file looks real.
func codexTranscript(t *testing.T, path, firstID string, metas [][2]string) {
	t.Helper()
	body := ""
	for _, m := range metas {
		body += `{"type":"session_meta","payload":{"id":"` + m[0] + `","cwd":"` + m[1] + `"}}` + "\n"
	}
	body += `{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"do the thing"}]}}` + "\n"
	writeTestFile(t, path, body)
}

// Codex appends another session_meta on compaction or fork, and those records
// carry an ANCESTOR's id. Letting each record overwrite the id made 54 files on
// a real machine claim one older session's identity — 55 list entries that all
// opened the same transcript. Identity must come from the first record, which is
// the one that matches the file's own name.
func TestCodexIdentityComesFromTheFirstSessionMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-2026-09-01T10-00-00-019e0000-0000-7000-8000-000000000001.jsonl")
	codexTranscript(t, path, "", [][2]string{
		{"019e0000-0000-7000-8000-000000000001", "/work/one"},
		{"019e0000-0000-7000-8000-00000000aaaa", "/work/two"},
		{"019e0000-0000-7000-8000-00000000bbbb", "/work/three"},
	})

	m := parseCodexMeta(path)
	if m.ID != "019e0000-0000-7000-8000-000000000001" {
		t.Fatalf("parseCodexMeta id = %q, want the file's own id from the first session_meta", m.ID)
	}
	// The cwd is the opposite: the newest record is where the session now lives.
	if m.Cwd != "/work/three" {
		t.Fatalf("parseCodexMeta cwd = %q, want the newest recorded cwd", m.Cwd)
	}
}

// Two files that share an ancestor id must still come back as two identities —
// this is the shape that produced the duplicate list entries.
func TestCodexFilesSharingAnAncestorStayDistinct(t *testing.T) {
	dir := t.TempDir()
	shared := "019e0000-0000-7000-8000-00000000aaaa"
	a := filepath.Join(dir, "rollout-2026-09-01T10-00-00-019e0000-0000-7000-8000-000000000001.jsonl")
	b := filepath.Join(dir, "rollout-2026-09-02T10-00-00-019e0000-0000-7000-8000-000000000002.jsonl")
	codexTranscript(t, a, "", [][2]string{
		{"019e0000-0000-7000-8000-000000000001", "/work/one"},
		{shared, "/work/one"},
	})
	codexTranscript(t, b, "", [][2]string{
		{"019e0000-0000-7000-8000-000000000002", "/work/one"},
		{shared, "/work/one"},
	})

	ma, mb := parseCodexMeta(a), parseCodexMeta(b)
	if ma.ID == mb.ID {
		t.Fatalf("both files report id %q; they must keep their own identity", ma.ID)
	}
	if ma.ID == shared || mb.ID == shared {
		t.Fatalf("a file adopted the ancestor id %q (a=%q b=%q)", shared, ma.ID, mb.ID)
	}
}

// The listing must honour a scope instead of returning the whole store.
func TestListCodexSessionsFiltersByScope(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	// One session inside a marked project, one outside any marker.
	repo := filepath.Join(t.TempDir(), "mono-glite")
	writeTestFile(t, filepath.Join(repo, ".axon-project"), "wanlian\n")
	plain := t.TempDir()

	inside := filepath.Join(home, "sessions", "2026", "09", "01", "rollout-2026-09-01T10-00-00-019e0000-0000-7000-8000-000000000001.jsonl")
	outside := filepath.Join(home, "sessions", "2026", "09", "02", "rollout-2026-09-02T10-00-00-019e0000-0000-7000-8000-000000000002.jsonl")
	codexTranscript(t, inside, "", [][2]string{{"019e0000-0000-7000-8000-000000000001", repo}})
	codexTranscript(t, outside, "", [][2]string{{"019e0000-0000-7000-8000-000000000002", plain}})

	all, err := ListCodexSessions(ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("ScopeAll returned %d sessions, want 2", len(all))
	}

	ns, err := ListCodexSessions("wanlian")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Namespace != "wanlian" {
		t.Fatalf("namespace scope returned %+v, want just the marked session", ns)
	}

	orphan, err := ListCodexSessions(ScopeUnassigned)
	if err != nil {
		t.Fatal(err)
	}
	if len(orphan) != 1 || orphan[0].Namespace != "" {
		t.Fatalf("ScopeUnassigned returned %+v, want just the unmarked session", orphan)
	}
}
