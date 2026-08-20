//go:build windows

// Package term on Windows uses cmd.exe with stdin/stdout pipes instead of PTY.
// ConPTY would be ideal but requires CGO; pipes give basic functionality.
package term

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// Session is one shell backed by stdin/stdout pipes. Safe for concurrent use.
type Session struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  bool
}

// Start launches cmd.exe (or COMSPEC) with piped stdin/stdout.
func Start(onData func([]byte), onExit func()) (*Session, error) {
	shell := os.Getenv("COMSPEC")
	if shell == "" {
		shell = "cmd.exe"
	}
	cmd := exec.Command(shell)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start shell: %w", err)
	}

	s := &Session{cmd: cmd, stdin: stdin}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				onData(chunk)
			}
			if err != nil {
				break
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

// Write sends input to the shell's stdin.
func (s *Session) Write(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || s.stdin == nil {
		return io.ErrClosedPipe
	}
	_, err := s.stdin.Write([]byte(data))
	return err
}

// Resize is a no-op on Windows pipe mode (no PTY to resize).
func (s *Session) Resize(rows, cols uint16) error {
	return nil
}

// Close terminates the shell.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.done = true
	if s.stdin != nil {
		s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
}
