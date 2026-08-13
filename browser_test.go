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

func TestResumeCommand(t *testing.T) {
	a := &App{}

	got := a.ResumeCommand("/Users/x/xx", "abc-123")
	want := "cd '/Users/x/xx' && claude --resume 'abc-123'\n"
	if got != want {
		t.Errorf("with cwd: got %q, want %q", got, want)
	}

	// No cwd: skip the cd, resume in place.
	got = a.ResumeCommand("", "abc-123")
	want = "claude --resume 'abc-123'\n"
	if got != want {
		t.Errorf("no cwd: got %q, want %q", got, want)
	}

	// A cwd that tries to break out stays a single quoted arg.
	got = a.ResumeCommand("/tmp'; rm -rf /", "id")
	want = `cd '/tmp'\''; rm -rf /' && claude --resume 'id'` + "\n"
	if got != want {
		t.Errorf("injection cwd: got %q, want %q", got, want)
	}
}
