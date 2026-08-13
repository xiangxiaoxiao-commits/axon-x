package main

import (
	"os"
	"path/filepath"
	"strings"
)

// This file resolves a project slug WITHOUT the agent having to pass one.
//
// Claude Code encodes a project's absolute path into its data-dir slug by
// replacing every path separator with '-' (e.g. /Users/me/app -> -Users-me-app),
// and it spawns this stdio server with its working directory set to the project
// root. So the server can recover the slug from os.Getwd() alone — the single
// biggest per-call friction was that every search/get/remember demanded an
// explicit `project`, forcing an upfront list_projects round-trip or a
// hand-computed slug. `project` stays honored when given (explicit wins); it just
// stops being mandatory.

// encodeSlug turns an absolute path into the slug scheme Claude Code uses:
// every path separator becomes '-'. The transform is deterministic from a real
// path, so the slug computed from a live cwd exactly matches the on-disk cache
// dir created for that same cwd — even though decodeSlug is lossy in the other
// direction.
func encodeSlug(absPath string) string {
	s := filepath.ToSlash(absPath)
	return strings.ReplaceAll(s, "/", "-")
}

// slugSource tags where a resolved slug came from, so callers can tailor the
// "nothing found" message (a wrong explicit slug vs. an auto-derived one that
// simply has no graph yet read very differently to the model).
type slugSource int

const (
	slugExplicit slugSource = iota // caller passed project
	slugMatched                    // derived from cwd and a cache dir exists
	slugDerived                    // derived from cwd, no cache yet (bootstrap)
	slugUnknown                    // no explicit slug and cwd unavailable
)

// resolveSlug picks the project slug for a call. An explicit non-empty arg always
// wins (backward compatible). Otherwise it derives one from the working
// directory: it prefers a cwd (or nearest ancestor) that already has a graphcache
// dir — so running the agent from a subdirectory still finds the project — and
// falls back to the raw cwd slug for bootstrapping a brand-new project.
func (h *toolHandler) resolveSlug(explicit string) (string, slugSource) {
	if s := strings.TrimSpace(explicit); s != "" {
		return s, slugExplicit
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", slugUnknown
	}
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	}
	existing := h.cacheDirSet()
	// Walk cwd upward; the deepest dir with a cache wins (nearest project root).
	for dir := wd; ; {
		if slug := encodeSlug(dir); existing[slug] {
			return slug, slugMatched
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	// No cache anywhere up the tree: derive from cwd so a first remember_knowledge
	// can bootstrap the project's graph under the correct slug.
	return encodeSlug(wd), slugDerived
}

// cacheDirSet returns the set of project slugs that already have a graphcache
// directory, used to match a cwd against a real project.
func (h *toolHandler) cacheDirSet() map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(filepath.Join(h.dataDir, "graphcache"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() {
			out[e.Name()] = true
		}
	}
	return out
}
