package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncodeSlug(t *testing.T) {
	cases := map[string]string{
		"/Users/me/app":        "-Users-me-app",
		"/Users/me/xx-service": "-Users-me-xx-service", // lossy but deterministic
		"/":                    "-",
	}
	for in, want := range cases {
		if got := encodeSlug(in); got != want {
			t.Errorf("encodeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// withCwd runs fn with the process working directory temporarily set to dir.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	defer func() { _ = os.Chdir(prev) }()
	fn()
}

func TestResolveSlug_ExplicitWins(t *testing.T) {
	h := &toolHandler{dataDir: t.TempDir()}
	slug, src := h.resolveSlug("  my-project  ")
	if slug != "my-project" || src != slugExplicit {
		t.Fatalf("got (%q,%v), want (my-project, explicit)", slug, src)
	}
}

func TestResolveSlug_MatchesCwdCacheDir(t *testing.T) {
	data := t.TempDir()
	// Use macOS-safe eval of symlinks (TempDir under /var -> /private/var).
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	slug := encodeSlug(proj)
	if err := os.MkdirAll(filepath.Join(data, "graphcache", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, proj, func() {
		got, src := h.resolveSlug("")
		if got != slug || src != slugMatched {
			t.Fatalf("got (%q,%v), want (%q, matched)", got, src, slug)
		}
	})
}

func TestResolveSlug_MatchesAncestorFromSubdir(t *testing.T) {
	data := t.TempDir()
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	sub := filepath.Join(proj, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	slug := encodeSlug(proj)
	if err := os.MkdirAll(filepath.Join(data, "graphcache", slug), 0o755); err != nil {
		t.Fatal(err)
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, sub, func() {
		got, src := h.resolveSlug("")
		if got != slug || src != slugMatched {
			t.Fatalf("from subdir got (%q,%v), want (%q, matched)", got, src, slug)
		}
	})
}

func TestResolveSlug_DerivesWhenNoCache(t *testing.T) {
	data := t.TempDir() // empty: no graphcache at all
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, proj, func() {
		got, src := h.resolveSlug("")
		if got != encodeSlug(proj) || src != slugDerived {
			t.Fatalf("got (%q,%v), want (%q, derived)", got, src, encodeSlug(proj))
		}
	})
}
