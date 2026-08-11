package main

import "axon/internal/claudedata"

// These methods expose Claude Code's on-disk sessions and memory files to the
// frontend, turning the app into a browser/manager for the native CLI's data.

// ListClaudeProjects returns all projects that have session transcripts.
func (a *App) ListClaudeProjects() ([]claudedata.Project, error) {
	return claudedata.ListProjects()
}

// ListClaudeSessions returns session summaries for a project.
func (a *App) ListClaudeSessions(projectSlug string) ([]claudedata.SessionMeta, error) {
	return claudedata.ListSessions(projectSlug)
}

// ReadClaudeSession loads the full transcript of a session.
func (a *App) ReadClaudeSession(projectSlug, sessionID string) ([]claudedata.SessionMessage, error) {
	return claudedata.ReadSession(projectSlug, sessionID)
}

// ListMemory returns the global CLAUDE.md plus a project's memory/*.md files.
func (a *App) ListMemoryFiles(projectSlug string) ([]claudedata.MemoryFile, error) {
	return claudedata.ListMemory(projectSlug)
}

// WriteMemory saves content to a memory file inside ~/.claude.
func (a *App) WriteMemoryFile(path, content string) error {
	return claudedata.WriteMemory(path, content)
}

// DeleteMemory removes a memory file inside ~/.claude.
func (a *App) DeleteMemoryFile(path string) error {
	return claudedata.DeleteMemory(path)
}
