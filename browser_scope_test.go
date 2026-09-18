package main

import (
	"os"
	"path/filepath"
	"testing"

	"axon/internal/claudedata"
)

// The tests below need every agent's session store to be the fixture's, not the
// machine's. HOME is the only switch all four honour (Claude Code, Codex and the
// two buddy agents derive their data directory from it), so it is pointed at a
// temporary home and the fixture is laid out inside it.
func isolateHome(t *testing.T) (home, markedRepo, plainDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")

	markedRepo = filepath.Join(t.TempDir(), "mono-glite")
	if err := os.MkdirAll(markedRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markedRepo, ".axon-project"), []byte("wanlian\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plainDir = t.TempDir()
	return home, markedRepo, plainDir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// jsonString quotes a path for embedding in a fixture transcript. (The package
// already has a shell-quoting `quote`, which is a different job.)
func jsonString(s string) string {
	out := `"`
	for _, r := range s {
		switch r {
		case '\\':
			out += `\\`
		case '"':
			out += `\"`
		default:
			out += string(r)
		}
	}
	return out + `"`
}

// writeClaudeTranscript drops one Claude Code transcript into the fixture home.
// The directory slug is a path encoding, deliberately unrelated to the namespace:
// a namespace spans repositories, so the slug is not how attribution works.
func writeClaudeTranscript(t *testing.T, home, slug, id, cwd string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".claude", "projects", slug, id+".jsonl"),
		`{"type":"user","cwd":`+jsonString(cwd)+`,"message":{"role":"user","content":"hello"}}`+"\n")
}

func writeCodexTranscript(t *testing.T, home, id, cwd string) {
	t.Helper()
	writeFile(t, filepath.Join(home, ".codex", "sessions", "2026", "09", "01", "rollout-2026-09-01T10-00-00-"+id+".jsonl"),
		`{"type":"session_meta","payload":{"id":"`+id+`","cwd":`+jsonString(cwd)+`}}`+"\n"+
			`{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"hi"}]}}`+"\n")
}

func writeBuddyTranscript(t *testing.T, home, agent, id, cwd string) {
	t.Helper()
	writeFile(t, filepath.Join(home, agent, "projects", "-work-proj", id+".jsonl"),
		`{"type":"message","sessionId":"`+id+`","role":"user","cwd":`+jsonString(cwd)+`,"content":[{"type":"input_text","text":"hi"}]}`+"\n")
}

// buildFixture lays down one session per agent inside the marked repo and one
// outside any marker, so both sides of every scope are populated.
func buildFixture(t *testing.T) (home string) {
	t.Helper()
	var repo, plain string
	home, repo, plain = isolateHome(t)

	writeClaudeTranscript(t, home, "-work-mono-glite", "claude-in", repo)
	writeClaudeTranscript(t, home, "-tmp-scratch", "claude-out", plain)
	writeCodexTranscript(t, home, "codex-in", repo)
	writeCodexTranscript(t, home, "codex-out", plain)
	writeBuddyTranscript(t, home, ".workbuddy", "buddy-in", repo)
	writeBuddyTranscript(t, home, ".workbuddy", "buddy-out", plain)
	return home
}

// The session browser's list must honour the scope it is given. It used to ignore
// the parameter entirely and always return every session on the machine, so the
// header's project selection had no visible effect on the page.
func TestListClaudeSessionsHonoursScope(t *testing.T) {
	buildFixture(t)
	a := &App{}

	all, err := a.ListClaudeSessions(claudedata.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Sessions) != 6 {
		t.Fatalf("ScopeAll returned %d sessions, want the 6 fixture sessions: %v", len(all.Sessions), ids(all.Sessions))
	}

	ns, err := a.ListClaudeSessions("wanlian")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(ns.Sessions); got != 3 {
		t.Fatalf("namespace scope returned %d sessions, want the 3 inside the marked repo: %v", got, ids(ns.Sessions))
	}
	for _, s := range ns.Sessions {
		if s.Namespace != "wanlian" {
			t.Fatalf("%s has namespace %q, want wanlian", s.ID, s.Namespace)
		}
	}
	// One from each agent: attribution must work the same for all four planes.
	// Claude Code ids are bare (the file name); the others carry an agent prefix.
	for _, want := range []string{"claude-in", "codex:codex-in", "workbuddy:buddy-in"} {
		if !containsID(ns.Sessions, want) {
			t.Errorf("namespace scope missed %q: %v", want, ids(ns.Sessions))
		}
	}

	orphan, err := a.ListClaudeSessions(claudedata.ScopeUnassigned)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(orphan.Sessions); got != 3 {
		t.Fatalf("ScopeUnassigned returned %d sessions, want the 3 outside any marker: %v", got, ids(orphan.Sessions))
	}
	for _, s := range orphan.Sessions {
		if s.Namespace != "" {
			t.Fatalf("ScopeUnassigned returned %s with namespace %q", s.ID, s.Namespace)
		}
	}

	for _, leaked := range []string{"claude-out", "codex:codex-out", "workbuddy:buddy-out"} {
		if containsID(ns.Sessions, leaked) {
			t.Fatalf("the namespace scope leaked %q from outside the repo: %v", leaked, ids(ns.Sessions))
		}
	}
	for _, leaked := range []string{"claude-in", "codex:codex-in", "workbuddy:buddy-in"} {
		if containsID(orphan.Sessions, leaked) {
			t.Fatalf("the unassigned scope leaked %q from inside the repo: %v", leaked, ids(orphan.Sessions))
		}
	}
}

