package claudedata

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MemoryFile is a human-editable memory/instruction file Claude Code loads.
type MemoryFile struct {
	Scope   string `json:"scope"` // "instructions" (CLAUDE.md) | "memory" (memory/*.md)
	Name    string `json:"name"`
	Path    string `json:"path"` // absolute
	Content string `json:"content"`
}

// ListMemory returns the global CLAUDE.md plus, for a project, its memory/*.md
// files. Pass an empty slug for just the global instructions.
func ListMemory(projectSlug string) ([]MemoryFile, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	out := make([]MemoryFile, 0)

	// Global instructions.
	gp := filepath.Join(root, "CLAUDE.md")
	if data, err := os.ReadFile(gp); err == nil {
		out = append(out, MemoryFile{Scope: "instructions", Name: "CLAUDE.md (全局规范)", Path: gp, Content: string(data)})
	}

	// Per-project memory files.
	if projectSlug != "" {
		mdir := filepath.Join(root, "projects", filepath.Base(projectSlug), "memory")
		entries, _ := os.ReadDir(mdir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			p := filepath.Join(mdir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			out = append(out, MemoryFile{Scope: "memory", Name: e.Name(), Path: p, Content: string(data)})
		}
	}
	return out, nil
}

// WriteMemory writes content to a memory file. The path must live inside
// ~/.claude (guards against path traversal from the UI).
func WriteMemory(path, content string) error {
	if err := ensureInsideRoot(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	return nil
}

// DeleteMemory removes a memory file (must be inside ~/.claude).
func DeleteMemory(path string) error {
	if err := ensureInsideRoot(path); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// ensureInsideRoot rejects paths that escape ~/.claude.
func ensureInsideRoot(path string) error {
	root, err := Root()
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return fmt.Errorf("refusing to touch path outside ~/.claude: %s", path)
	}
	return nil
}
