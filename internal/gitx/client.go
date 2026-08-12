package gitx

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"strings"
)

// Size budgets for diff filtering/truncation (see docs/TECH_COMMIT.md §2.6).
// Conservative starting values: enough context for a good message without
// flooding the model with noise, secrets, or huge generated files.
const (
	maxFileDiffLines = 400   // per-file line cap before truncation
	maxFileDiffBytes = 20000 // per-file byte cap before truncation (~20 KB)
	maxTotalBytes    = 60000 // overall budget; beyond it we degrade to a summary
)

// client is the default Client backed by the system git CLI.
type client struct{}

// New returns a Client that shells out to the system `git` binary. It holds no
// state, so one instance is safe to reuse across repositories.
func New() Client { return &client{} }

// runGit executes `git args...` in dir and returns stdout. It never goes through
// a shell (exec.Command with an argv array), so there is no shell-injection
// surface. On failure it returns a readable error carrying git's stderr.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// Status implements Client. It first resolves the repo root; if dir is not a git
// repo, it returns RepoStatus{IsRepo:false} with no error (a normal, expected
// state the UI handles). Otherwise it reports branch and parsed working-tree
// changes from `git status --porcelain=v1`.
func (c *client) Status(dir string) (RepoStatus, error) {
	root, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		// Not a git repo (or dir does not exist): report cleanly, no error.
		return RepoStatus{IsRepo: false, Changes: []FileChange{}}, nil
	}
	st := RepoStatus{IsRepo: true, Root: strings.TrimSpace(root), Changes: []FileChange{}}

	// Current branch. On a detached HEAD `--abbrev-ref` yields "HEAD" (normalize
	// to ""); on an unborn branch (fresh repo, no commits) it also yields "HEAD",
	// so fall back to `branch --show-current`, which reports the unborn name.
	if branch, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		if b := strings.TrimSpace(branch); b != "HEAD" {
			st.Branch = b
		}
	}
	if st.Branch == "" {
		if branch, err := runGit(dir, "branch", "--show-current"); err == nil {
			st.Branch = strings.TrimSpace(branch)
		}
	}

	// Porcelain v1 has a stable, machine-parseable format: "XY <path>", where X
	// is the index (staged) status and Y the worktree (unstaged) status.
	out, err := runGit(dir, "status", "--porcelain=v1")
	if err != nil {
		return st, err
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		fc := parseStatusLine(line)
		if fc.Path == "" {
			continue
		}
		st.Changes = append(st.Changes, fc)
		if fc.Staged {
			st.HasStaged = true
		}
	}
	return st, nil
}

// parseStatusLine turns one `git status --porcelain=v1` line into a FileChange.
// Columns: X (index), Y (worktree), a space, then the path. Renames/copies show
// "XY old -> new"; we keep the new path. Untracked files are "?? path".
func parseStatusLine(line string) FileChange {
	x, y := line[0], line[1]
	rest := strings.TrimSpace(line[3:])

	fc := FileChange{}
	if x == '?' && y == '?' {
		fc.Path = unquotePath(rest)
		fc.Status = "?"
		fc.Unstaged = true
		return fc
	}

	// For renames/copies the path field is "old -> new"; keep the new name.
	p := rest
	if i := strings.Index(rest, " -> "); i >= 0 {
		p = rest[i+len(" -> "):]
	}
	fc.Path = unquotePath(p)

	fc.Staged = x != ' ' && x != '?'
	fc.Unstaged = y != ' ' && y != '?'

	// Prefer the meaningful (non-space) code for the simplified status.
	switch {
	case x != ' ':
		fc.Status = string(x)
	case y != ' ':
		fc.Status = string(y)
	}
	return fc
}

// unquotePath strips the surrounding quotes git adds for paths with special
// characters. Best-effort: on any parse doubt it returns the raw string.
func unquotePath(p string) string {
	if len(p) >= 2 && p[0] == '"' && p[len(p)-1] == '"' {
		return p[1 : len(p)-1]
	}
	return p
}

