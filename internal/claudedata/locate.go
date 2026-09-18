package claudedata

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Locating a transcript by session id used to mean walking a directory tree on
// every call, and those calls sit on the UI's hot path: opening one session
// resolves the same file twice (once for the "where I left off" card, once for
// the transcript), and for Codex the walk covers the whole session store.
//
// The cache below remembers id -> path. Every hit is re-validated with a stat,
// so a transcript that was moved or deleted is re-walked rather than reported at
// a stale path — and a miss is still a walk, so correctness never depends on the
// cache being warm.
const maxLocateEntries = 20000

// Cache namespaces. Each agent derives a session's identity differently (Claude
// from the file name, Codex and Buddy from a record inside the file), so the
// same id string must not resolve across agents.
const (
	locateKindClaude = "claude"
	locateKindCodex  = "codex"
	locateKindBuddy  = "buddy"
)

type locateKey struct {
	kind string
	id   string
}

var locateCache = struct {
	sync.Mutex
	m map[locateKey]string
}{m: make(map[locateKey]string)}

// locateCached returns the remembered path for (kind, id) while the file is
// still present.
func locateCached(kind, id string) string {
	if id == "" {
		return ""
	}
	locateCache.Lock()
	path := locateCache.m[locateKey{kind, id}]
	locateCache.Unlock()
	if path == "" {
		return ""
	}
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return ""
	}
	return path
}

// rememberLocate stores a resolved path. A full map simply resets instead of
// growing without bound; that costs one walk and no correctness.
func rememberLocate(kind, id, path string) {
	if id == "" || path == "" {
		return
	}
	locateCache.Lock()
	defer locateCache.Unlock()
	if len(locateCache.m) >= maxLocateEntries {
		locateCache.m = make(map[locateKey]string)
	}
	locateCache.m[locateKey{kind, id}] = path
}

// sessionFileNameMatches classifies how well a transcript file name corresponds
// to a session id:
//
//	exact -> the id is the file's own name ("<id>.jsonl") or its trailing
//	         component ("rollout-2026-08-28T11-00-30-<id>.jsonl")
//	loose -> the id merely appears inside the name
//
// Matching loosely on its own used to be the only test, so a session whose id is
// a prefix of another session's id could resolve to the wrong transcript. Callers
// prefer exact, and keep loose only as a fallback for unusual layouts.
func sessionFileNameMatches(base, id string) (exact, loose bool) {
	if id == "" || !strings.HasSuffix(base, ".jsonl") {
		return false, false
	}
	name := strings.TrimSuffix(base, ".jsonl")
	switch {
	case name == id, strings.HasSuffix(name, "-"+id):
		return true, true
	case strings.Contains(name, id):
		return false, true
	}
	return false, false
}

// findSessionFileUnder walks each root in turn looking for the transcript that
// belongs to id. An exact file-name match ends the search immediately; a loose
// match is only returned once every root has been searched without one.
func findSessionFileUnder(id string, roots ...string) string {
	if id == "" {
		return ""
	}
	loose := ""
	for _, root := range roots {
		if root == "" {
			continue
		}
		exact := ""
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			isExact, isLoose := sessionFileNameMatches(info.Name(), id)
			switch {
			case isExact:
				exact = path
				return filepath.SkipAll
			case isLoose && loose == "":
				loose = path
			}
			return nil
		})
		if exact != "" {
			return exact
		}
	}
	return loose
}
