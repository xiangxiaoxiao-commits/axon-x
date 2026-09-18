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
	Model   string          `json:"model"`
	Content json.RawMessage `json:"content"`
}

// cwdProbeLines bounds how far probeCwd reads before giving up. Claude Code
// records "cwd" on the first handful of events (line ~4 in practice), so a small
// budget classifies a transcript without loading megabytes of conversation.
const cwdProbeLines = 40

// ListSessions returns session summaries for a project, newest first.
func ListSessions(projectSlug string) ([]SessionMeta, error) {
	return listSessions(projectSlug, false)
}

// ListAllSessions returns every Claude Code transcript in a project, including
// nested subagent transcripts. It is intended for the session browser; graph
// indexing deliberately keeps using ListSessions so subagent work is not
// distilled a second time alongside its parent session.
func ListAllSessions(projectSlug string) ([]SessionMeta, error) {
	return listSessions(projectSlug, true)
}

func listSessions(projectSlug string, recursive bool) ([]SessionMeta, error) {
	dir, err := projectDir(projectSlug)
	if err != nil {
		return nil, err
	}
	files, err := sessionFiles(dir, recursive)
	if err != nil {
		return nil, fmt.Errorf("read project dir: %w", err)
	}

	out := make([]SessionMeta, 0, len(files))
	for _, path := range files {
		out = append(out, scanClaudeMeta(path, projectSlug))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// ListSessionsForNamespace returns the top-level transcripts of every Claude
// Code project whose recorded working directory belongs to `namespace` — i.e.
// a .axon-project marker sits in that directory or one of its ancestors.
//
// This is the link knowledge indexing needs. A namespace is a product-level
// concept ("which body of work does this knowledge belong to") while Claude
// Code's own directories are path-encoded ("-Users-me-projects-mono-glite"), so
// matching a namespace against directory names cannot work: the only reliable
// bridge is the cwd each transcript records for itself, the same bridge the
// Codex and Buddy scanners already use.
//
// Nested subagent transcripts are excluded unless recursive is set, matching
// ListSessions so parent and subagent work is not distilled twice.
func ListSessionsForNamespace(namespace string, recursive bool) ([]SessionMeta, error) {
	if namespace == "" {
		return nil, nil
	}
	projects, err := ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]SessionMeta, 0)
	for _, p := range projects {
		dir, err := projectDir(p.Slug)
		if err != nil {
			continue
		}
		files, err := sessionFiles(dir, recursive)
		if err != nil {
			continue
		}
		for _, path := range files {
			// Cheap reject first: a transcript whose recorded cwd already points at
			// another namespace never pays for a full scan. When the probe finds no
			// cwd at all we fall through and let the full scan decide, so an unusual
			// transcript is never silently dropped.
			if cwd := probeCwd(path); cwd != "" && ResolveNamespace(cwd) != namespace {
				continue
			}
			meta := scanClaudeMeta(path, p.Slug)
			if meta.Namespace != namespace {
				continue
			}
			out = append(out, meta)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// HasProject reports whether ~/.claude/projects/<slug> exists, i.e. whether the
// value names a Claude Code project directory rather than a namespace.
func HasProject(slug string) bool {
	if slug == "" {
		return false
	}
	dir, err := projectDir(slug)
	if err != nil {
		return false
	}
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// ProjectNamespace resolves the .axon-project namespace of a Claude Code project
// directory by reading the cwd of its newest transcript. Empty when the
// directory has no sessions or none of them ran inside a marked project.
func ProjectNamespace(slug string) string {
	dir, err := projectDir(slug)
	if err != nil {
		return ""
	}
	files, err := sessionFiles(dir, false)
	if err != nil || len(files) == 0 {
		return ""
	}
	newest, newestMod := "", int64(-1)
	for _, f := range files {
		if info, err := os.Stat(f); err == nil && info.ModTime().UnixMilli() > newestMod {
			newest, newestMod = f, info.ModTime().UnixMilli()
		}
	}
	if newest == "" {
		return ""
	}
	return ResolveNamespace(probeCwd(newest))
}

// projectDir resolves ~/.claude/projects/<slug>. filepath.Base guards against a
// caller passing a path instead of a bare slug.
func projectDir(projectSlug string) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "projects", filepath.Base(filepath.Clean(projectSlug))), nil
}

// scanClaudeMeta builds a SessionMeta for one transcript, resolving the
// namespace from the cwd the transcript records.
func scanClaudeMeta(path, projectSlug string) SessionMeta {
	var meta SessionMeta
	info, err := os.Stat(path)
	if err != nil {
		return meta
	}
	title, msgs, cwd, model := scanTitleAndCount(path)
	return SessionMeta{
		ID:           strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		ProjectSlug:  projectSlug,
		Title:        title,
		MessageCount: msgs,
		UpdatedAt:    info.ModTime().UnixMilli(),
		SizeBytes:    info.Size(),
		Cwd:          cwd,
		Model:        model,
		File:         path,
		Namespace:    ResolveNamespace(cwd),
	}
}

func sessionFiles(dir string, recursive bool) ([]string, error) {
	if recursive {
		var files []string
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".jsonl") {
				files = append(files, path)
			}
			return nil
		})
		return files, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jsonl") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

// probeCwd reads only the head of a transcript to recover its working
// directory. Cheap enough to run over every session on disk (a few lines each)
// before deciding which ones need a full scan.
func probeCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16*1024), 1<<20)
	for n := 0; n < cwdProbeLines && sc.Scan(); n++ {
		var ev rawEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Cwd != "" {
			return ev.Cwd
		}
	}
	return ""
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

	var firstPrompt, lastPrompt string
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
			if firstPrompt == "" && len(ev.Message) > 0 {
				var m rawMessage
				if json.Unmarshal(ev.Message, &m) == nil {
					firstPrompt = extractText(m.Content)
				}
			}
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
	if title == "" {
		title = firstLine(firstPrompt, 60)
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
