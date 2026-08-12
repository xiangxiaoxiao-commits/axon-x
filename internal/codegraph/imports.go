package codegraph

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Import regexes for the language-agnostic layer. Each captures the imported
// module/path in group 1. Deliberately simple: they cover the mainstream import
// syntaxes so any language yields file -> dependency edges without an AST.
var (
	reJSImport  = regexp.MustCompile(`(?m)^\s*import\b.*?from\s+['"]([^'"]+)['"]`)
	reJSRequire = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	rePyImport  = regexp.MustCompile(`(?m)^\s*import\s+([A-Za-z0-9_.]+)`)
	rePyFrom    = regexp.MustCompile(`(?m)^\s*from\s+([A-Za-z0-9_.]+)\s+import\b`)
	reJavaImp   = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([A-Za-z0-9_.]+)\s*;`)
	reRustUse   = regexp.MustCompile(`(?m)^\s*use\s+([A-Za-z0-9_:]+)`)
	reCInclude  = regexp.MustCompile(`(?m)^\s*#include\s+[<"]([^>"]+)[>"]`)
)

// regexImports returns the import targets of a non-Go source file, chosen by
// extension. Best-effort: unknown extensions yield nothing (the file still has
// a node and a contains edge).
func regexImports(repoDir, rel string) []string {
	src := readFileBounded(repoDir, rel)
	if src == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(rel))
	var res []*regexp.Regexp
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx":
		res = []*regexp.Regexp{reJSImport, reJSRequire}
	case ".py":
		res = []*regexp.Regexp{rePyImport, rePyFrom}
	case ".java", ".kt", ".scala":
		res = []*regexp.Regexp{reJavaImp}
	case ".rs":
		res = []*regexp.Regexp{reRustUse}
	case ".c", ".cc", ".cpp", ".h", ".hpp", ".m":
		res = []*regexp.Regexp{reCInclude}
	default:
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, re := range res {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			t := strings.TrimSpace(m[1])
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}
