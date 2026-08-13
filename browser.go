package main

import (
	"strings"

	"axon/internal/claudedata"
)

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

// ClaudeSessionProgress returns the "where did I leave off" snapshot (last user
// prompt + last assistant reply) so the UI can preview a session before
// resuming it.
func (a *App) ClaudeSessionProgress(projectSlug, sessionID string) (claudedata.SessionProgress, error) {
	return claudedata.ReadProgress(projectSlug, sessionID)
}

// ResumeCommand builds the shell command that reopens a session in Claude Code:
// cd into the session's original working directory, then `claude --resume <id>`.
// Both fields are single-quote escaped so paths/ids with spaces or metacharacters
// can't break out of the command. A trailing newline makes it execute when
// written to the embedded terminal. cwd may be empty (older transcripts); then
// the cd is skipped and resume runs in the terminal's current directory.
func (a *App) ResumeCommand(cwd, sessionID string) string {
	resume := "claude --resume " + shellQuote(sessionID)
	if strings.TrimSpace(cwd) == "" {
		return resume + "\n"
	}
	return "cd " + shellQuote(cwd) + " && " + resume + "\n"
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes, so
// it is safe as one argument in a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
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
