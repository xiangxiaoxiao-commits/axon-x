package main

import "testing"

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/Users/x/xx": "'/Users/x/xx'",
		"":            "''",
		"a b":         "'a b'",
		"it's":        `'it'\''s'`,
		"a;rm -rf /":  "'a;rm -rf /'", // metacharacters stay inside quotes
		"$(whoami)":   "'$(whoami)'",
		"`id`":        "'`id`'",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResumeShellCommand(t *testing.T) {
	got := resumeShellCommand("/Users/x/xx", "abc-123")
	want := "cd '/Users/x/xx' && claude --resume 'abc-123'"
	if got != want {
		t.Errorf("with cwd: got %q, want %q", got, want)
	}

	// No cwd: skip the cd, resume in place.
	got = resumeShellCommand("", "abc-123")
	want = "claude --resume 'abc-123'"
	if got != want {
		t.Errorf("no cwd: got %q, want %q", got, want)
	}

	// A cwd that tries to break out stays a single quoted arg.
	got = resumeShellCommand("/tmp'; rm -rf /", "id")
	want = `cd '/tmp'\''; rm -rf /' && claude --resume 'id'`
	if got != want {
		t.Errorf("injection cwd: got %q, want %q", got, want)
	}
}

func TestResumeCommandNewline(t *testing.T) {
	a := &App{}
	if got := a.ResumeCommand("/x", "id"); got != "cd '/x' && claude --resume 'id'\n" {
		t.Errorf("ResumeCommand should append newline, got %q", got)
	}
}
