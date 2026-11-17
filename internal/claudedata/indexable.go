package claudedata

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// DecodeProjectSlug turns a Claude directory slug back into a readable path.
// Best-effort only (the encoding maps both "/" and "-" to "-"), used for
// display when no transcript recorded an exact cwd.
func DecodeProjectSlug(slug string) string {
	return decodeSlug(slug)
}

// ListIndexableProjects aggregates every project Axon could distil knowledge
// from, across all three agent stores. Each result is keyed by the namespace
// IndexProject expects, so the picker can offer projects that have no knowledge
// yet — without which a freshly reset knowledge base leaves nothing clickable.
//
// It deliberately reads only cheap metadata (a Codex rollout's first line, a
// Buddy project directory name) instead of parsing transcripts: this runs on
// every picker refresh, and a full parse of ~700 sessions costs seconds.
func ListIndexableProjects() []Project {
	type agg struct {
		path     string
		count    int
		updated  int64
		explicit string // namespace recorded by a marker file, when known
	}
	byNS := map[string]*agg{}

	add := func(ns, path string, count int, updated int64, explicit bool) {
		ns = strings.TrimSpace(ns)
		if ns == "" || count <= 0 {
			return
		}
		// Only projects that opt in via a .axon-project marker are offered.
		// Every agent also records throwaway working directories (a WorkBuddy
		// run folder, a Documents/Codex/2026-06-03 scratch dir, even "/"), and
		// listing those turned the picker into 120+ meaningless entries.
		if !explicit {
			return
		}
		a := byNS[ns]
		if a == nil {
			a = &agg{path: path}
			byNS[ns] = a
		}
		a.count += count
		if updated > a.updated {
			a.updated = updated
			if path != "" {
				a.path = path
			}
		}
		a.explicit = ns
	}

	// Claude Code: already resolves namespace from each transcript's cwd.
	if projects, err := ListProjects(); err == nil {
		for _, p := range projects {
			ns := p.Namespace
			if ns == "" {
				ns = p.Slug
			}
			add(ns, p.Path, p.SessionCount, p.UpdatedAt, p.Namespace != "")
		}
	}

	// Codex rollouts are filed by date, so the project only exists inside the
	// session_meta record's cwd.
	if root, err := codexRoot(); err == nil {
		for _, dir := range []string{"sessions", "archived_sessions"} {
			_ = filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
					return nil
				}
				cwd := codexSessionCwd(path)
				if cwd == "" {
					return nil
				}
				ns := ResolveNamespace(cwd)
				explicit := ns != ""
				if ns == "" {
					ns = filepath.Base(cwd)
				}
				add(ns, cwd, 1, info.ModTime().UnixMilli(), explicit)
				return nil
			})
		}
	}

	// Buddy agents file sessions one directory per project, named by slug.
	for _, agent := range buddyAgents {
		root, err := buddyRoot(agent)
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, "projects"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root, "projects", e.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			count := 0
			var newest int64
			for _, f := range files {
				if !strings.HasSuffix(f.Name(), ".jsonl") {
					continue
				}
				count++
				if info, ierr := f.Info(); ierr == nil {
					if ms := info.ModTime().UnixMilli(); ms > newest {
						newest = ms
					}
				}
			}
			if count == 0 {
				continue
			}
			cwd := decodeSlug(e.Name())
			ns := ResolveNamespace(cwd)
			explicit := ns != ""
			if ns == "" {
				ns = e.Name()
			}
			add(ns, cwd, count, newest, explicit)
		}
	}

	out := make([]Project, 0, len(byNS))
	for ns, a := range byNS {
		out = append(out, Project{
			Slug:         ns,
			Path:         a.path,
			Namespace:    ns,
			SessionCount: a.count,
			UpdatedAt:    a.updated,
		})
	}
	return out
}

// codexSessionCwd reads only the leading session_meta record of a rollout file.
func codexSessionCwd(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 16*1024), 1024*1024)
	for i := 0; i < 3 && sc.Scan(); i++ {
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Cwd string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		if ev.Type == "session_meta" && ev.Payload.Cwd != "" {
			return ev.Payload.Cwd
		}
	}
	return ""
}
