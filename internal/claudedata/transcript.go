package claudedata

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Axon indexes transcripts written by three different agents, and they share no
// wire format:
//
//	Claude Code   {"type":"user","message":{"content":[{"type":"text","text":"…"}]}}
//	Codex rollout {"type":"event_msg","payload":{"type":"user_message","message":"…"}}
//	WorkBuddy     {"type":"message","role":"user","content":[{"type":"input_text","text":"…"}]}
//
// The reader used to understand only the first shape, so every Codex and
// WorkBuddy session parsed to zero messages: distillation then finished
// "successfully" having indexed nothing. Everything below exists to make one
// line of any of the three parse into (role, text).

// transcriptLine is the union of the three shapes, read field by field so a
// line that only matches one of them still decodes.
type transcriptLine struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Message json.RawMessage `json:"message"` // Claude: message object
	Content json.RawMessage `json:"content"` // WorkBuddy: block array
	Payload *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"payload"` // Codex
}

// parseTranscriptLine decodes one JSONL line into a displayable turn. It returns
// ok=false for anything that is not user/assistant prose: metadata, tool calls,
// tool results, reasoning traces and injected context.
func parseTranscriptLine(line []byte) (role, text string, ok bool) {
	var ev transcriptLine
	if json.Unmarshal(line, &ev) != nil {
		return "", "", false
	}

	switch ev.Type {
	case "user", "assistant":
		// Claude Code.
		if len(ev.Message) == 0 {
			return "", "", false
		}
		var im innerMessage
		if json.Unmarshal(ev.Message, &im) != nil {
			return "", "", false
		}
		t := extractText(im.Content)
		if strings.TrimSpace(t) == "" {
			return "", "", false
		}
		return ev.Type, t, true

	case "message":
		// WorkBuddy / CodeBuddy.
		if ev.Role != "user" && ev.Role != "assistant" {
			return "", "", false
		}
		t := cleanInjectedContext(extractText(ev.Content))
		if strings.TrimSpace(t) == "" {
			return "", "", false
		}
		return ev.Role, t, true

	case "event_msg":
		// Codex rollout.
		if ev.Payload == nil {
			return "", "", false
		}
		switch ev.Payload.Type {
		case "user_message":
			role = "user"
		case "agent_message":
			role = "assistant"
		default:
			return "", "", false
		}
		t := strings.TrimSpace(ev.Payload.Message)
		if t == "" || isToolEcho(t) {
			return "", "", false
		}
		return role, t, true
	}

	return "", "", false
}

// isToolEcho reports whether a Codex message is a tool invocation or its raw
// output rather than something the human or the model actually said. Codex
// records both as ordinary agent_message events, and keeping them would drown
// the distilled knowledge in shell command dumps.
func isToolEcho(t string) bool {
	for _, prefix := range []string{
		"[external_agent_tool_call",
		"[external_agent_tool_result",
		"[external unsupported block",
		"[external_agent_tool",
	} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

var (
	// WorkBuddy wraps the real prompt in <user_query>…</user_query> and stuffs
	// everything else (tool policy, memory, skills) around it.
	userQueryRE = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
	// When there is no user_query wrapper, drop the injected reminder blocks.
	systemReminderRE = regexp.MustCompile(`(?s)<system-reminder[^>]*>.*?</system-reminder>`)
)

// cleanInjectedContext reduces a WorkBuddy user turn to the text the human
// actually typed. Without this every distilled "user" fact is a wall of
// tool-policy boilerplate.
func cleanInjectedContext(t string) string {
	if m := userQueryRE.FindStringSubmatch(t); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	t = systemReminderRE.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}
