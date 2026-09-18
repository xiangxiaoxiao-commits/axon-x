package claudedata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProjects returns all projects under ~/.claude/projects, newest first.
//
// Each entry carries the namespace its work belongs to (resolved from the cwd
// recorded inside the newest transcript) and the real working directory when one
// is known — the slug's own encoding is lossy, and on Windows it renders as a
// nonsense "/C/Users/…" path, so the recorded cwd is preferred for display and
// for anyone treating Path as a repository root.
func ListProjects() ([]Project, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Project{}, nil
		}
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	out := make([]Project, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pdir := filepath.Join(dir, e.Name())
		files, _ := os.ReadDir(pdir)
		count := 0
		var newest int64
		newestFile := ""
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			count++
			if info, err := f.Info(); err == nil {
				if ms := info.ModTime().UnixMilli(); ms >= newest {
					newest, newestFile = ms, filepath.Join(pdir, f.Name())
				}
			}
		}
		if count == 0 {
			continue
		}
		path := decodeSlug(e.Name())
		namespace := ""
		if newestFile != "" {
			if cwd := probeCwd(newestFile); cwd != "" {
				path = cwd
				namespace = ResolveNamespace(cwd)
			}
		}
		out = append(out, Project{
			Slug:         e.Name(),
			Path:         path,
			Namespace:    namespace,
			SessionCount: count,
			UpdatedAt:    newest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}
