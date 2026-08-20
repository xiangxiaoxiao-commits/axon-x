package main

import (
	"runtime"
	"testing"
)

func TestQuote(t *testing.T) {
	if runtime.GOOS == "windows" {
		cases := map[string]string{
			`C:\Users\x\xx`: `"C:\Users\x\xx"`,
			"":              `""`,
			"a b":           `"a b"`,
			`say "hi"`:      `"say ""hi"""`,
		}
		for in, want := range cases {
			if got := quote(in); got != want {
				t.Errorf("quote(%q) = %q, want %q", in, got, want)
			}
		}
	} else {
		cases := map[string]string{
			"/Users/x/xx": "'/Users/x/xx'",
			"":            "''",
			"a b":         "'a b'",
			"it's":        `'it'\''s'`,
			"a;rm -rf /":  "'a;rm -rf /'",
			"$(whoami)":   "'$(whoami)'",
		}
		for in, want := range cases {
			if got := quote(in); got != want {
				t.Errorf("quote(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

func TestResumeShellCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		got := resumeShellCommand(`C:\Users\x\xx`, "abc-123")
		want := `cd /d "C:\Users\x\xx" & claude --resume "abc-123"`
		if got != want {
			t.Errorf("with cwd: got %q, want %q", got, want)
		}
		got = resumeShellCommand("", "abc-123")
		want = `claude --resume "abc-123"`
		if got != want {
			t.Errorf("no cwd: got %q, want %q", got, want)
		}
	} else {
		got := resumeShellCommand("/Users/x/xx", "abc-123")
		want := "cd '/Users/x/xx' && claude --resume 'abc-123'"
		if got != want {
			t.Errorf("with cwd: got %q, want %q", got, want)
		}
		got = resumeShellCommand("", "abc-123")
		want = "claude --resume 'abc-123'"
		if got != want {
			t.Errorf("no cwd: got %q, want %q", got, want)
		}
	}
}

func TestResumeCodex(t *testing.T) {
	got := resumeShellCommand("/x", "codex:abc-123")
	if runtime.GOOS == "windows" {
		want := `cd /d "/x" & codex resume "abc-123"`
		if got != want {
			t.Errorf("codex resume: got %q, want %q", got, want)
		}
	} else {
		want := "cd '/x' && codex resume 'abc-123'"
		if got != want {
			t.Errorf("codex resume: got %q, want %q", got, want)
		}
	}
}

func TestResumeBuddy(t *testing.T) {
	got := resumeShellCommand("/x", "workbuddy:abc")
	if runtime.GOOS == "windows" {
		want := `cd /d "/x"`
		if got != want {
			t.Errorf("buddy resume: got %q, want %q", got, want)
		}
	} else {
		want := "cd '/x'"
		if got != want {
			t.Errorf("buddy resume: got %q, want %q", got, want)
		}
	}
}
