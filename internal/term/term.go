// Package term runs a real login shell behind a PTY so the app can host an
// embedded terminal alongside the AI chat. Output is streamed to a callback
// (wired to a Wails event); input, resize and close are driven from the UI.
package term

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// Session is one PTY-backed shell. Safe for concurrent Write/Resize/Close.
type Session struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	ptmx *os.File
	done bool
}

// Start launches the user's shell in a new PTY. onData is called with each
// chunk of shell output on a background goroutine until the shell exits or the
// session is closed. onExit (optional) fires once when the shell process ends.
func Start(onData func([]byte), onExit func()) (*Session, error) {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	s := &Session{cmd: cmd, ptmx: ptmx}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				onData(chunk)
			}
			if err != nil {
				break // EOF or closed
			}
		}
		s.mu.Lock()
		s.done = true
		s.mu.Unlock()
		if onExit != nil {
			onExit()
		}
	}()
	return s, nil
}

// Write sends input (keystrokes) to the shell.
func (s *Session) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || s.ptmx == nil {
		return io.ErrClosedPipe
	}
	_, err := s.ptmx.Write([]byte(data))
	return err
}

// Resize informs the PTY of the terminal's new dimensions.
func (s *Session) Resize(rows, cols uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || s.ptmx == nil {
		return io.ErrClosedPipe
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Close terminates the shell and releases the PTY.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	if s.ptmx != nil {
		s.ptmx.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}
