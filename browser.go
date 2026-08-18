package main

import (
	"sort"
	"strings"

	"axon/internal/claudedata"
)

// These methods expose Claude Code's on-disk sessions and memory files to the
// frontend, turning the app into a browser/manager for the native CLI's data.

// ListClaudeProjects returns all projects that have session transcripts.
func (a *App) ListClaudeProjects() ([]claudedata.Project, error) {
	return claudedata.ListProjects()
}

// ListClaudeSessions returns session summaries for a project, including all
// agent sources (Claude Code, Codex, WorkBuddy, CodeBuddy).
func (a *App) ListClaudeSessions(projectSlug string) ([]claudedata.SessionMeta, error) {
	// Claude Code sessions.
	claude, _ := claudedata.ListSessions(projectSlug)
	// Codex sessions.
	codex, _ := claudedata.ListCodexSessions(projectSlug)
	// WorkBuddy + CodeBuddy sessions.
	buddy, _ := claudedata.ListBuddySessions(projectSlug)

	all := make([]claudedata.SessionMeta, 0, len(claude)+len(codex)+len(buddy))
	all = append(all, claude...)
	all = append(all, codex...)
	all = append(all, buddy...)

	// Sort newest first.
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt > all[j].UpdatedAt })
	return all, nil
}

// ReadClaudeSession loads the full transcript of a session. Handles sessions
// from all agents: plain ID = Claude Code, "codex:" prefix = Codex,
// "workbuddy:"/"codebuddy:" prefix = buddy agents.
func (a *App) ReadClaudeSession(projectSlug, sessionID string) ([]claudedata.SessionMessage, error) {
	if strings.HasPrefix(sessionID, "codex:") {
		rawID := strings.TrimPrefix(sessionID, "codex:")
		file := claudedata.FindCodexSessionFile(rawID)
		if file == "" {
			return nil, nil
		}
		return claudedata.ReadCodexSession(file)
	}
	if strings.HasPrefix(sessionID, "workbuddy:") || strings.HasPrefix(sessionID, "codebuddy:") {
		rawID := sessionID
		rawID = strings.TrimPrefix(rawID, "workbuddy:")
		rawID = strings.TrimPrefix(rawID, "codebuddy:")
		file := claudedata.FindBuddySessionFile(rawID)
		if file == "" {
			return nil, nil
		}
		return claudedata.ReadBuddySession(file)
	}
	return claudedata.ReadSession(projectSlug, sessionID)
}

// ClaudeSessionProgress returns the "where did I leave off" snapshot (last user
// prompt + last assistant reply) so the UI can preview a session before
// resuming it.
func (a *App) ClaudeSessionProgress(projectSlug, sessionID string) (claudedata.SessionProgress, error) {
	return claudedata.ReadProgress(projectSlug, sessionID)
}

// resumeShellCommand builds the shell command that reopens a session in Claude
// Code: cd into the session's original working directory, then
// `claude --resume <id>`. Both fields are single-quote escaped so paths/ids with
// spaces or metacharacters can't break out of the command. cwd may be empty
// (older transcripts); then the cd is skipped and resume runs in place.
func resumeShellCommand(cwd, sessionID string) string {
	resume := "claude --resume " + shellQuote(sessionID)
	if strings.TrimSpace(cwd) == "" {
		return resume
	}
	return "cd " + shellQuote(cwd) + " && " + resume
}

// ResumeCommand returns the resume command with a trailing newline, ready to
// hand to TermStartResume (the newline makes it execute in the new tab's shell).
func (a *App) ResumeCommand(cwd, sessionID string) string {
	return resumeShellCommand(cwd, sessionID) + "\n"
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
