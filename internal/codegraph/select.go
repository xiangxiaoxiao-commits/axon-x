package codegraph

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"axon/internal/graph"
)

// SelectKeyFiles ranks source files by importance and returns the top n paths
// for LLM business enrichment (the budgeted, paid stage). Signals: change
// frequency (git log), how many contains/depends edges point into the file's
// package (centrality), entry-point naming, and a mild size preference. Pure
// scoring — safe to call without a model.
func SelectKeyFiles(repoDir string, files []string, rels []graph.Relation, n int) []string {
	if n <= 0 || len(files) == 0 {
		return nil
	}
	freq := changeFrequency(repoDir) // path -> commit count (may be empty)
	inDeg := packageInDegree(rels)   // package dir -> incoming edge count

	type scored struct {
		path  string
		score float64
	}
	ranked := make([]scored, 0, len(files))
	for _, f := range files {
		s := 0.0
		s += 3.0 * float64(freq[f]) // changed often -> core
		s += 1.5 * float64(inDeg[packageName(f)])
		if isEntryPoint(f) {
			s += 4.0
		}
		s += sizePreference(repoDir, f)
		ranked = append(ranked, scored{path: f, score: s})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	if len(ranked) > n {
		ranked = ranked[:n]
	}
	out := make([]string, len(ranked))
	for i, r := range ranked {
		out[i] = r.path
	}
	return out
}

// changeFrequency counts how often each file appears in recent history via
// `git log --name-only`. Empty when the dir is not a git repo (best-effort).
func changeFrequency(repoDir string) map[string]int {
	cmd := exec.Command("git", "-C", repoDir, "log", "--pretty=format:", "--name-only", "-n", "300")
	out, err := cmd.Output()
	if err != nil {
		return map[string]int{}
	}
	freq := map[string]int{}
	for _, ln := range strings.Split(string(out), "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" {
			freq[filepath.ToSlash(ln)]++
		}
	}
	return freq
}

// packageInDegree counts relation targets that resolve to each package dir, a
// proxy for how depended-upon a package is.
func packageInDegree(rels []graph.Relation) map[string]int {
	deg := map[string]int{}
	for _, r := range rels {
		deg[packageName(r.To)]++
	}
	return deg
}

// isEntryPoint flags files whose names/paths suggest an entry or service layer.
func isEntryPoint(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	switch base {
	case "main.go", "app.go", "index.ts", "index.js", "server.go", "server.ts":
		return true
	}
	if strings.HasPrefix(filepath.ToSlash(rel), "cmd/") {
		return true
	}
	for _, kw := range []string{"controller", "service", "handler", "router", "route"} {
		if strings.Contains(base, kw) {
			return true
		}
	}
	return false
}

// sizePreference gives a small bonus to mid-sized files (real content) and
// penalizes tiny or huge ones (stubs / generated). Bounded to keep it a
// tie-breaker, not a dominant signal.
func sizePreference(repoDir, rel string) float64 {
	src := readFileBounded(repoDir, rel)
	n := len(src)
	switch {
	case n == 0:
		return 0
	case n < 200:
		return 0.2
	case n < 12000:
		return 1.0
	default:
		return 0.3
	}
}
