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
