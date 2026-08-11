package claudedata

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListProjects returns all projects under ~/.claude/projects, newest first.
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
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			count++
			if info, err := f.Info(); err == nil {
				if ms := info.ModTime().UnixMilli(); ms > newest {
					newest = ms
				}
			}
		}
		if count == 0 {
			continue
		}
		out = append(out, Project{
			Slug:         e.Name(),
			Path:         decodeSlug(e.Name()),
			SessionCount: count,
			UpdatedAt:    newest,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// decodeSlug turns a project slug back into a readable path. The encoding is
// lossy (both '/' and '-' map to '-'), so this is best-effort for display.
func decodeSlug(slug string) string {
	s := strings.TrimPrefix(slug, "-")
	return "/" + strings.ReplaceAll(s, "-", "/")
}
