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
// scope selects the reach (see scope.go): a namespace name, ScopeUnassigned, or
// ScopeAll.
func ListBuddySessions(scope string) ([]SessionMeta, error) {
	return listBuddySessions(scope, false)
}

// ListAllBuddySessions includes nested subagent transcripts for the session
// browser. Knowledge indexing continues to use ListBuddySessions so parent and
// subagent content are not both distilled.
func ListAllBuddySessions(scope string) ([]SessionMeta, error) {
	return listBuddySessions(scope, true)
}

func listBuddySessions(scope string, recursive bool) ([]SessionMeta, error) {
	var all []SessionMeta
	for _, agent := range buddyAgents {
		sessions, err := listBuddyAgentSessions(agent, scope, recursive)
		if err != nil {
			continue // agent not installed, skip
		}
		all = append(all, sessions...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt > all[j].UpdatedAt })
	return all, nil
}

func listBuddyAgentSessions(agent buddyAgent, scope string, recursive bool) ([]SessionMeta, error) {
	root, err := buddyRoot(agent)
	if err != nil {
		return nil, err
	}
	projDir := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		return nil, err
	}

	scope = NormalizeScope(scope)
	probeNamespace := ""
	if IsNamespaceScope(scope) {
		probeNamespace = scope
	}

	var out []SessionMeta
	for _, projEntry := range entries {
		if !projEntry.IsDir() {
			continue
		}
		pdir := filepath.Join(projDir, projEntry.Name())
		files, _ := sessionFiles(pdir, recursive)
		// Cheap reject before the full parse, same as the Claude and Codex
		// scanners: a session from another namespace should cost one small read,
		// not a full transcript parse. Survivors are parsed in parallel.
		candidates := files
		if probeNamespace != "" {
			candidates = make([]string, 0, len(files))
			for _, f := range files {
				if cwd := probeBuddyCwd(f); cwd != "" && ResolveNamespace(cwd) != probeNamespace {
					continue
				}
				candidates = append(candidates, f)
			}
		}
		for _, meta := range scanFiles(candidates, func(p string) SessionMeta {
			return scanBuddyMeta(p, agent)
		}) {
			if meta.ID == "" || !MatchesScope(meta, scope) {
				continue
			}
			out = append(out, meta)
		}
	}
	return out, nil
}

// buddyProbeLines bounds probeBuddyCwd.
const buddyProbeLines = 40

// probeBuddyCwd returns the working directory recorded on a buddy session's
// early records, or "" when none appears near the head of the file.
func probeBuddyCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for n := 0; n < buddyProbeLines && sc.Scan(); n++ {
		var entry buddyEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if entry.Cwd != "" {
			return entry.Cwd
		}
	}
	return ""
}

// scanBuddyMeta returns a session's metadata. The parse is cached by
// path + mtime + size; only a fresh (or changed) file is read.
func scanBuddyMeta(path string, agent buddyAgent) SessionMeta {
	_, mtime, size, ok := statForScan(path)
	if !ok {
		return SessionMeta{}
	}
	const kind = "buddy|"
	if m, hit := lookupScan(kind+path, mtime, size); hit {
		return newBuddyMeta(path, agent, mtime, size, m)
	}
	m := parseBuddyMeta(path)
	storeScan(kind+path, mtime, size, m)
	return newBuddyMeta(path, agent, mtime, size, m)
}

func newBuddyMeta(path string, agent buddyAgent, mtime, size int64, m sessionBrowserMeta) SessionMeta {
	id := m.ID
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(path), ".jsonl")
	}
	slug := ResolveNamespace(m.Cwd)
	return SessionMeta{
		ID:           agent.Name + ":" + id,
		ProjectSlug:  slug,
		Title:        m.Title,
		MessageCount: m.Msgs,
		UpdatedAt:    mtime,
		SizeBytes:    size,
		Cwd:          m.Cwd,
		Model:        agent.Name,
		File:         path,
		Namespace:    slug,
	}
}

// parseBuddyMeta is the expensive half of scanBuddyMeta.
func parseBuddyMeta(path string) sessionBrowserMeta {
	f, err := os.Open(path)
	if err != nil {
		return sessionBrowserMeta{}
	}
	defer f.Close()

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

	return sessionBrowserMeta{ID: sessionID, Title: title, Msgs: msgs, Cwd: cwd}
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

// FindBuddySessionFile locates the .jsonl file for a buddy session ID. The
// answer is memoised (see locate.go) so repeated opens stop re-walking every
// agent's project tree.
func FindBuddySessionFile(sessionID string) string {
	if p := locateCached(locateKindBuddy, sessionID); p != "" {
		return p
	}
	var roots []string
	for _, agent := range buddyAgents {
		root, err := buddyRoot(agent)
		if err != nil {
			continue
		}
		roots = append(roots, filepath.Join(root, "projects"))
	}
	found := findSessionFileUnder(sessionID, roots...)
	rememberLocate(locateKindBuddy, sessionID, found)
	return found
}
