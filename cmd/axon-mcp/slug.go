package main

import (
	"os"
	"path/filepath"
	"strings"
)

// This file resolves a project namespace for MCP tool calls.
//
// The new model is explicit naming: each project directory contains an
// `.axon-project` file whose content is the namespace name (e.g. "gaia",
// "glite", "axon"). The server walks cwd upward looking for this file. If
// project is passed explicitly it always wins; if not found anywhere, the call
// errors out asking the user to create .axon-project or pass project explicitly.

// projectFileName is the marker file placed in a project root to declare its
// knowledge-graph namespace.
const projectFileName = ".axon-project"

// slugSource tags where a resolved slug came from, so callers can tailor the
// "nothing found" message.
type slugSource int

const (
	slugExplicit slugSource = iota // caller passed project explicitly
	slugMapped                     // resolved from .axon-project file in cwd or ancestor
	slugUnknown                    // no explicit slug and no .axon-project found
)

// resolveSlug picks the project namespace for a call.
//
//  1. An explicit non-empty arg always wins (backward compatible).
//  2. Walk cwd upward looking for .axon-project; its trimmed first line is the
//     namespace name.
//  3. Nothing found → return ("", slugUnknown) so the caller can report a clear
//     error.
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
	// Walk upward looking for .axon-project.
	for dir := wd; ; {
		content, err := os.ReadFile(filepath.Join(dir, projectFileName))
		if err == nil {
			if ns := parseProjectFile(content); ns != "" {
				return ns, slugMapped
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return "", slugUnknown
}

// parseProjectFile extracts the namespace from an .axon-project file: the
// trimmed first non-empty line. Returns "" if the file is empty or blank.
func parseProjectFile(content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// namespaceDirs returns the set of namespace names that already have a
// graphcache directory (i.e. queryable projects).
func (h *toolHandler) namespaceDirs() []string {
	entries, err := os.ReadDir(filepath.Join(h.dataDir, "graphcache"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}