// Diff implements Client. It selects the diff command by scope, then filters and
// size-bounds the result. Returns the (possibly truncated/summarized) diff text
// and whether any truncation happened.
func (c *client) Diff(dir string, scope DiffScope) (string, bool, error) {
	var args []string
	switch scope {
	case ScopeStaged:
		args = []string{"diff", "--cached"}
	case ScopeUnstaged:
		args = []string{"diff"}
	case ScopeAll:
		args = []string{"diff", "HEAD"}
	default:
		args = []string{"diff", "--cached"}
	}

	raw, err := runGit(dir, args...)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	diff, truncated := filterAndTruncate(dir, scope, raw)
	return diff, truncated, nil
}

// filterAndTruncate splits a raw unified diff into per-file blocks and applies:
//   - skip binary files (content omitted, filename noted);
//   - skip lockfiles/generated artifacts and likely-secret files by path;
//   - per-file truncation beyond maxFileDiffLines / maxFileDiffBytes;
//   - overall degrade: if the assembled diff still exceeds maxTotalBytes, fall
//     back to a `--numstat` + hunk-header summary instead of full content.
//
// It returns the processed diff and whether anything was dropped/truncated.
func filterAndTruncate(dir string, scope DiffScope, raw string) (string, bool) {
	blocks := splitDiffBlocks(raw)
	truncated := false
	var out strings.Builder

	for _, blk := range blocks {
		p := diffBlockPath(blk)
		switch {
		case p == "":
			// Unrecognized preamble; keep as-is (rare).
			out.WriteString(blk)
		case skipPathSecret(p):
			fmt.Fprintf(&out, "diff --git a/%s b/%s\n# omitted: sensitive file (%s) — content not sent to the model\n", p, p, p)
			truncated = true
		case skipPathGenerated(p):
			fmt.Fprintf(&out, "diff --git a/%s b/%s\n# omitted: lockfile/generated artifact (%s) — content not sent\n", p, p, p)
			truncated = true
		case isBinaryBlock(blk):
			fmt.Fprintf(&out, "diff --git a/%s b/%s\n# omitted: binary file changed (%s)\n", p, p, p)
			truncated = true
		default:
			b, t := truncateBlock(blk)
			out.WriteString(b)
			truncated = truncated || t
		}
	}

	// Second-level degrade: overall still too large. Replace the whole thing
	// with a structured summary (numstat + hunk headers) so the model still
	// sees which files/functions changed and by how much.
	if out.Len() > maxTotalBytes {
		if summary, ok := diffSummary(dir, scope, raw); ok {
			return summary, true
		}
	}
	return out.String(), truncated
}

// splitDiffBlocks breaks a unified diff into per-file chunks, each starting at a
// "diff --git " line. Any leading content before the first header is kept as its
// own block so nothing is silently dropped.
func splitDiffBlocks(raw string) []string {
	const marker = "diff --git "
	idxs := []int{}
	for i := 0; i < len(raw); {
		j := strings.Index(raw[i:], marker)
		if j < 0 {
			break
		}
		abs := i + j
		// A header must be at start-of-string or start-of-line.
		if abs == 0 || raw[abs-1] == '\n' {
			idxs = append(idxs, abs)
			i = abs + len(marker)
		} else {
			i = abs + len(marker)
		}
	}
	if len(idxs) == 0 {
		return []string{raw}
	}
	var blocks []string
	if idxs[0] > 0 {
		blocks = append(blocks, raw[:idxs[0]])
	}
	for k, start := range idxs {
		end := len(raw)
		if k+1 < len(idxs) {
			end = idxs[k+1]
		}
		blocks = append(blocks, raw[start:end])
	}
	return blocks
}

// diffBlockPath extracts the new-side path ("b/…") from a block's "diff --git"
// header. Returns "" if the block is not a file header.
func diffBlockPath(blk string) string {
	nl := strings.IndexByte(blk, '\n')
	head := blk
	if nl >= 0 {
		head = blk[:nl]
	}
	const marker = "diff --git "
	if !strings.HasPrefix(head, marker) {
		return ""
	}
	rest := head[len(marker):]
	// rest is "a/<path> b/<path>"; take the part after the last " b/".
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+len(" b/"):]
	}
	return ""
}

