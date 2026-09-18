// Package claudedata reads Claude Code's on-disk data: session transcripts
// under ~/.claude/projects/<slug>/<id>.jsonl and the file-based memory
// (~/.claude/CLAUDE.md and per-project memory/). It is read-mostly; memory
// files can also be written back so the user can manage what Claude remembers.
package claudedata

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// configDirEnv is the environment variable Claude Code itself honours to
// relocate its data directory. Reading it here matters on machines whose
// profile lives somewhere unusual (common on Windows: a relocated
// %USERPROFILE%, or Claude Code driven from WSL/Git-Bash where HOME differs
// from the native profile). Without it, axon would scan an empty
// ~/.claude/projects and report "no sessions" even though hundreds exist.
const configDirEnv = "CLAUDE_CONFIG_DIR"

// Root returns the Claude Code data directory: $CLAUDE_CONFIG_DIR when set,
// otherwise ~/.claude.
func Root() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(configDirEnv)); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Project is one directory under ~/.claude/projects. Its slug encodes the
// original working directory (path separators replaced by dashes).
type Project struct {
	Slug string `json:"slug"`
	// Path is the best original working directory for display and for use as a
	// repo root: taken from the cwd recorded inside the newest transcript when
	// available, otherwise decoded best-effort from the slug (lossy — the
	// encoding maps both "/" and "-" to "-").
	Path string `json:"path"`
	// Namespace is the .axon-project namespace this directory's work belongs to,
	// resolved from the recorded cwd. Empty when no marker file applies, in which
	// case indexing falls back to treating the directory slug as the namespace.
	Namespace    string `json:"namespace"`
	SessionCount int    `json:"sessionCount"`
	UpdatedAt    int64  `json:"updatedAt"` // newest session mtime, unix millis
}

// SessionMeta is a lightweight summary of one .jsonl session file.
type SessionMeta struct {
	ID           string `json:"id"`
	ProjectSlug  string `json:"projectSlug"`
	Title        string `json:"title"`
	MessageCount int    `json:"messageCount"`
	UpdatedAt    int64  `json:"updatedAt"` // file mtime, unix millis
	SizeBytes    int64  `json:"sizeBytes"`
	// Cwd is the working directory the session ran in, read from the transcript's
	// own "cwd" field (exact, unlike the lossy slug). Needed to `cd` there before
	// `claude --resume`. Empty if the transcript never recorded one.
	Cwd string `json:"cwd"`
	// Model is the family label (Opus/Sonnet/Haiku/…) of the last real model that
	// answered in this session, derived from assistant events' message.model.
	// Empty if the transcript recorded no usable model (e.g. only synthetic
	// events). Used to group/filter sessions by model.
	Model string `json:"model"`
	// File is the absolute path of the transcript itself. Callers that already
	// hold a SessionMeta can read it straight back with ReadSessionFile instead of
	// re-deriving the path (which for Codex/Buddy used to mean a full tree walk
	// per session).
	File string `json:"file"`
	// Namespace is the .axon-project namespace this session belongs to, resolved
	// from Cwd. Empty when the session ran outside any marked project.
	Namespace string `json:"namespace"`
}

// uuidTail matches a trailing UUID inside a rollout file name, e.g.
// "rollout-2026-08-27T00-27-019e8b6c-d8ea-71a0-a0b7-c882c1e3cdbb".
var uuidTail = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// decodeSlug turns a project slug back into a readable path. The encoding is
// lossy (both '/' and '-' map to '-'), so this is best-effort for display and
// only used when a transcript never recorded its cwd.
func decodeSlug(slug string) string {
	// Windows slugs keep the drive letter and drop the colon: "D--work-glite-ui"
	// started life as "D:\work\glite-ui". Rebuild a native-looking path so the
	// project picker doesn't show a nonsensical "/D/work/glite/ui".
	if len(slug) >= 2 && slug[1] == '-' && isDriveLetter(slug[0]) {
		rest := strings.TrimPrefix(slug[2:], "-")
		return strings.ToUpper(slug[:1]) + `:\` + strings.ReplaceAll(rest, "-", `\`)
	}
	s := strings.TrimPrefix(slug, "-")
	return "/" + strings.ReplaceAll(s, "-", "/")
}

func isDriveLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
