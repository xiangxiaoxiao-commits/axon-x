package mcpinstall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Agent describes one supported AI agent and how to install/uninstall MCP.
type Agent struct {
	Name   string `json:"name"`   // internal key: "claude", "codex", "workbuddy", "codebuddy"
	Label  string `json:"label"`  // display name
	Format string `json:"format"` // "json" or "toml"
}

// SupportedAgents lists all agents the one-click install supports.
var SupportedAgents = []Agent{
	{Name: "claude", Label: "Claude Code", Format: "json"},
	{Name: "codex", Label: "OpenAI Codex", Format: "toml"},
	{Name: "workbuddy", Label: "WorkBuddy", Format: "json"},
	{Name: "codebuddy", Label: "CodeBuddy", Format: "json"},
}

// AgentStatus is the install status for one agent.
type AgentStatus struct {
	Agent      Agent  `json:"agent"`
	Installed  bool   `json:"installed"`
	Command    string `json:"command"`
	ConfigPath string `json:"configPath"`
	Error      string `json:"error,omitempty"`
}

// agentConfigPath returns the config file path for a given agent.
func agentConfigPath(agent Agent) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	switch agent.Name {
	case "claude":
		return filepath.Join(home, ".claude.json"), nil
	case "codex":
		dir := os.Getenv("CODEX_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".codex")
		}
		return filepath.Join(dir, "config.toml"), nil
	case "workbuddy":
		return filepath.Join(home, ".workbuddy", ".mcp.json"), nil
	case "codebuddy":
		return filepath.Join(home, ".codebuddy", ".mcp.json"), nil
	default:
		return "", fmt.Errorf("unknown agent %q", agent.Name)
	}
}

// StatusAll returns the install status for every supported agent.
func StatusAll() []AgentStatus {
	bin, _ := LocateMCPBinary()
	var out []AgentStatus
	for _, ag := range SupportedAgents {
		st := AgentStatus{Agent: ag}
		path, err := agentConfigPath(ag)
		if err != nil {
			st.Error = err.Error()
			out = append(out, st)
			continue
		}
		st.ConfigPath = path
		switch ag.Format {
		case "json":
			st.Installed, st.Command = jsonAgentStatus(path)
		case "toml":
			st.Installed, st.Command = tomlAgentStatus(path)
		}
		out = append(out, st)
	}
	_ = bin
	return out
}

// InstallAll registers axon-knowledge in all supported agents. Returns per-agent errors.
func InstallAll(mcpBin string) []AgentStatus {
	if mcpBin == "" {
		var err error
		mcpBin, err = LocateMCPBinary()
		if err != nil {
			st := make([]AgentStatus, len(SupportedAgents))
			for i, ag := range SupportedAgents {
				st[i] = AgentStatus{Agent: ag, Error: err.Error()}
			}
			return st
		}
	}
	// On Windows, normalize to forward slashes for consistency in JSON configs,
	// but keep backslashes in TOML (Windows native path).
	var out []AgentStatus
	for _, ag := range SupportedAgents {
		st := AgentStatus{Agent: ag}
		path, err := agentConfigPath(ag)
		if err != nil {
			st.Error = err.Error()
			out = append(out, st)
			continue
		}
		st.ConfigPath = path

		switch ag.Format {
		case "json":
			err = installJSON(path, mcpBin)
		case "toml":
			err = installTOML(path, mcpBin)
		}
		if err != nil {
			st.Error = err.Error()
		} else {
			st.Installed = true
			st.Command = mcpBin
		}
		out = append(out, st)
	}
	return out
}

// UninstallAll removes axon-knowledge from all supported agents.
func UninstallAll() []AgentStatus {
	var out []AgentStatus
	for _, ag := range SupportedAgents {
		st := AgentStatus{Agent: ag}
		path, err := agentConfigPath(ag)
		if err != nil {
			st.Error = err.Error()
			out = append(out, st)
			continue
		}
		st.ConfigPath = path
		switch ag.Format {
		case "json":
			err = uninstallJSON(path)
		case "toml":
			err = uninstallTOML(path)
		}
		if err != nil {
			st.Error = err.Error()
		}
		out = append(out, st)
	}
	return out
}

