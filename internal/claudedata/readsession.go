package claudedata

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// SessionMessage is one displayable turn from a session transcript.
type SessionMessage struct {
	Role string `json:"role"` // "user" | "assistant"
	Text string `json:"text"`
}

// SessionProgress is the "where did I leave off" snapshot of a session: the last
// real user prompt and the tail of the last assistant reply. It lets you decide
// whether to resume without scrolling the whole transcript.
type SessionProgress struct {
	LastUser      string `json:"lastUser"`      // last genuine user prompt (not a tool result)
	LastAssistant string `json:"lastAssistant"` // full text of the last assistant reply
	UpdatedAt     int64  `json:"updatedAt"`     // file mtime, unix millis
}

// innerMessage matches the "message" object on user/assistant events. Content
// is either a plain string or an array of typed blocks; we keep text blocks.
type innerMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ReadSession loads the full transcript of one session as ordered messages,
// keeping only user/assistant text (tool calls and metadata are skipped).
func ReadSession(projectSlug, sessionID string) ([]SessionMessage, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	path := findClaudeSessionFile(root, projectSlug, sessionID)
	if path == "" {
		return nil, fmt.Errorf("open session: transcript %q not found", sessionID)
	}
	return ReadSessionFile(path)
}

// ReadSessionFile loads a transcript by its absolute path. Prefer this over
// ReadSession whenever the caller already holds a SessionMeta (which carries
// File): it skips re-deriving the project directory and the recursive fallback
// walk, which matters when indexing hundreds of sessions.
func ReadSessionFile(path string) ([]SessionMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	out := make([]SessionMessage, 0)
	for sc.Scan() {
		var ev rawEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type != "user" && ev.Type != "assistant" {
			continue
		}
		var im innerMessage
		if json.Unmarshal(ev.Message, &im) != nil {
			continue
		}
		text := extractText(im.Content)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out = append(out, SessionMessage{Role: ev.Type, Text: text})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	return out, nil
}

// ReadProgress returns just the last genuine user prompt and the last assistant
// reply of a session — the "where did I leave off" snapshot. Tool-result-only
// user turns carry no text blocks, so extractText yields "" for them and they're
// skipped naturally: the result is the last thing you actually typed, not a tool
// echo.
func ReadProgress(projectSlug, sessionID string) (SessionProgress, error) {
	root, err := Root()
	if err != nil {
		return SessionProgress{}, err
	}
	path := findClaudeSessionFile(root, projectSlug, sessionID)
	if path == "" {
		return SessionProgress{}, fmt.Errorf("open session: transcript %q not found", sessionID)
	}
	return readProgressFile(path)
}

// progressTailWindows are the byte windows readProgressFile tries, smallest
// first. A progress card needs only the last prompt and the last reply, and
// those sit at the very end of the transcript — yet this used to scan the file
// from the start, costing ~235 ms on a 16 MB session and then reading the same
// file again in full to display it. A tail read finds both in a few hundred
// kilobytes, so a big transcript costs milliseconds.
//
// The list is deliberately short. Widening is only worth it when the previous
// window was too small to contain a line; trying ever-larger windows in the hope
// of finding a field that sits far back just re-reads the file several times over
// (measured: a 4 MB subagent transcript whose last message ran to 570 KB took
// 125 ms that way, worse than the 59 ms full scan it replaced). So the total
// extra I/O is capped at one tail read after the first, and the fallback is the
// whole file — never worse than the behaviour this replaced.
var progressTailWindows = []int64{256 << 10, 1 << 20}

// readProgressFile reads the two fields from the end of a transcript, falling
// back to a full scan when the tail cannot answer.
func readProgressFile(path string) (SessionProgress, error) {
	f, err := os.Open(path)
	if err != nil {
		return SessionProgress{}, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return SessionProgress{}, fmt.Errorf("open session: %w", err)
	}
	updatedAt := info.ModTime().UnixMilli()
	size := info.Size()

	for _, window := range progressTailWindows {
		if window >= size {
			break // the full scan below covers this file
		}
		tail, tooSmall, err := scanTailWindow(f, size-window, window)
		if err != nil {
			break // unreadable window: let the full scan report the real problem
		}
		if tooSmall {
			// The window held no line break at all, so it sits inside a single
			// long line. A wider window is a targeted fix: it reaches back past
			// the start of that line.
			continue
		}
		if tail.LastUser != "" && tail.LastAssistant != "" {
			tail.UpdatedAt = updatedAt
			return tail, nil
		}
		// The window held whole lines but did not answer both fields. Finding the
		// missing one would mean re-reading further back, which is exactly what
		// the full scan does — once.
		break
	}

	full, err := scanProgressEvents(f, 0)
	if err != nil {
		return SessionProgress{}, fmt.Errorf("scan session: %w", err)
	}
	full.UpdatedAt = updatedAt
	return full, nil
}

// scanTailWindow parses the complete lines inside the last `window` bytes of f.
//
// tooSmall reports that the slice contained no line break at all, which means a
// single line is longer than the window — the case that made a naive tail reader
// return nothing for a subagent transcript whose last message ran to half a
// megabyte. Distinguishing it from "the window held real lines but no turns" is
// what lets the caller widen in one case and give up cheaply in the other.
func scanTailWindow(f *os.File, start, window int64) (SessionProgress, bool, error) {
	buf := make([]byte, window)
	n, err := f.ReadAt(buf, start)
	if err != nil && err != io.EOF {
		return SessionProgress{}, false, err
	}
	buf = buf[:n]
	idx := bytes.IndexByte(buf, '\n')
	if idx < 0 {
		return SessionProgress{}, true, nil
	}
	// Everything after the first line break is a whole line.
	p, err := scanProgressEvents(bytes.NewReader(buf[idx+1:]), 0)
	return p, false, err
}

// scanProgressEvents reads user/assistant turns from a reader positioned at
// offset (0 for the whole-file case), keeping the last text of each role.
func scanProgressEvents(r io.ReadSeeker, offset int64) (SessionProgress, error) {
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return SessionProgress{}, err
	}
	var p SessionProgress
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var ev rawEvent
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type != "user" && ev.Type != "assistant" {
			continue
		}
		var im innerMessage
		if json.Unmarshal(ev.Message, &im) != nil {
			continue
		}
		text := strings.TrimSpace(extractText(im.Content))
		if text == "" {
			continue
		}
		if ev.Type == "user" {
			p.LastUser = text
		} else {
			p.LastAssistant = text
		}
	}
	if err := sc.Err(); err != nil {
		return p, err
	}
	return p, nil
}

