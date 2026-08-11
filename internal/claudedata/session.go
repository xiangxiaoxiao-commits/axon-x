package claudedata

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rawEvent is the subset of a jsonl line we care about.
type rawEvent struct {
	Type    string          `json:"type"`
	Title   string          `json:"title"`   // ai-title events
	Message json.RawMessage `json:"message"` // user/assistant events
}

// ListSessions returns session summaries for a project, newest first.
func ListSessions(projectSlug string) ([]SessionMeta, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "projects", filepath.Base(projectSlug))
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read project dir: %w", err)
	}

	out := make([]SessionMeta, 0)
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".jsonl") {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(f.Name(), ".jsonl")
		title, msgs := scanTitleAndCount(filepath.Join(dir, f.Name()))
		out = append(out, SessionMeta{
			ID:           id,
			ProjectSlug:  projectSlug,
			Title:        title,
			MessageCount: msgs,
			UpdatedAt:    info.ModTime().UnixMilli(),
			SizeBytes:    info.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// scanTitleAndCount streams a session file to pull the latest ai-title and
// count user/assistant turns, without loading the whole file into memory.
func scanTitleAndCount(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	title := ""
	count := 0
	for sc.Scan() {
		var ev rawEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "ai-title":
			if ev.Title != "" {
				title = ev.Title
			}
		case "user", "assistant":
			count++
		}
	}
	return title, count
}
