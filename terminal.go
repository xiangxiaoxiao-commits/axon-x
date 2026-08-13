package main

import (
	"encoding/base64"
	"sync"

	"axon/internal/term"
)

// Terminal events emitted to the frontend. Payloads carry the tab id so the
// frontend can route output/exit to the right xterm instance.
const (
	EventTermData = "term:data"
	EventTermExit = "term:exit"
)

// termEvent is the payload for term:* events: which tab, and (for data) the
// base64-encoded shell bytes.
type termEvent struct {
	ID   string `json:"id"`
	Data string `json:"data,omitempty"`
}

// termState holds every open embedded shell, keyed by a frontend-assigned tab
// id, so multiple terminals (e.g. several resumed sessions) run side by side.
type termState struct {
	mu       sync.Mutex
	sessions map[string]*term.Session
}

// TermStart launches a shell for tab id (idempotent: a live tab is reused).
// Output is streamed to the frontend as term:data{id,data}.
func (a *App) TermStart(id string) error {
	return a.termStart(id, "")
}

// TermStartResume launches a shell for tab id and, once it's ready, runs cmd
// (a full `cd … && claude --resume …\n` line). Injecting server-side avoids the
// frontend having to race the shell's first prompt.
func (a *App) TermStartResume(id, cmd string) error {
	return a.termStart(id, cmd)
}

// termStart is the shared launcher. If initialCmd is non-empty it's written to
// the PTY right after start (the shell buffers input, so it runs once ready).
func (a *App) termStart(id, initialCmd string) error {
	a.term.mu.Lock()
	if a.term.sessions == nil {
		a.term.sessions = make(map[string]*term.Session)
	}
	if _, ok := a.term.sessions[id]; ok {
		a.term.mu.Unlock()
		return nil // already running
	}
	a.term.mu.Unlock()

	s, err := term.Start(
		func(b []byte) {
			a.emit(EventTermData, termEvent{ID: id, Data: base64.StdEncoding.EncodeToString(b)})
		},
		func() {
			a.term.mu.Lock()
			delete(a.term.sessions, id)
			a.term.mu.Unlock()
			a.emit(EventTermExit, termEvent{ID: id})
		},
	)
	if err != nil {
		return err
	}
	a.term.mu.Lock()
	a.term.sessions[id] = s
	a.term.mu.Unlock()

	if initialCmd != "" {
		_ = s.Write(initialCmd)
	}
	return nil
}

// session returns the live session for a tab id, or nil.
func (a *App) session(id string) *term.Session {
	a.term.mu.Lock()
	defer a.term.mu.Unlock()
	return a.term.sessions[id]
}

// TermWrite forwards keystrokes to the shell of tab id.
func (a *App) TermWrite(id, data string) error {
	if s := a.session(id); s != nil {
		return s.Write(data)
	}
	return nil
}

// TermResize informs the shell of tab id of the terminal size (character cells).
func (a *App) TermResize(id string, rows, cols int) error {
	if s := a.session(id); s != nil {
		return s.Resize(uint16(rows), uint16(cols))
	}
	return nil
}

// TermStop terminates the shell of tab id and forgets it.
func (a *App) TermStop(id string) {
	a.term.mu.Lock()
	s := a.term.sessions[id]
	delete(a.term.sessions, id)
	a.term.mu.Unlock()
	if s != nil {
		s.Close()
	}
}
