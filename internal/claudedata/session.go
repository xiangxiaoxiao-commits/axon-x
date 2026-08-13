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

// rawEvent is the subset of a jsonl line we care about. Claude Code emits the
// AI-generated title on an "ai-title" event under the camelCase key "aiTitle"
// (and mirrors the user's latest prompt on "last-prompt" events as
// "lastPrompt"); the older lower "title" key is gone, so read the current keys —
// otherwise every session renders as "(无标题)".
type rawEvent struct {
	Type       string          `json:"type"`
	AITitle    string          `json:"aiTitle"`    // ai-title events
	LastPrompt string          `json:"lastPrompt"` // last-prompt events (title fallback)
	Cwd        string          `json:"cwd"`        // working dir, present on most events
	Message    json.RawMessage `json:"message"`    // user/assistant events
}

// rawMessage is the subset of an assistant event's "message" object we read: the
// model id that produced the reply.
type rawMessage struct {
	Model string `json:"model"`
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
		title, msgs, cwd, model := scanTitleAndCount(filepath.Join(dir, f.Name()))
		out = append(out, SessionMeta{
			ID:           id,
			ProjectSlug:  projectSlug,
			Title:        title,
			MessageCount: msgs,
			UpdatedAt:    info.ModTime().UnixMilli(),
			SizeBytes:    info.Size(),
			Cwd:          cwd,
			Model:        model,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// scanTitleAndCount streams a session file to pull the latest ai-title, count
// user/assistant turns, capture the working directory, and note the last real
// model that answered — all without loading the whole file into memory.
func scanTitleAndCount(path string) (title string, count int, cwd, model string) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, "", ""
	}
	defer f.Close()

	var lastPrompt string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev rawEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if cwd == "" && ev.Cwd != "" {
			cwd = ev.Cwd
		}
		switch ev.Type {
		case "ai-title":
			if ev.AITitle != "" {
				title = ev.AITitle
			}
		case "last-prompt":
			if ev.LastPrompt != "" {
				lastPrompt = ev.LastPrompt
			}
		case "user":
			count++
		case "assistant":
			count++
			// Track the last non-synthetic model that produced a reply. Synthetic
			// events (e.g. "<synthetic>") aren't a real model choice, so skip them.
			if len(ev.Message) > 0 {
				var m rawMessage
				if json.Unmarshal(ev.Message, &m) == nil && m.Model != "" && m.Model != "<synthetic>" {
					model = m.Model
				}
			}
		}
	}
	// Fall back to the latest user prompt (first line, trimmed) when no AI title
	// was generated yet, so short/new sessions still show something meaningful.
	if title == "" {
		title = firstLine(lastPrompt, 60)
	}
	return title, count, cwd, model
}

// firstLine returns the first non-empty line of s, trimmed and capped at max
// runes (with an ellipsis when truncated). Empty in, empty out.
func firstLine(s string, max int) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > max {
			return string(r[:max]) + "…"
		}
		return line
	}
	return ""
}
