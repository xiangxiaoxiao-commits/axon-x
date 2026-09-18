package claudedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setConfigDir points the package at a throwaway Claude data directory. Root()
// honours CLAUDE_CONFIG_DIR, so this exercises the same code path a relocated
// Windows profile would take.
func setConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(configDirEnv, dir)
	return dir
}

func writeSession(t *testing.T, projectDir, id, cwd string) string {
	t.Helper()
	path := filepath.Join(projectDir, id+".jsonl")
	body := `{"type":"user","cwd":` + quoteJSON(cwd) + `,"message":{"role":"user","content":"do the thing"}}` + "\n"
	writeTestFile(t, path, body)
	return path
}

func quoteJSON(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

// markProject drops a .axon-project marker naming ns into dir.
func markProject(t *testing.T, dir, ns string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, ".axon-project"), ns+"\n")
}

// A namespace is resolved through the cwd each transcript records, not through
// Claude Code's path-encoded directory name — the regression that made indexing
// a namespace find almost nothing.
func TestListSessionsForNamespaceResolvesThroughRecordedCwd(t *testing.T) {
	root := setConfigDir(t)
	work := t.TempDir()
	markProject(t, filepath.Join(work, "repo-a"), "nsA")
	markProject(t, filepath.Join(work, "repo-b"), "nsB")

	// Two Claude directories, one per repo, plus an unmarked scratch directory.
	dirA := filepath.Join(root, "projects", "-tmp-work-repo-a")
	dirB := filepath.Join(root, "projects", "-tmp-work-repo-b")
	dirNone := filepath.Join(root, "projects", "-tmp-scratch")
	wantA := writeSession(t, dirA, "aaaa-1111", filepath.Join(work, "repo-a"))
	writeSession(t, dirB, "bbbb-2222", filepath.Join(work, "repo-b"))
	writeSession(t, dirNone, "cccc-3333", work)

	got, err := ListSessionsForNamespace("nsA", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSessionsForNamespace(nsA) returned %d sessions, want 1", len(got))
	}
	if got[0].ID != "aaaa-1111" {
		t.Fatalf("got session %q, want aaaa-1111", got[0].ID)
	}
	if got[0].Namespace != "nsA" {
		t.Fatalf("Namespace = %q, want nsA", got[0].Namespace)
	}
	if got[0].File != wantA {
		t.Fatalf("File = %q, want %q", got[0].File, wantA)
	}

	// A namespace drawn from a subdirectory must not leak into its siblings.
	if other, _ := ListSessionsForNamespace("nsB", false); len(other) != 1 || other[0].ID != "bbbb-2222" {
		t.Fatalf("ListSessionsForNamespace(nsB) = %+v, want just bbbb-2222", other)
	}
	if none, _ := ListSessionsForNamespace("", false); len(none) != 0 {
		t.Fatalf("empty namespace should match nothing, got %d", len(none))
	}
}

func TestProjectNamespaceAndPathComeFromNewestSession(t *testing.T) {
	root := setConfigDir(t)
	work := t.TempDir()
	repo := filepath.Join(work, "mono-glite")
	markProject(t, repo, "wanlian")

	slug := "-tmp-work-mono-glite"
	writeSession(t, filepath.Join(root, "projects", slug), "dddd-4444", repo)

	if got := ProjectNamespace(slug); got != "wanlian" {
		t.Fatalf("ProjectNamespace() = %q, want wanlian", got)
	}
	if !HasProject(slug) {
		t.Fatal("HasProject() = false, want true for an existing project dir")
	}
	if HasProject("nsA") {
		t.Fatal("HasProject() = true for a bare namespace, want false")
	}

	projects, err := ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 1 {
		t.Fatalf("ListProjects() returned %d projects, want 1", len(projects))
	}
	if projects[0].Namespace != "wanlian" {
		t.Fatalf("Project.Namespace = %q, want wanlian", projects[0].Namespace)
	}
	if projects[0].Path != repo {
		t.Fatalf("Project.Path = %q, want the recorded cwd %q", projects[0].Path, repo)
	}
}

func TestReadSessionFileReadsWithoutProjectLookup(t *testing.T) {
	root := setConfigDir(t)
	dir := filepath.Join(root, "projects", "-tmp-x")
	path := writeSession(t, dir, "eeee-5555", t.TempDir())

	msgs, err := ReadSessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != "do the thing" {
		t.Fatalf("ReadSessionFile() = %+v, want one user message", msgs)
	}
}

