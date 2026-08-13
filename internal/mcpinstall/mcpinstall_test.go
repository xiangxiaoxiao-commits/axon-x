package mcpinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeHome points HOME (and USERPROFILE on Windows) at a temp dir so tests hit
// a throwaway ~/.claude.json instead of the developer's real one.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	return home
}

// fakeMCPBin creates a stand-in executable so Install's existence check passes.
func fakeMCPBin(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), mcpBinaryName())
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstall_FreshConfig(t *testing.T) {
	fakeHome(t)
	bin := fakeMCPBin(t)

	if err := Install(bin); err != nil {
		t.Fatalf("Install: %v", err)
	}

	st, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !st.Installed {
		t.Fatal("Installed = false after Install, want true")
	}
	if st.Command != bin {
		t.Fatalf("Command = %q, want %q", st.Command, bin)
	}
}

func TestInstall_PreservesOtherFields(t *testing.T) {
	home := fakeHome(t)
	bin := fakeMCPBin(t)

	// Seed a config that already has unrelated top-level keys and another MCP
	// server, mirroring a real ~/.claude.json.
	seed := map[string]any{
		"numStartups": 42,
		"theme":       "dark",
		"mcpServers": map[string]any{
			"codegraph": map[string]any{"type": "stdio", "command": "codegraph", "args": []string{"serve", "--mcp"}},
		},
	}
	writeSeed(t, filepath.Join(home, ".claude.json"), seed)

	if err := Install(bin); err != nil {
		t.Fatalf("Install: %v", err)
	}

	root := readSeed(t, filepath.Join(home, ".claude.json"))
	// Unrelated keys survive.
	if root["numStartups"] == nil || root["theme"] == nil {
		t.Fatalf("unrelated top-level keys were dropped: %v", root)
	}
	servers := root["mcpServers"].(map[string]any)
	// Pre-existing server survives.
	if _, ok := servers["codegraph"]; !ok {
		t.Fatal("existing codegraph server was dropped")
	}
	// Our server was added.
	if _, ok := servers[ServerName]; !ok {
		t.Fatalf("%s not added", ServerName)
	}
}

func TestInstall_UpdatesInPlace(t *testing.T) {
	fakeHome(t)
	bin1 := fakeMCPBin(t)
	if err := Install(bin1); err != nil {
		t.Fatalf("Install 1: %v", err)
	}
	bin2 := fakeMCPBin(t)
	if err := Install(bin2); err != nil {
		t.Fatalf("Install 2: %v", err)
	}
	st, _ := GetStatus()
	if st.Command != bin2 {
		t.Fatalf("Command = %q after re-install, want %q", st.Command, bin2)
	}
}

func TestUninstall_RemovesOnlyOurEntry(t *testing.T) {
	home := fakeHome(t)
	bin := fakeMCPBin(t)
	seed := map[string]any{
		"mcpServers": map[string]any{
			"codegraph": map[string]any{"type": "stdio", "command": "codegraph"},
		},
	}
	writeSeed(t, filepath.Join(home, ".claude.json"), seed)
	if err := Install(bin); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	st, _ := GetStatus()
	if st.Installed {
		t.Fatal("still Installed after Uninstall")
	}
	servers := readSeed(t, filepath.Join(home, ".claude.json"))["mcpServers"].(map[string]any)
	if _, ok := servers["codegraph"]; !ok {
		t.Fatal("Uninstall clobbered the codegraph server")
	}
}

func TestUninstall_NoConfigIsNoError(t *testing.T) {
	fakeHome(t)
	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall with no config: got %v, want nil", err)
	}
}

func TestGetStatus_NoConfig(t *testing.T) {
	fakeHome(t)
	st, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus with no config: %v", err)
	}
	if st.Installed {
		t.Fatal("Installed = true with no config")
	}
}

func TestInstall_MissingBinaryFails(t *testing.T) {
	fakeHome(t)
	if err := Install(filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("Install with missing binary: got nil error, want failure")
	}
}

// --- seed helpers ---

func writeSeed(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSeed(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
