package main

import (
	"runtime"
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

// ScopeOption is one entry of the session browser's scope switcher.
type ScopeOption struct {
	Key   string `json:"key"`   // value to pass back as the scope
	Label string `json:"label"` // human label ("wanlian", "未归属", "全部")
	Count int    `json:"count"`
}

// SessionList is a scope's sessions plus the counts that label the switcher, so
// the page can explain a filtered list without a second scan.
type SessionList struct {
	Sessions   []claudedata.SessionMeta `json:"sessions"`
	Scopes     []ScopeOption            `json:"scopes"`
	Total      int                      `json:"total"`
	Unassigned int                      `json:"unassigned"`
}

// ListClaudeSessions returns the sessions the browser should show for one scope
// (see claudedata: a namespace name, ScopeUnassigned, or ScopeAll).
//
// It used to ignore its parameter and always return every session on the
// machine, on the grounds that a namespace name never matches Claude Code's
// path-encoded directory name. That reasoning was right about the directory
// names and wrong about the conclusion: each transcript records the working
// directory it ran in, and that is a reliable bridge from a namespace to a
// session. So the header's "which body of work" selection now actually governs
// the list instead of being decorative.
func (a *App) ListClaudeSessions(scope string) (SessionList, error) {
	scope = claudedata.NormalizeScope(scope)

	// One unfiltered pass, then filter and count in memory. The alternative —
	// asking each agent for the scope and then again for the totals — would scan
	// every transcript twice, and the whole point of the metadata cache is that a
	// repeat scan is nearly free while a second one is not.
	var all []claudedata.SessionMeta
	projects, _ := claudedata.ListProjects()
	for _, p := range projects {
		sessions, _ := claudedata.ListAllSessions(p.Slug)
		all = append(all, sessions...)
	}
	codex, _ := claudedata.ListCodexSessions(claudedata.ScopeAll)
	all = append(all, codex...)
	buddy, _ := claudedata.ListAllBuddySessions(claudedata.ScopeAll)
	all = append(all, buddy...)

	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt > all[j].UpdatedAt })

	res := SessionList{Total: len(all), Sessions: []claudedata.SessionMeta{}}
	for _, s := range all {
		if s.Namespace == "" {
			res.Unassigned++
		}
		if claudedata.MatchesScope(s, scope) {
			res.Sessions = append(res.Sessions, s)
		}
	}

	// Only three reaches are worth a tab: the namespace the header selected (the
	// page's default), the sessions no namespace claims, and everything. Other
	// namespaces are reached through the header picker, not from here.
	if claudedata.IsNamespaceScope(scope) {
		res.Scopes = append(res.Scopes, ScopeOption{
			Key: scope, Label: scope, Count: len(res.Sessions),
		})
	}
	res.Scopes = append(res.Scopes,
		ScopeOption{Key: claudedata.ScopeUnassigned, Label: "未归属", Count: res.Unassigned},
		ScopeOption{Key: claudedata.ScopeAll, Label: "全部", Count: res.Total},
	)
	return res, nil
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
// resuming it. For non-Claude sessions, returns empty progress (no error).
func (a *App) ClaudeSessionProgress(projectSlug, sessionID string) (claudedata.SessionProgress, error) {
	// Non-Claude sessions don't support ReadProgress; return empty.
	if strings.HasPrefix(sessionID, "codex:") ||
		strings.HasPrefix(sessionID, "workbuddy:") ||
		strings.HasPrefix(sessionID, "codebuddy:") {
		return claudedata.SessionProgress{}, nil
	}
	return claudedata.ReadProgress(projectSlug, sessionID)
}

// resumeShellCommand builds the shell command that reopens a session in the
// appropriate agent. Dispatches by session ID prefix:
//   - plain ID -> claude --resume <id>
//   - codex:<id> -> codex resume <id>
//   - workbuddy:/codebuddy: -> cd to cwd only (no resume support yet)
//
// On Windows, uses cmd.exe syntax (cd /d, &); on Unix, POSIX shell (cd, &&).
func resumeShellCommand(cwd, sessionID string) string {
	var resume string
	switch {
	case strings.HasPrefix(sessionID, "codex:"):
		rawID := strings.TrimPrefix(sessionID, "codex:")
		resume = "codex resume " + quote(rawID)
	case strings.HasPrefix(sessionID, "workbuddy:") || strings.HasPrefix(sessionID, "codebuddy:"):
		resume = ""
	default:
		resume = "claude --resume " + quote(sessionID)
	}
	if strings.TrimSpace(cwd) == "" {
		if resume == "" {
			return echoMsg("This session does not support resume")
		}
		return resume
	}
	if resume == "" {
		return cdCmd(cwd)
	}
	return cdCmd(cwd) + chainOp() + resume
}

// ResumeCommand returns the resume command with a trailing newline, ready to
// hand to TermStartResume (the newline makes it execute in the new tab's shell).
func (a *App) ResumeCommand(cwd, sessionID string) string {
	return resumeShellCommand(cwd, sessionID) + "\n"
}

// quote wraps a value for safe shell interpolation (platform-aware).
func quote(s string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// cdCmd returns the cd command appropriate for the OS.
func cdCmd(dir string) string {
	if runtime.GOOS == "windows" {
		return `cd /d ` + quote(dir)
	}
	return "cd " + quote(dir)
}

// chainOp returns the command chaining operator for the OS.
func chainOp() string {
	if runtime.GOOS == "windows" {
		return " & "
	}
	return " && "
}

// echoMsg returns a platform-appropriate echo command.
func echoMsg(msg string) string {
	if runtime.GOOS == "windows" {
		return `echo ` + msg
	}
	return "echo " + quote(msg)
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
