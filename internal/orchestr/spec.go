package orchestr

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"axon/internal/task"
)

// ParseSpec extracts the JSON object from a model reply (tolerating code fences
// or surrounding prose) and unmarshals it into a Spec. ok is false when the
// reply can't be parsed, so the caller can degrade to an empty, user-fillable
// form instead of retrying (and paying for another call).
func ParseSpec(reply string) (task.Spec, bool) {
	s := strings.TrimSpace(reply)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var spec task.Spec
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return task.Spec{}, false
	}
	return spec, true
}

// TitleFromInput derives a short title from the first non-empty line of input.
// The cut is by rune, not byte: slicing a multi-byte line at 60 bytes would
// leave a truncated UTF-8 sequence and render as garbage in the task list.
func TitleFromInput(input string) string {
	for _, line := range strings.Split(input, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if rs := []rune(line); len(rs) > 60 {
			return string(rs[:60]) + "..."
		}
		return line
	}
	return "Untitled task"
}

// enrichQuery builds the knowledge-recall query for enrichment: the rough input
// plus, when a spec already carries a scope (re-enrich), its files/modules and
// their base names, so code-sourced entities get recalled too.
func enrichQuery(t task.Task) string {
	scope := t.Spec.Scope
	if len(scope) == 0 {
		return t.Input
	}
	var b strings.Builder
	b.WriteString(t.Input)
	for _, s := range scope {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		b.WriteString(" ")
		b.WriteString(s)
		if strings.Contains(s, "/") {
			base := s[strings.LastIndex(s, "/")+1:]
			b.WriteString(" ")
			b.WriteString(strings.TrimSuffix(base, filepath.Ext(base)))
		}
	}
	return b.String()
}
