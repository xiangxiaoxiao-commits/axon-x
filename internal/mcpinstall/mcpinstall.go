// Package mcpinstall wires axon's stdio MCP server into Claude Code's user-scope
// configuration (~/.claude.json) so the GUI can offer one-click install without
// shelling out to the `claude` CLI — which is often not on PATH when the app is
// launched from Finder/Explorer. It edits only the single "axon-knowledge"
// entry under mcpServers and preserves every other field via a JSON round-trip.
package mcpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ServerName is the stable key axon uses under mcpServers. Re-installing
// overwrites this entry in place; uninstalling removes exactly this key.
const ServerName = "axon-knowledge"

// serverEntry is one stdio MCP server as Claude Code stores it. Fields match the
// on-disk schema: type/command/args/env.
type serverEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// Status reports whether axon's MCP server is registered and, if so, which
// binary path it points at (so the UI can flag a stale path after an app move).
type Status struct {
	Installed  bool   `json:"installed"`
	Command    string `json:"command"`
	ConfigPath string `json:"configPath"`
}

// ConfigPath returns the path to Claude Code's user config (~/.claude.json).
// os.UserHomeDir resolves $HOME on unix and %USERPROFILE% on Windows, matching
// where Claude Code itself looks.
func ConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

// GetStatus reads the config and reports whether axon-knowledge is registered.
// A missing config file is not an error — it just means "not installed".
func GetStatus() (Status, error) {
	path, err := ConfigPath()
	if err != nil {
		return Status{}, err
	}
	st := Status{ConfigPath: path}

	root, err := readConfig(path)
	if err != nil {
		return st, err
	}
	servers := serversMap(root)
	if raw, ok := servers[ServerName]; ok {
		var e serverEntry
		if json.Unmarshal(raw, &e) == nil {
			st.Installed = true
			st.Command = e.Command
		}
	}
	return st, nil
}

// Install registers (or updates in place) the axon-knowledge stdio server
// pointing at mcpBin. It creates ~/.claude.json if absent and leaves every
// other key untouched. The write is atomic (temp file + rename).
func Install(mcpBin string) error {
	if mcpBin == "" {
		return fmt.Errorf("mcp binary path is empty")
	}
	if _, err := os.Stat(mcpBin); err != nil {
		return fmt.Errorf("mcp binary not found at %q: %w", mcpBin, err)
	}

	path, err := ConfigPath()
	if err != nil {
		return err
	}
	root, err := readConfig(path)
	if err != nil {
		return err
	}
	if root == nil {
		root = map[string]json.RawMessage{}
	}

	servers := serversMap(root)
	entry, _ := json.Marshal(serverEntry{Type: "stdio", Command: mcpBin})
	servers[ServerName] = entry

	if err := putServers(root, servers); err != nil {
		return err
	}
	return writeConfig(path, root)
}

// Uninstall removes the axon-knowledge entry. Removing a missing entry (or a
// missing config file) is a no-op, not an error.
func Uninstall() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	root, err := readConfig(path)
	if err != nil || root == nil {
		return err
	}
	servers := serversMap(root)
	if _, ok := servers[ServerName]; !ok {
		return nil
	}
	delete(servers, ServerName)
	if err := putServers(root, servers); err != nil {
		return err
	}
	return writeConfig(path, root)
}
