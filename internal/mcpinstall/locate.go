package mcpinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// mcpBinaryName is the axon-mcp executable's file name for the current OS.
func mcpBinaryName() string {
	if runtime.GOOS == "windows" {
		return "axon-mcp.exe"
	}
	return "axon-mcp"
}

// LocateMCPBinary finds the axon-mcp executable to register. It looks, in order:
//   - next to the running executable (the packaged layout: axon-mcp ships
//     beside the main binary inside the .app / install dir);
//   - in ./build/bin relative to the working dir (the `wails dev` layout).
//
// It returns an absolute path so the entry written into Claude Code's config
// keeps working regardless of Claude Code's own working directory.
func LocateMCPBinary() (string, error) {
	name := mcpBinaryName()

	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), name)
		if isFile(cand) {
			return filepath.Abs(cand)
		}
	}

	if wd, err := os.Getwd(); err == nil {
		cand := filepath.Join(wd, "build", "bin", name)
		if isFile(cand) {
			return filepath.Abs(cand)
		}
	}

	return "", fmt.Errorf("%s not found next to the app or in build/bin; build it with `go build -o build/bin/%s ./cmd/axon-mcp`", name, name)
}

// isFile reports whether path exists and is a regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