// Windows slugs keep a drive letter; the old decoder rendered them as
// "/C/Users/me/…", which made the project picker unusable there. The encoding
// stays lossy ("-" is both a separator and a literal), which is why Project.Path
// prefers the cwd recorded inside the transcript.
func TestDecodeSlugHandlesWindowsDriveLetter(t *testing.T) {
	if got := decodeSlug("D--work-mono-glite"); got != `D:\work\mono\glite` {
		t.Fatalf("decodeSlug() = %q, want a native D:\\ path", got)
	}
	if strings.HasPrefix(decodeSlug("C--Users-me-proj"), "/") {
		t.Fatalf("decodeSlug() = %q, want no leading slash for a drive-letter slug", decodeSlug("C--Users-me-proj"))
	}
	if got := decodeSlug("-Users-me-projects"); got != "/Users/me/projects" {
		t.Fatalf("decodeSlug() = %q, want /Users/me/projects", got)
	}
}

// Rollout file names used to yield junk ids like "00-27-019e8b6c-…" that then
// keyed the knowledge cache; the id is the trailing UUID.
func TestCodexIDFromFileNameExtractsUUID(t *testing.T) {
	got := codexIDFromFileName("rollout-2026-08-27T00-27-019e8b6c-d8ea-71a0-a0b7-c882c1e3cdbb.jsonl")
	want := "019e8b6c-d8ea-71a0-a0b7-c882c1e3cdbb"
	if got != want {
		t.Fatalf("codexIDFromFileName() = %q, want %q", got, want)
	}
	if got := codexIDFromFileName("plain-name.jsonl"); got != "plain-name" {
		t.Fatalf("codexIDFromFileName() = %q, want plain-name", got)
	}
}

// probeCwd must classify a transcript without reading the whole conversation.
func TestProbeCwdReadsOnlyTheHead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	writeTestFile(t, path, `{"type":"last-prompt","lastPrompt":"hi"}`+"\n"+
		`{"type":"user","cwd":"/work/repo","message":{"role":"user","content":"x"}}`+"\n")
	if got := probeCwd(path); got != "/work/repo" {
		t.Fatalf("probeCwd() = %q, want /work/repo", got)
	}
}

func TestProbeCodexAndBuddyCwd(t *testing.T) {
	dir := t.TempDir()
	codex := filepath.Join(dir, "rollout.jsonl")
	writeTestFile(t, codex, `{"type":"session_meta","payload":{"id":"c1","cwd":"/work/repo"}}`+"\n")
	if got := probeCodexCwd(codex); got != "/work/repo" {
		t.Fatalf("probeCodexCwd() = %q, want /work/repo", got)
	}

	buddy := filepath.Join(dir, "b.jsonl")
	writeTestFile(t, buddy, `{"type":"message","role":"user","cwd":"/work/repo","content":[{"type":"input_text","text":"hi"}]}`+"\n")
	if got := probeBuddyCwd(buddy); got != "/work/repo" {
		t.Fatalf("probeBuddyCwd() = %q, want /work/repo", got)
	}
}

// A workspace root whose children all belong to one namespace is treated as that
// namespace; disagreement leaves the session unattributed instead of guessing.
func TestResolveNamespaceFromChildren(t *testing.T) {
	root := t.TempDir()
	markProject(t, filepath.Join(root, "repo-a"), "nsA")
	markProject(t, filepath.Join(root, "repo-b"), "nsA")
	if got := ResolveNamespace(root); got != "nsA" {
		t.Fatalf("ResolveNamespace(workspace root) = %q, want nsA", got)
	}

	mixed := t.TempDir()
	markProject(t, filepath.Join(mixed, "repo-a"), "nsA")
	markProject(t, filepath.Join(mixed, "repo-b"), "nsB")
	if got := ResolveNamespace(mixed); got != "" {
		t.Fatalf("ResolveNamespace(ambiguous root) = %q, want empty", got)
	}

	if got := ResolveNamespace(t.TempDir()); got != "" {
		t.Fatalf("ResolveNamespace(unmarked dir) = %q, want empty", got)
	}
}

// The home directory is not a project container: one marked checkout next to
// Desktop/Documents must not capture every scratch session in ~.
func TestResolveNamespaceIgnoresHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	markProject(t, filepath.Join(home, "some-repo"), "nsA")

	if got := ResolveNamespace(home); got != "" {
		t.Fatalf("ResolveNamespace(home) = %q, want empty", got)
	}
	if got := ResolveNamespace(filepath.Join(home, "some-repo")); got != "nsA" {
		t.Fatalf("ResolveNamespace(marked child) = %q, want nsA", got)
	}
}
