package codegraph

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// maxFileBytes caps the size of a file whose content we read (imports/AST).
// Larger files still get a `file` node but no content is parsed — they are
// usually generated or data files whose body is noise.
const maxFileBytes = 256 * 1024

// sourceExts is the whitelist of source extensions the generic layer processes.
// Non-source files (.md/.json/.png/.lock) are skipped entirely.
var sourceExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".java": true, ".rs": true, ".rb": true, ".php": true,
	".c": true, ".cc": true, ".cpp": true, ".h": true, ".hpp": true,
	".cs": true, ".kt": true, ".swift": true, ".scala": true, ".m": true,
}

// ListSourceFiles walks repoDir and returns repo-relative source file paths,
// skipping generated/vendored directories, lockfiles, secret files and
// non-source extensions (see docs/TECH_CODEGRAPH.md §3.1). Paths use forward
// slashes so entity names are stable across platforms.
func ListSourceFiles(repoDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(repoDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		rel, relErr := filepath.Rel(repoDir, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !sourceExts[strings.ToLower(filepath.Ext(rel))] {
			return nil
		}
		if skipPathGenerated(rel) || skipPathSecret(rel) {
			return nil
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// skipDir matches directories whose contents are noise for a code graph.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", ".next",
		"target", ".idea", ".vscode", "__pycache__", ".gradle", "out":
		return true
	}
	return false
}

// ReadFile reads a repo-relative source file's content bounded by maxFileBytes,
// returning "" when missing or too large. Exported for the app layer's LLM pass.
func ReadFile(repoDir, rel string) string { return readFileBounded(repoDir, rel) }

// readFileBounded reads a whitelisted file's content, or "" when it is missing
// or exceeds maxFileBytes (kept as a node without parsed content).
func readFileBounded(repoDir, rel string) string {
	full := filepath.Join(repoDir, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil || info.Size() > maxFileBytes {
		return ""
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	return string(data)
}

// --- path blacklists (mirrors gitx's skipPath* so code extraction applies the
// same generated/secret filtering; kept local to avoid exporting from gitx) ---

func skipPathGenerated(p string) bool {
	base := path.Base(p)
	switch base {
	case "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
		"Cargo.lock", "poetry.lock", "composer.lock", "Gemfile.lock":
		return true
	}
	if strings.HasSuffix(base, ".lock") || strings.HasSuffix(base, ".min.js") ||
		strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".snap") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "node_modules", "vendor", "dist", "build", ".next", "target":
			return true
		}
	}
	return false
}

func skipPathSecret(p string) bool {
	base := strings.ToLower(path.Base(p))
	if base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	for _, suf := range []string{".pem", ".key", ".p12", ".pfx", ".keystore", ".jks"} {
		if strings.HasSuffix(base, suf) {
			return true
		}
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") {
		return true
	}
	if strings.Contains(base, "credential") || strings.Contains(base, "secret") {
		return true
	}
	return false
}