// Every row must fall inside the scope that produced it, and the counts offered
// to the switcher must describe the same set the rows come from.
func TestListClaudeSessionsScopeCountsAreConsistent(t *testing.T) {
	buildFixture(t)
	a := &App{}

	all, err := a.ListClaudeSessions(claudedata.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Sessions) != all.Total {
		t.Fatalf("ScopeAll listed %d of %d sessions", len(all.Sessions), all.Total)
	}

	var unassigned, attributed int
	for _, s := range all.Sessions {
		if !claudedata.MatchesScope(s, claudedata.ScopeAll) {
			t.Fatalf("ScopeAll returned %s, which does not match the scope", s.ID)
		}
		if s.Namespace == "" {
			unassigned++
		} else {
			attributed++
		}
	}
	if unassigned != all.Unassigned {
		t.Fatalf("Unassigned = %d, but %d sessions carry no namespace", all.Unassigned, unassigned)
	}
	if all.Total != unassigned+attributed {
		t.Fatalf("Total %d != unassigned %d + attributed %d", all.Total, unassigned, attributed)
	}

	// The switcher always offers "未归属" and "全部" with those exact counts.
	byKey := map[string]int{}
	for _, o := range all.Scopes {
		byKey[o.Key] = o.Count
	}
	if got := byKey[claudedata.ScopeUnassigned]; got != all.Unassigned {
		t.Fatalf("未归属 chip shows %d, want %d", got, all.Unassigned)
	}
	if got := byKey[claudedata.ScopeAll]; got != all.Total {
		t.Fatalf("全部 chip shows %d, want %d", got, all.Total)
	}

	// Asking for a namespace offers that namespace first, with its own count.
	ns, err := a.ListClaudeSessions("wanlian")
	if err != nil {
		t.Fatal(err)
	}
	if len(ns.Scopes) != 3 {
		t.Fatalf("namespace scope offered %d chips, want 3: %+v", len(ns.Scopes), ns.Scopes)
	}
	if ns.Scopes[0].Key != "wanlian" || ns.Scopes[0].Label != "wanlian" {
		t.Fatalf("first chip = %+v, want the requested namespace", ns.Scopes[0])
	}
	if ns.Scopes[0].Count != len(ns.Sessions) {
		t.Fatalf("namespace chip count %d != %d listed sessions", ns.Scopes[0].Count, len(ns.Sessions))
	}
	for _, o := range ns.Scopes[1:] {
		if o.Key != claudedata.ScopeUnassigned && o.Key != claudedata.ScopeAll {
			t.Fatalf("unexpected chip %+v", o)
		}
	}
}

// An empty scope means "no selection yet"; it must reach everything rather than
// silently returning nothing.
func TestListClaudeSessionsTreatsEmptyScopeAsEverything(t *testing.T) {
	buildFixture(t)
	a := &App{}

	empty, err := a.ListClaudeSessions("")
	if err != nil {
		t.Fatal(err)
	}
	all, err := a.ListClaudeSessions(claudedata.ScopeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Sessions) != len(all.Sessions) {
		t.Fatalf("empty scope listed %d sessions, ScopeAll listed %d", len(empty.Sessions), len(all.Sessions))
	}
	if len(empty.Sessions) == 0 {
		t.Fatal("the fixture produced no sessions, so this test proves nothing")
	}
}

func ids(list []claudedata.SessionMeta) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, s.ID)
	}
	return out
}

func containsID(list []claudedata.SessionMeta, id string) bool {
	for _, s := range list {
		if s.ID == id {
			return true
		}
	}
	return false
}
