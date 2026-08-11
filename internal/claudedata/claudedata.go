// Package claudedata reads Claude Code's on-disk data: session transcripts
// under ~/.claude/projects/<slug>/<id>.jsonl and the file-based memory
// (~/.claude/CLAUDE.md and per-project memory/). It is read-mostly; memory
// files can also be written back so the user can manage what Claude remembers.
package claudedata

import (
	"os"
	"path/filepath"
)

// Root returns the ~/.claude directory.
func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// Project is one directory under ~/.claude/projects. Its slug encodes the
// original working directory (path separators replaced by dashes).
type Project struct {
	Slug         string `json:"slug"`
	Path         string `json:"path"` // decoded best-effort original path
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
}