// --- JSON-based agents (Claude, WorkBuddy, CodeBuddy) ---

func installJSON(path, mcpBin string) error {
	// Ensure parent directory exists (e.g. ~/.workbuddy/ might not exist yet).
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	root, err := readConfig(path)
	if err != nil {
		return err
	}
	if root == nil {
		root = map[string]json.RawMessage{}
	}
	servers := serversMap(root)
	entry, _ := json.Marshal(serverEntry{Type: "stdio", Command: mcpBin, Args: []string{}})
	servers[ServerName] = entry
	if err := putServers(root, servers); err != nil {
		return err
	}
	return writeConfig(path, root)
}

func uninstallJSON(path string) error {
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

func jsonAgentStatus(path string) (installed bool, command string) {
	root, err := readConfig(path)
	if err != nil || root == nil {
		return false, ""
	}
	servers := serversMap(root)
	if raw, ok := servers[ServerName]; ok {
		var e serverEntry
		if json.Unmarshal(raw, &e) == nil && e.Command != "" {
			return true, e.Command
		}
	}
	return false, ""
}

// --- TOML-based agent (Codex) ---
// We handle the TOML config with simple string manipulation to avoid pulling a
// TOML library. The section format is:
//
//	[mcp_servers.axon-knowledge]
//	command = "/path/to/axon-mcp"

func installTOML(path, mcpBin string) error {
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %q: %w", path, err)
	}

	section := fmt.Sprintf("[mcp_servers.%s]", ServerName)
	text := string(content)

	// Normalize path for TOML (escape backslashes on Windows).
	escapedBin := mcpBin
	if runtime.GOOS == "windows" {
		escapedBin = strings.ReplaceAll(mcpBin, `\`, `\\`)
	}
	newSection := fmt.Sprintf("%s\ncommand = %q\n", section, escapedBin)

	if idx := strings.Index(text, section); idx >= 0 {
		// Replace existing section (up to next [section] or EOF).
		end := findNextSection(text, idx+len(section))
		text = text[:idx] + newSection + "\n" + text[end:]
	} else {
		// Append new section.
		if !strings.HasSuffix(text, "\n") && len(text) > 0 {
			text += "\n"
		}
		text += "\n" + newSection
	}

	return os.WriteFile(path, []byte(text), 0o644)
}

func uninstallTOML(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %q: %w", path, err)
	}

	section := fmt.Sprintf("[mcp_servers.%s]", ServerName)
	text := string(content)

	idx := strings.Index(text, section)
	if idx < 0 {
		return nil // not installed
	}
	end := findNextSection(text, idx+len(section))
	text = text[:idx] + text[end:]

	return os.WriteFile(path, []byte(text), 0o644)
}

func tomlAgentStatus(path string) (installed bool, command string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	section := fmt.Sprintf("[mcp_servers.%s]", ServerName)
	text := string(content)
	idx := strings.Index(text, section)
	if idx < 0 {
		return false, ""
	}
	// Extract command value from lines after the section header.
	end := findNextSection(text, idx+len(section))
	block := text[idx+len(section) : end]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "command") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"`)
				if runtime.GOOS == "windows" {
					val = strings.ReplaceAll(val, `\\`, `\`)
				}
				return true, val
			}
		}
	}
	return true, ""
}

// findNextSection returns the index of the next TOML [section] header after pos,
// or len(text) if none found.
func findNextSection(text string, pos int) int {
	rest := text[pos:]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '\n' && i+1 < len(rest) && rest[i+1] == '[' {
			return pos + i + 1
		}
	}
	return len(text)
}
