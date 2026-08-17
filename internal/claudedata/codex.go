package claudedata

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// codexRoot returns the ~/.codex directory.
func codexRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// codexSessionEntry is one line in a Codex .jsonl session file.
type codexSessionEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// codexSessionMeta is the payload of a session_meta entry.
type codexSessionMeta struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// codexResponseItem is the payload of a response_item entry.
type codexResponseItem struct {
	Role    string              `json:"role"`
	Type    string              `json:"type"`
	Content []codexContentBlock `json:"content"`
}

// codexContentBlock is one element of the content array.
type codexContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ListCodexSessions returns all Codex sessions (active + archived) as
// SessionMeta, newest first. If projectSlug is non-empty, only sessions whose
// cwd resolves (via .axon-project) to that slug are returned.
func ListCodexSessions(projectSlug string) ([]SessionMeta, error) {
	root, err := codexRoot()
	if err != nil {
		return nil, err
	}

	var files []string
	// Active sessions: ~/.codex/sessions/YYYY/MM/DD/*.jsonl
	_ = filepath.Walk(filepath.Join(root, "sessions"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	// Archived sessions: ~/.codex/archived_sessions/*.jsonl
	entries, _ := os.ReadDir(filepath.Join(root, "archived_sessions"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, filepath.Join(root, "archived_sessions", e.Name()))
		}
	}

	var out []SessionMeta
	for _, f := range files {
		meta := scanCodexMeta(f)
		if meta.ID == "" {
			continue
		}
		if projectSlug != "" && meta.ProjectSlug != projectSlug {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// scanCodexMeta reads a Codex session file to extract metadata.
func scanCodexMeta(path string) SessionMeta {
	f, err := os.Open(path)
	if err != nil {
		return SessionMeta{}
	}
	defer f.Close()

	info, _ := f.Stat()
	var mtime, size int64
	if info != nil {
		mtime = info.ModTime().UnixMilli()
		size = info.Size()
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var sessionID, cwd, title string
	msgs := 0
	for sc.Scan() {
		var entry codexSessionEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		switch entry.Type {
		case "session_meta":
			var meta codexSessionMeta
			_ = json.Unmarshal(entry.Payload, &meta)
			sessionID = meta.SessionID
			cwd = meta.Cwd
		case "response_item":
			msgs++
			if title == "" {
				var item codexResponseItem
				_ = json.Unmarshal(entry.Payload, &item)
				if item.Role == "user" {
					for _, c := range item.Content {
						if c.Type == "input_text" && c.Text != "" {
							text := c.Text
							if idx := strings.Index(text, "</environment_context>"); idx >= 0 {
								text = strings.TrimSpace(text[idx+len("</environment_context>"):])
							}
							if len(text) > 80 {
								text = text[:80]
							}
							title = text
							break
						}
					}
				}
			}
		}
	}

	if sessionID == "" {
		base := filepath.Base(path)
		sessionID = strings.TrimSuffix(base, ".jsonl")
		if strings.HasPrefix(sessionID, "rollout-") {
			parts := strings.SplitN(sessionID, "-", 5)
			if len(parts) >= 5 {
				sessionID = parts[4]
			}
		}
	}

	slug := resolveProjectFromCwd(cwd)

	return SessionMeta{
		ID:           "codex:" + sessionID,
		ProjectSlug:  slug,
		Title:        title,
		MessageCount: msgs,
		UpdatedAt:    mtime,
		SizeBytes:    size,
		Cwd:          cwd,
		Model:        "codex",
	}
}

// ReadCodexSession loads a Codex session as ordered messages.
func ReadCodexSession(sessionFile string) ([]SessionMessage, error) {
	f, err := os.Open(sessionFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var msgs []SessionMessage
	for sc.Scan() {
		var entry codexSessionEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "response_item" {
			continue
		}
		var item codexResponseItem
		if json.Unmarshal(entry.Payload, &item) != nil {
			continue
		}
		if item.Role != "user" && item.Role != "assistant" {
			continue
		}

		var text strings.Builder
		for _, c := range item.Content {
			switch c.Type {
			case "input_text", "output_text":
				if c.Text != "" {
					text.WriteString(c.Text)
					text.WriteString("\n")
				}
			}
		}
		if text.Len() == 0 {
			continue
		}
		msgs = append(msgs, SessionMessage{Role: item.Role, Text: text.String()})
	}
	return msgs, nil
}

// resolveProjectFromCwd walks up from a directory looking for .axon-project.
func resolveProjectFromCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	for dir := cwd; ; {
		content, err := os.ReadFile(filepath.Join(dir, ".axon-project"))
		if err == nil {
			for _, line := range strings.Split(string(content), "\n") {
				if s := strings.TrimSpace(line); s != "" {
					return s
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// FindCodexSessionFile locates the .jsonl file for a Codex session ID.
func FindCodexSessionFile(sessionID string) string {
	root, err := codexRoot()
	if err != nil {
		return ""
	}

	var found string
	_ = filepath.Walk(filepath.Join(root, "sessions"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.Contains(filepath.Base(path), sessionID) {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found != "" {
		return found
	}

	entries, _ := os.ReadDir(filepath.Join(root, "archived_sessions"))
	for _, e := range entries {
		if strings.Contains(e.Name(), sessionID) {
			return filepath.Join(root, "archived_sessions", e.Name())
		}
	}
	return ""
}