// findClaudeSessionFile returns the transcript for a session id. The answer is
// memoised (see locate.go); the stat check there makes a moved transcript re-walk
// instead of resolving to a stale path.
func findClaudeSessionFile(root, projectSlug, sessionID string) string {
	if p := locateCached(locateKindClaude, sessionID); p != "" {
		return p
	}
	projectDir := filepath.Join(root, "projects", filepath.Base(projectSlug))
	name := filepath.Base(sessionID) + ".jsonl"
	// The common case is a top-level transcript, which is one stat. Anything else
	// (a subagent transcript in a nested directory) falls back to a search that
	// prefers an exact file-name match over a name that merely contains the id.
	direct := filepath.Join(projectDir, name)
	if info, err := os.Stat(direct); err == nil && !info.IsDir() {
		rememberLocate(locateKindClaude, sessionID, direct)
		return direct
	}
	found := findSessionFileUnder(sessionID, projectDir)
	rememberLocate(locateKindClaude, sessionID, found)
	return found
}

// extractText handles content that is either a JSON string or an array of
// blocks, concatenating the text blocks.
func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []contentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "text" && blk.Text != "" {
				if b.Len() > 0 {
					b.WriteString("\n")
				}
				b.WriteString(blk.Text)
			}
		}
		return b.String()
	}
	return ""
}