// isBinaryBlock reports whether a diff block is for a binary file. git prints
// either "Binary files a/x and b/x differ" or a "GIT binary patch" section.
func isBinaryBlock(blk string) bool {
	return strings.Contains(blk, "\nBinary files ") || strings.Contains(blk, "\nGIT binary patch")
}

// truncateBlock caps one file's diff at maxFileDiffLines / maxFileDiffBytes,
// keeping the head (headers + first hunks) and appending a truncation note.
func truncateBlock(blk string) (string, bool) {
	if len(blk) <= maxFileDiffBytes {
		if strings.Count(blk, "\n") <= maxFileDiffLines {
			return blk, false
		}
	}
	lines := strings.SplitAfter(blk, "\n")
	var b strings.Builder
	kept := 0
	for _, ln := range lines {
		if kept >= maxFileDiffLines || b.Len()+len(ln) > maxFileDiffBytes {
			break
		}
		b.WriteString(ln)
		kept++
	}
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("# ... (truncated: file diff too large)\n")
	return b.String(), true
}

// diffSummary produces a compact structured summary: `--numstat` (per-file
// +add/-del, binary marked "-") plus each file's hunk headers (the "@@ … @@"
// lines, which carry the enclosing function/context). Used when the full diff
// blows the overall budget. Returns ok=false if git fails.
func diffSummary(dir string, scope DiffScope, raw string) (string, bool) {
	var numArgs []string
	switch scope {
	case ScopeUnstaged:
		numArgs = []string{"diff", "--numstat"}
	case ScopeAll:
		numArgs = []string{"diff", "HEAD", "--numstat"}
	default:
		numArgs = []string{"diff", "--cached", "--numstat"}
	}
	numstat, err := runGit(dir, numArgs...)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	b.WriteString("# diff too large — structured summary only (per-file +add/-del and changed hunks)\n\n")
	b.WriteString("## file changes (added\tdeleted\tpath)\n")
	b.WriteString(strings.TrimSpace(numstat))
	b.WriteString("\n\n## changed hunks (function/context headers)\n")
	for _, ln := range strings.Split(raw, "\n") {
		if strings.HasPrefix(ln, "+++ ") || strings.HasPrefix(ln, "--- ") {
			continue
		}
		if strings.HasPrefix(ln, "diff --git ") || strings.HasPrefix(ln, "@@ ") {
			b.WriteString(ln)
			b.WriteString("\n")
		}
	}
	return b.String(), true
}

// --- path blacklists (see docs/TECH_COMMIT.md §2.6) ---

// skipPathGenerated matches lockfiles and generated/vendored artifacts whose
// diff is noise for a commit message.
func skipPathGenerated(p string) bool {
	base := path.Base(p)
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
		"Cargo.lock", "poetry.lock", "composer.lock", "Gemfile.lock":
		return true
	}
	if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".min.js") ||
		strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".snap") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "node_modules", "vendor", "dist", "build", ".next", "target":
			return true
		}
	}
	return false
}

// skipPathSecret matches files that commonly hold secrets. Their content is
// never sent to the model, only the filename is noted.
func skipPathSecret(p string) bool {
	base := strings.ToLower(path.Base(p))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	for _, suf := range []string{".pem", ".key", ".p12", ".pfx", ".keystore", ".jks"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return true
	}
	if strings.Contains(base, "credential") || strings.Contains(base, "secret") {
		return true
	}
	return false
}

// Commit implements Client. When stageAll is true it runs `git add -A` first so
// the whole working tree is committed; otherwise only already-staged changes go
// in. The message is passed on stdin via `git commit -F -`, so newlines/quotes/
// `$`/CJK need no escaping and there is no injection surface. It never pushes and
// never forces. On success it returns the new commit's short hash.
func (c *client) Commit(dir, message string, stageAll bool) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", fmt.Errorf("commit message is empty")
	}
	if stageAll {
		if _, err := runGit(dir, "add", "-A"); err != nil {
			return "", err
		}
	}

	cmd := exec.Command("git", "commit", "-F", "-")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(message)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git commit failed: %s", msg)
	}

	hash, err := runGit(dir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}
