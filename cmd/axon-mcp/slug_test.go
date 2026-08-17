package main

import (
	"os"
	"path/filepath"
	"testing"
)

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

func TestResolveSlug_ReadsAxonProjectFile(t *testing.T) {
	data := t.TempDir()
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	// Write .axon-project in the project root.
	if err := os.WriteFile(filepath.Join(proj, ".axon-project"), []byte("gaia\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, proj, func() {
		got, src := h.resolveSlug("")
		if got != "gaia" || src != slugMapped {
			t.Fatalf("got (%q,%v), want (gaia, mapped)", got, src)
		}
	})
}

func TestResolveSlug_FindsAxonProjectInAncestor(t *testing.T) {
	data := t.TempDir()
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	sub := filepath.Join(proj, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".axon-project"), []byte("glite"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, sub, func() {
		got, src := h.resolveSlug("")
		if got != "glite" || src != slugMapped {
			t.Fatalf("from subdir got (%q,%v), want (glite, mapped)", got, src)
		}
	})
}

func TestResolveSlug_UnknownWhenNoFile(t *testing.T) {
	data := t.TempDir()
	// Use a temp dir with no .axon-project anywhere up.
	proj := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(proj); err == nil {
		proj = resolved
	}
	h := &toolHandler{dataDir: data}
	withCwd(t, proj, func() {
		got, src := h.resolveSlug("")
		if got != "" || src != slugUnknown {
			t.Fatalf("got (%q,%v), want (\"\", unknown)", got, src)
		}
	})
}

func TestParseProjectFile(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"gaia\n", "gaia"},
		{"  glite  \n\n", "glite"},
		{"\n\n  axon  ", "axon"},
		{"", ""},
		{"  \n  \n  ", ""},
	}
	for _, tc := range cases {
		got := parseProjectFile([]byte(tc.input))
		if got != tc.want {
			t.Errorf("parseProjectFile(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNamespaceDirs(t *testing.T) {
	data := t.TempDir()
	gc := filepath.Join(data, "graphcache")
	for _, ns := range []string{"gaia", "glite", "axon", "_global_"} {
		if err := os.MkdirAll(filepath.Join(gc, ns), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := &toolHandler{dataDir: data}
	dirs := h.namespaceDirs()
	if len(dirs) != 4 {
		t.Fatalf("got %d dirs, want 4: %v", len(dirs), dirs)
	}
}
