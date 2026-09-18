package claudedata

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// codexRoot returns the Codex data directory.
// Respects CODEX_HOME env var; defaults to ~/.codex.
func codexRoot() (string, error) {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return dir, nil
	}
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
	ID        string `json:"id"`
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
// SessionMeta, newest first. scope selects the reach (see scope.go): a namespace
// name, ScopeUnassigned, or ScopeAll.
func ListCodexSessions(scope string) ([]SessionMeta, error) {
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

	scope = NormalizeScope(scope)
	probeNamespace := ""
	if IsNamespaceScope(scope) {
		probeNamespace = scope
	}

	// When a single namespace is requested, reject cheaply first: the leading
	// session_meta record carries the cwd, so a session belonging to another
	// namespace never pays for a full parse. Survivors are parsed in parallel.
	candidates := files
	if probeNamespace != "" {
		candidates = make([]string, 0, len(files))
		for _, f := range files {
			if cwd := probeCodexCwd(f); cwd != "" && ResolveNamespace(cwd) != probeNamespace {
				continue
			}
			candidates = append(candidates, f)
		}
	}

	var out []SessionMeta
	for _, meta := range scanFiles(candidates, scanCodexMeta) {
		if meta.ID == "" || !MatchesScope(meta, scope) {
			continue
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// codexProbeLines bounds probeCodexCwd. Codex writes session_meta as the first
// record, so this is normally a single-line read.
const codexProbeLines = 20

// probeCodexCwd returns the working directory recorded in the session_meta
// record, or "" when no such record appears near the head of the file.
//
// This is the cheap half of scope filtering: it reads the first record only, and
// its answer is used to *reject* a transcript before parsing. That makes it
// coupled to parseCodexMeta, which takes the cwd from the newest record — the two
// agree only while a transcript keeps one cwd throughout. On a real 452-file
// store no file ever changed cwd across its session_meta records, so the coupling
// is safe today; if Codex ever starts recording a relocated cwd mid-file, the
// rejection here has to be relaxed (return "" when a second session_meta is seen)
// rather than left to silently drop sessions from a namespace listing.
func probeCodexCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for n := 0; n < codexProbeLines && sc.Scan(); n++ {
		var entry codexSessionEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		if entry.Type != "session_meta" {
			continue
		}
		var meta codexSessionMeta
		if json.Unmarshal(entry.Payload, &meta) == nil {
			return meta.Cwd
		}
	}
	return ""
}

// scanCodexMeta reads a Codex session file to extract metadata. The parse is
// cached by path + mtime + size: listing the browser re-reads every session in
// the Codex store, which is the slowest part of opening the page.
func scanCodexMeta(path string) SessionMeta {
	_, mtime, size, ok := statForScan(path)
	if !ok {
		return SessionMeta{}
	}
	const kind = "codex|"
	if m, hit := lookupScan(kind+path, mtime, size); hit {
		return newCodexMeta(path, mtime, size, m)
	}
	m := parseCodexMeta(path)
	storeScan(kind+path, mtime, size, m)
	return newCodexMeta(path, mtime, size, m)
}

func newCodexMeta(path string, mtime, size int64, m sessionBrowserMeta) SessionMeta {
	slug := ResolveNamespace(m.Cwd)
	return SessionMeta{
		ID:           "codex:" + m.ID,
		ProjectSlug:  slug,
		Title:        m.Title,
		MessageCount: m.Msgs,
		UpdatedAt:    mtime,
		SizeBytes:    size,
		Cwd:          m.Cwd,
		Model:        "codex",
		File:         path,
		Namespace:    slug,
	}
}

// parseCodexMeta is the expensive half: it walks the transcript to collect the
// session id, working directory, a title and a message count.
//
// Identity comes from the FIRST session_meta record only. Codex appends another
// session_meta whenever a conversation is compacted or forked, and those later
// records carry an ANCESTOR's session id — so letting each one overwrite the id
// made dozens of unrelated files claim one identity (54 files reported a single
// older session's id on this machine, yielding 55 list entries that all opened
// the same transcript, and re-distilling that session once per duplicate). The
// first record always matches the file's own name, so it is the real identity.
// The cwd is the opposite: taken from the newest record that has one, because
// that is where the session is currently anchored.
func parseCodexMeta(path string) sessionBrowserMeta {
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
		var entry codexSessionEntry
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		switch entry.Type {
		case "session_meta":
			var meta codexSessionMeta
			_ = json.Unmarshal(entry.Payload, &meta)
			if sessionID == "" {
				sessionID = meta.ID
				if sessionID == "" {
					sessionID = meta.SessionID
				}
			}
			if meta.Cwd != "" {
				cwd = meta.Cwd
			}
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
		sessionID = codexIDFromFileName(filepath.Base(path))
	}
	return sessionBrowserMeta{ID: sessionID, Title: title, Msgs: msgs, Cwd: cwd}
}

// codexIDFromFileName recovers a session id from a transcript file name when the
// session_meta record is missing or lacks one. Rollout names look like
// "rollout-2026-08-27T00-27-<uuid>"; the id is the trailing UUID, not the
// dash-split remainder (which used to yield junk ids such as
// "00-27-019e8b6c-…" that then keyed the knowledge cache).
func codexIDFromFileName(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	if m := uuidTail.FindString(name); m != "" {
		return m
	}
	return name
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

// ResolveNamespace classifies a session by the working directory it recorded:
// the namespace of the nearest .axon-project marker at or above that directory,
// or — for a workspace root that is not marked itself but whose immediate
// subdirectories are marked repositories (the "~/projects/<repo>" layout) — the
// single namespace those children share.
//
// A cwd whose marked children disagree stays unresolved instead of guessing, and
// so does a cwd with no markers anywhere nearby; callers report those sessions
// as unmanaged rather than filing their knowledge under the wrong namespace.
func ResolveNamespace(cwd string) string {
	if cwd == "" {
		return ""
	}
	if ns := resolveProjectFromCwd(cwd); ns != "" {
		return ns
	}
	return namespaceFromChildren(cwd)
}

// childNamespaceCache memoises namespaceFromChildren. Only non-empty results are
// cached: a negative answer must not stick, because the user may drop a marker
// into the tree later and re-index without restarting the app.
var childNamespaceCache = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func namespaceFromChildren(cwd string) string {
	// A home directory is not a project container. It routinely has one marked
	// child (a checkout living next to Desktop/Documents), and treating it as a
	// workspace root would quietly file every scratch session in ~ under that
	// project's namespace.
	if home, err := os.UserHomeDir(); err == nil && home != "" && filepath.Clean(cwd) == filepath.Clean(home) {
		return ""
	}

	entries, err := os.ReadDir(cwd)
	if err != nil {
		return ""
	}

	childNamespaceCache.Lock()
	if ns, ok := childNamespaceCache.m[cwd]; ok {
		childNamespaceCache.Unlock()
		return ns
	}
	childNamespaceCache.Unlock()

	found := ""
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		ns := readMark(filepath.Join(cwd, e.Name()))
		if ns == "" {
			continue
		}
		if found == "" {
			found = ns
			continue
		}
		if found != ns {
			return "" // ambiguous: several namespaces live under this root
		}
	}
	if found != "" {
		childNamespaceCache.Lock()
		childNamespaceCache.m[cwd] = found
		childNamespaceCache.Unlock()
	}
	return found
}

// resolveProjectFromCwd walks up from a directory looking for .axon-project.
func resolveProjectFromCwd(cwd string) string {
	if cwd == "" {
		return ""
	}
	for dir := cwd; ; {
		if ns := readMark(dir); ns != "" {
			return ns
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// readMark returns the first non-empty line of dir/.axon-project, or "".
func readMark(dir string) string {
	content, err := os.ReadFile(filepath.Join(dir, ".axon-project"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(content), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// FindCodexSessionFile locates the .jsonl file for a Codex session ID. The
// answer is memoised (see locate.go): the walk below covers the whole session
// store, and the session browser resolves a file on every open.
func FindCodexSessionFile(sessionID string) string {
	if p := locateCached(locateKindCodex, sessionID); p != "" {
		return p
	}
	root, err := codexRoot()
	if err != nil {
		return ""
	}
	// The live tree is searched first, so an active session wins over an
	// archived copy of the same id; archived sessions are the fallback.
	found := findSessionFileUnder(sessionID,
		filepath.Join(root, "sessions"),
		filepath.Join(root, "archived_sessions"),
	)
	rememberLocate(locateKindCodex, sessionID, found)
	return found
}
