// Package gitx wraps the git CLI for the commit tool: read repo state and
// diffs, and create commits. It shells out to `git` (never through a shell
// string) so behavior matches the user's terminal exactly. It never pushes and
// never force-anything; commits happen only when the caller explicitly asks.
package gitx

// FileChange is one changed path in the working tree / index.
type FileChange struct {
	Path     string `json:"path"`
	Status   string `json:"status"`   // "M" | "A" | "D" | "R" | "?" (porcelain code, simplified)
	Staged   bool   `json:"staged"`   // has staged changes
	Unstaged bool   `json:"unstaged"` // has unstaged changes
}

// RepoStatus is the snapshot the commit UI shows.
type RepoStatus struct {
	IsRepo  bool         `json:"isRepo"`
	Root    string       `json:"root"`   // absolute repo root
	Branch  string       `json:"branch"` // current branch (or "" if detached)
	Changes []FileChange `json:"changes"`
	// HasStaged is true if at least one file has staged changes.
	HasStaged bool `json:"hasStaged"`
}

// DiffScope selects which diff to read.
type DiffScope string

const (
	ScopeStaged   DiffScope = "staged"   // git diff --cached
	ScopeUnstaged DiffScope = "unstaged" // git diff
	ScopeAll      DiffScope = "all"      // staged + unstaged (working tree vs HEAD)
)

// Client runs git commands against a repository directory.
type Client interface {
	// Status returns repo state for dir (dir may be any path inside the repo).
	// If dir is not a git repo, returns RepoStatus{IsRepo:false} with no error.
	Status(dir string) (RepoStatus, error)

	// Diff returns the unified diff for the given scope, already filtered
	// (binary/lockfiles skipped) and size-bounded (large diffs summarized).
	// Returns the diff text and whether it was truncated.
	Diff(dir string, scope DiffScope) (diff string, truncated bool, err error)

	// Commit creates a commit in dir with the given message, passed via stdin
	// (`git commit -F -`) to avoid any quoting/injection. If stageAll is true it
	// runs `git add -A` first; otherwise only already-staged changes are
	// committed. Never pushes. Returns the new commit's short hash.
	Commit(dir, message string, stageAll bool) (hash string, err error)
}
