// Package search provides fast keyword search over Claude Code session
// content. It uses substring (LIKE) matching rather than FTS5: Chinese has no
// word boundaries, so substring match is both simpler and more accurate here,
// and a few thousand messages stay fast. The index is a standalone SQLite DB.
package search

import (
	"database/sql"
	"fmt"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Index is the keyword-search index over session messages.
type Index struct {
	db *sql.DB
}

// Hit is one matched message with a snippet and its location.
type Hit struct {
	ProjectSlug string `json:"projectSlug"`
	SessionID   string `json:"sessionId"`
	Title       string `json:"title"`
	Role        string `json:"role"`
	Snippet     string `json:"snippet"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// Open creates/opens the search index database under dataDir.
func Open(dataDir string) (*Index, error) {
	p := filepath.Join(dataDir, "search.db")
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", p))
	if err != nil {
		return nil, fmt.Errorf("open search db: %w", err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS docs (
		project_slug TEXT NOT NULL,
		session_id   TEXT NOT NULL,
		title        TEXT NOT NULL DEFAULT '',
		role         TEXT NOT NULL,
		content      TEXT NOT NULL,
		updated_at   INTEGER NOT NULL
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create docs table: %w", err)
	}
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_docs_session ON docs(session_id)`)
	return &Index{db: db}, nil
}

// Close releases the index.
func (ix *Index) Close() error { return ix.db.Close() }

// HasSession reports whether a session is already indexed (dedup on re-index).
func (ix *Index) HasSession(sessionID string) bool {
	var n int
	ix.db.QueryRow(`SELECT COUNT(1) FROM docs WHERE session_id = ? LIMIT 1`, sessionID).Scan(&n)
	return n > 0
}

// AddMessage indexes one message.
func (ix *Index) AddMessage(projectSlug, sessionID, title, role, content string, updatedAt int64) error {
	_, err := ix.db.Exec(
		`INSERT INTO docs (project_slug, session_id, title, role, content, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		projectSlug, sessionID, title, role, content, updatedAt,
	)
	return err
}
