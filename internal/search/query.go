package search

import (
	"fmt"
	"strings"
)

// Query returns messages whose content contains the keyword (case-insensitive
// substring). If projectSlug is non-empty, results are limited to it. Newest
// first, capped at limit.
func (ix *Index) Query(keyword, projectSlug string, limit int) ([]Hit, error) {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return []Hit{}, nil
	}
	if limit <= 0 {
		limit = 50
	}

	q := `SELECT project_slug, session_id, title, role, content, updated_at
	      FROM docs WHERE content LIKE ? ESCAPE '\'`
	args := []any{"%" + escapeLike(kw) + "%"}
	if projectSlug != "" {
		q += ` AND project_slug = ?`
		args = append(args, projectSlug)
	}
	q += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := ix.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	out := make([]Hit, 0)
	for rows.Next() {
		var h Hit
		var content string
		if err := rows.Scan(&h.ProjectSlug, &h.SessionID, &h.Title, &h.Role, &content, &h.UpdatedAt); err != nil {
			return nil, err
		}
		h.Snippet = snippet(content, kw)
		out = append(out, h)
	}
	return out, rows.Err()
}

// escapeLike escapes LIKE wildcards so a literal keyword matches literally.
func escapeLike(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return r.Replace(s)
}

// snippet returns text around the first case-insensitive match of kw.
func snippet(content, kw string) string {
	lc, lk := strings.ToLower(content), strings.ToLower(kw)
	i := strings.Index(lc, lk)
	if i < 0 {
		if len(content) > 160 {
			return content[:160] + "…"
		}
		return content
	}
	// Work in runes to avoid cutting multibyte (Chinese) characters.
	runes := []rune(content)
	// Map byte index i to rune index.
	ri := len([]rune(content[:i]))
	start := ri - 40
	if start < 0 {
		start = 0
	}
	end := ri + len([]rune(kw)) + 60
	if end > len(runes) {
		end = len(runes)
	}
	s := string(runes[start:end])
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s = s + "…"
	}
	return s
}
