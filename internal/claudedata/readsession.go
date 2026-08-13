package claudedata

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	path := filepath.Join(root, "projects", filepath.Base(projectSlug), filepath.Base(sessionID)+".jsonl")
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

// ReadProgress streams a session and returns just the last genuine user prompt
// and the last assistant reply. Tool-result-only user turns carry no text
// blocks, so extractText yields "" for them and they're skipped naturally — the
// result is the last thing you actually typed, not a tool echo.
func ReadProgress(projectSlug, sessionID string) (SessionProgress, error) {
	root, err := Root()
	if err != nil {
		return SessionProgress{}, err
	}
	path := filepath.Join(root, "projects", filepath.Base(projectSlug), filepath.Base(sessionID)+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return SessionProgress{}, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	var p SessionProgress
	if info, err := f.Stat(); err == nil {
		p.UpdatedAt = info.ModTime().UnixMilli()
	}
	sc := bufio.NewScanner(f)
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
		return p, fmt.Errorf("scan session: %w", err)
	}
	return p, nil
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
