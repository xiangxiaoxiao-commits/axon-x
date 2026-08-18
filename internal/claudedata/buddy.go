package claudedata

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// buddyAgent identifies which buddy-style agent a session belongs to.
type buddyAgent struct {
	Name    string // "workbuddy" or "codebuddy"
	DataDir string // ".workbuddy" or ".codebuddy"
}

var buddyAgents = []buddyAgent{
	{Name: "workbuddy", DataDir: ".workbuddy"},
	{Name: "codebuddy", DataDir: ".codebuddy"},
}

// buddyRoot returns the data directory for a buddy agent.
func buddyRoot(agent buddyAgent) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, agent.DataDir), nil
}

// buddyEntry is one line in a buddy .jsonl session file.
type buddyEntry struct {
	ID        string          `json:"id"`
	Timestamp json.RawMessage `json:"timestamp"` // can be string or number
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"sessionId"`
}

// buddyContentBlock is one element of the content array.
type buddyContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ListBuddySessions returns all WorkBuddy + CodeBuddy sessions as SessionMeta.
// If projectSlug is non-empty, filters by .axon-project resolution.
func ListBuddySessions(projectSlug string) ([]SessionMeta, error) {
	var all []SessionMeta
	for _, agent := range buddyAgents {
		sessions, err := listBuddyAgentSessions(agent, projectSlug)
		if err != nil {
			continue // agent not installed, skip
		}
		all = append(all, sessions...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt > all[j].UpdatedAt })
	return all, nil
}

func listBuddyAgentSessions(agent buddyAgent, projectSlug string) ([]SessionMeta, error) {
	root, err := buddyRoot(agent)
	if err != nil {
		return nil, err
	}
	projDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return nil, err
	}

	var out []SessionMeta
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		pdir := filepath.Join(projDir, projEntry.Name())
		files, _ := os.ReadDir(pdir)
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			fpath := filepath.Join(pdir, f.Name())
			meta := scanBuddyMeta(fpath, agent)
			if meta.ID == "" {
				continue
			}
			if projectSlug != "" && meta.ProjectSlug != projectSlug {
				continue
			}
			out = append(out, meta)
		}
	}
	return out, nil
}

// scanBuddyMeta reads the first few lines to extract metadata.
func scanBuddyMeta(path string, agent buddyAgent) SessionMeta {
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
		var entry buddyEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}
		msgs++
		if cwd == "" && entry.Cwd != "" {
			cwd = entry.Cwd
		}
		if sessionID == "" && entry.SessionID != "" {
			sessionID = entry.SessionID
		}
		if title == "" && entry.Role == "user" {
			title = extractBuddyText(entry.Content, 80)
		}
	}

	if sessionID == "" {
		sessionID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}

	slug := resolveProjectFromCwd(cwd)

	return SessionMeta{
		ID:           agent.Name + ":" + sessionID,
		ProjectSlug:  slug,
		Title:        title,
		MessageCount: msgs,
		UpdatedAt:    mtime,
		SizeBytes:    size,
		Cwd:          cwd,
		Model:        agent.Name,
	}
}

// ReadBuddySession loads a buddy session as ordered messages.
func ReadBuddySession(sessionFile string) ([]SessionMessage, error) {
	f, err := os.Open(sessionFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var msgs []SessionMessage
	for sc.Scan() {
		var entry buddyEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "message" {
			continue
		}
		if entry.Role != "user" && entry.Role != "assistant" {
			continue
		}
		text := extractBuddyText(entry.Content, 0)
		if text == "" {
			continue
		}
		msgs = append(msgs, SessionMessage{Role: entry.Role, Text: text})
	}
	return msgs, nil
}

// extractBuddyText parses the content array and returns concatenated text.
// If maxLen > 0, truncates. Handles both array-of-blocks and plain string.
func extractBuddyText(raw json.RawMessage, maxLen int) string {
	if len(raw) == 0 {
		return ""
	}
	// Try as array of blocks first.
	var blocks []buddyContentBlock
	if json.Unmarshal(raw, &blocks) == nil && len(blocks) > 0 {
		var b strings.Builder
		for _, blk := range blocks {
			if (blk.Type == "input_text" || blk.Type == "output_text" || blk.Type == "text") && blk.Text != "" {
				b.WriteString(blk.Text)
				b.WriteString("\n")
			}
		}
		text := strings.TrimSpace(b.String())
		// Strip environment_context / system-reminder / user_query XML wrappers.
		if idx := strings.Index(text, "</system-reminder>"); idx >= 0 {
			text = strings.TrimSpace(text[idx+len("</system-reminder>"):])
		}
		if idx := strings.Index(text, "</environment_context>"); idx >= 0 {
			text = strings.TrimSpace(text[idx+len("</environment_context>"):])
		}
		// Extract content from <user_query> tags if present.
		if start := strings.Index(text, "<user_query>"); start >= 0 {
			inner := text[start+len("<user_query>"):]
			if end := strings.Index(inner, "</user_query>"); end >= 0 {
				text = strings.TrimSpace(inner[:end])
			} else {
				text = strings.TrimSpace(inner)
			}
		}
		if maxLen > 0 && len(text) > maxLen {
			text = text[:maxLen]
		}
		return text
	}
	// Try as plain string.
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if maxLen > 0 && len(s) > maxLen {
			s = s[:maxLen]
		}
		return s
	}
	return ""
}

// FindBuddySessionFile locates the .jsonl file for a buddy session ID.
func FindBuddySessionFile(sessionID string) string {
	for _, agent := range buddyAgents {
		root, err := buddyRoot(agent)
		if err != nil {
			continue
		}
		projDir := filepath.Join(root, "projects")
		var found string
		_ = filepath.Walk(projDir, func(path string, info os.FileInfo, err error) error {
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
	}
	return ""
}
