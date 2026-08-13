package mcpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// readConfig loads ~/.claude.json as a top-level object of raw fields, so
// unknown keys survive a round-trip untouched. A missing file returns (nil,
// nil): the caller treats that as an empty config.
func readConfig(path string) (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	if len(data) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse %q (is it valid JSON?): %w", path, err)
	}
	return root, nil
}

// serversMap extracts the mcpServers object as a map of raw entries. A missing
// or malformed mcpServers yields a fresh empty map, so callers can always add.
func serversMap(root map[string]json.RawMessage) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	if root == nil {
		return out
	}
	if raw, ok := root["mcpServers"]; ok {
		_ = json.Unmarshal(raw, &out) // tolerate absent/invalid: keep empty map
		if out == nil {
			out = map[string]json.RawMessage{}
		}
	}
	return out
}

// putServers writes the servers map back into root under mcpServers.
func putServers(root map[string]json.RawMessage, servers map[string]json.RawMessage) error {
	raw, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal mcpServers: %w", err)
	}
	root["mcpServers"] = raw
	return nil
}

// writeConfig serializes root and writes it atomically (temp file + rename in
// the same directory) so a crash mid-write can't corrupt Claude Code's config.
// Output is indented to match the file's human-editable convention.
func writeConfig(path string, root map[string]json.RawMessage) error {
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".claude.json.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op if the rename succeeded

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config %q: %w", path, err)
	}
	return nil
}
