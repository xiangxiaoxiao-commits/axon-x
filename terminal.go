package main

import (
	"encoding/base64"
	"sync"

	"axon/internal/term"
)

// Terminal events emitted to the frontend.
const (
	EventTermData = "term:data"
	EventTermExit = "term:exit"
)

// termState holds the single embedded shell session. One terminal is enough
// for the first version; a multi-tab terminal can come later.
type termState struct {
	mu      sync.Mutex
	session *term.Session
}

// TermStart launches the embedded shell (idempotent: a running session is
// reused). Shell output is streamed to the frontend as base64 on term:data.
func (a *App) TermStart() error {
	a.term.mu.Lock()
	defer a.term.mu.Unlock()
	if a.term.session != nil {
		return nil
	}
	s, err := term.Start(
		func(b []byte) {
			// base64 so arbitrary bytes survive the JSON event bridge intact.
			a.emit(EventTermData, base64.StdEncoding.EncodeToString(b))
		},
		func() {
			a.term.mu.Lock()
			a.term.session = nil
			a.term.mu.Unlock()
			a.emit(EventTermExit, "")
		},
	)
	if err != nil {
		return err
	}
	a.term.session = s
	return nil
}

// TermWrite forwards keystrokes to the shell.
func (a *App) TermWrite(data string) error {
	a.term.mu.Lock()
	s := a.term.session
	a.term.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Write(data)
}

// TermResize informs the shell of the terminal size (in character cells).
func (a *App) TermResize(rows, cols int) error {
	a.term.mu.Lock()
	s := a.term.session
	a.term.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Resize(uint16(rows), uint16(cols))
}

// TermStop terminates the embedded shell.
func (a *App) TermStop() {
	a.term.mu.Lock()
	s := a.term.session
	a.term.session = nil
	a.term.mu.Unlock()
	if s != nil {
		s.Close()
	}
}
