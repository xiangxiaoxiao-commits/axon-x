package main

import (
	"fmt"

	"axon/internal/mcpinstall"
)

// MCPStatus reports whether axon's knowledge MCP server is registered in Claude
// Code's user config, and where it points. Bound for the Settings view so the
// UI can show an "已接入 / 未接入" state.
func (a *App) MCPStatus() (mcpinstall.Status, error) {
	return mcpinstall.GetStatus()
}

// InstallMCP wires axon-knowledge into Claude Code's config in one click. It
// locates the shipped axon-mcp binary, registers it (user scope), and returns
// the refreshed status. Idempotent: calling it again updates the path in place.
func (a *App) InstallMCP() (mcpinstall.Status, error) {
	bin, err := mcpinstall.LocateMCPBinary()
	if err != nil {
		return mcpinstall.Status{}, fmt.Errorf("locate axon-mcp: %w", err)
	}
	if err := mcpinstall.Install(bin); err != nil {
		return mcpinstall.Status{}, fmt.Errorf("register MCP server: %w", err)
	}
	return mcpinstall.GetStatus()
}

// UninstallMCP removes the axon-knowledge entry from Claude Code's config and
// returns the refreshed status. Removing a missing entry is not an error.
func (a *App) UninstallMCP() (mcpinstall.Status, error) {
	if err := mcpinstall.Uninstall(); err != nil {
		return mcpinstall.Status{}, fmt.Errorf("remove MCP server: %w", err)
	}
	return mcpinstall.GetStatus()
}

// --- Multi-agent install ---

// MCPAgentStatusAll returns the install status of axon-knowledge across all
// supported agents (Claude Code, Codex, WorkBuddy, CodeBuddy).
func (a *App) MCPAgentStatusAll() []mcpinstall.AgentStatus {
	return mcpinstall.StatusAll()
}

// MCPInstallAll registers axon-knowledge in all supported agents in one click.
func (a *App) MCPInstallAll() []mcpinstall.AgentStatus {
	bin, _ := mcpinstall.LocateMCPBinary()
	return mcpinstall.InstallAll(bin)
}

// MCPUninstallAll removes axon-knowledge from all supported agents.
func (a *App) MCPUninstallAll() []mcpinstall.AgentStatus {
	return mcpinstall.UninstallAll()
}
