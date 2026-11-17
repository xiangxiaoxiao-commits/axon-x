package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"axon/internal/db"
	"axon/internal/graph"
	"axon/internal/retrieve"
)

// Knowledge ages. Environment details — which IP answers, which namespace a pod
// lives in, which branch is deployed — are true for weeks and then silently
// wrong once the environment is recycled. A card that still asserts "dev-120
// runs feature/3.21-switch" is worse than no card at all: it is confidently
// false. These routines let the age be visible and the stale cards be removed.

// StaleEntity is one card considered too old to trust, with the reason so the
// user can judge before anything is deleted.
type StaleEntity struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	ObservedAt int64  `json:"observedAt"` // unix millis of the newest fact
	AgeDays    int    `json:"ageDays"`
	Facts      int    `json:"facts"`
	Reason     string `json:"reason"` // "environment" | "age"
	Sample     string `json:"sample,omitempty"`
}

// PruneResult reports what a prune would do (dry run) or did.
type PruneResult struct {
	Candidates []StaleEntity `json:"candidates"`
	Removed    int           `json:"removed"`
	Total      int           `json:"total"`
	BackupDir  string        `json:"backupDir,omitempty"`
}

const (
	// DefaultPruneDays is how old an ordinary card must be before it is offered
	// for deletion.
	DefaultPruneDays = 60
	// EnvironmentTTLDays is the much shorter horizon applied to facts that name
	// a live environment: these rot as soon as the environment is recycled.
	EnvironmentTTLDays = 21
)

var environmentHints = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),        // IP
	regexp.MustCompile(`(?i)\b(dev|sit|uat|prod|stg|pre|gray)[-_]?\d`),   // environment编号
	regexp.MustCompile(`(?i)\b(k8s|cluster|namespace|pod|node|ingress)\b`),
	regexp.MustCompile(`(?i)\b(feature|release|hotfix)/[\w.\-]+`),         // 分支
	regexp.MustCompile(`\b(端口|nodeport|port)\s*[:：=]?\s*\d{2,5}\b`),
	regexp.MustCompile(`(?i)\b(镜像|image)\s*[:：]\s*[\w.\-/]+:\w+`),
}

// IsEnvironmentKnowledge reports whether a card is mostly about a live
// environment (host, namespace, branch, image), i.e. knowledge with a short
// shelf life rather than a durable decision.
func IsEnvironmentKnowledge(e graph.Entity) bool {
	text := e.Name + "\n" + strings.Join(e.Observations, "\n")
	for _, re := range environmentHints {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// entityObservedAt returns the newest fact timestamp on a card, or 0 when the
// card carries no timing information at all (legacy caches).
func entityObservedAt(e graph.Entity) int64 {
	var newest int64
	graph.NormalizeEntityAssertions(&e)
	for _, a := range e.Assertions {
		if a.ObservedAt > newest {
			newest = a.ObservedAt
		}
	}
	return newest
}

// PruneStaleKnowledge removes cards whose newest fact is older than the cutoff.
// Environment-specific cards get a much shorter horizon because they expire on
// their own. dryRun only reports.
//
// Nothing is written until the caller opts in, and every touched cache file is
// copied to backup/prune-<timestamp>/ first: deleting knowledge is irreversible
// otherwise.
func (a *App) PruneStaleKnowledge(projectSlug string, days int, dryRun bool) (PruneResult, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return PruneResult{}, err
	}
	if days <= 0 {
		days = DefaultPruneDays
	}
	now := time.Now()

	g, err := retrieve.AssembleGraph(dataDir, projectSlug)
	if err != nil {
		return PruneResult{}, err
	}
	res := PruneResult{Total: len(g.Entities)}

	ageCutoff := now.AddDate(0, 0, -days).UnixMilli()
	envCutoff := now.AddDate(0, 0, -EnvironmentTTLDays).UnixMilli()

	stale := map[string]StaleEntity{}
	for _, e := range g.Entities {
		at := entityObservedAt(e)
		if at == 0 {
			continue // no timing information: never auto-delete
		}
		age := int(now.Sub(time.UnixMilli(at)).Hours() / 24)
		env := IsEnvironmentKnowledge(e)
		switch {
		case env && at < envCutoff:
			stale[e.Name] = StaleEntity{
				Name: e.Name, Type: e.Type, ObservedAt: at, AgeDays: age,
				Facts: len(e.Observations), Reason: "environment", Sample: firstFact(e),
			}
		case !env && at < ageCutoff:
			stale[e.Name] = StaleEntity{
				Name: e.Name, Type: e.Type, ObservedAt: at, AgeDays: age,
				Facts: len(e.Observations), Reason: "age", Sample: firstFact(e),
			}
		}
	}

	for _, s := range stale {
		res.Candidates = append(res.Candidates, s)
	}
	sort.Slice(res.Candidates, func(i, j int) bool {
		if res.Candidates[i].AgeDays != res.Candidates[j].AgeDays {
			return res.Candidates[i].AgeDays > res.Candidates[j].AgeDays
		}
		return res.Candidates[i].Name < res.Candidates[j].Name
	})

	if dryRun || len(res.Candidates) == 0 {
		return res, nil
	}

	backup := filepath.Join(dataDir, "backup", "prune-"+now.Format("20060102-150405"))
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return res, fmt.Errorf("创建备份目录: %w", err)
	}
	res.BackupDir = backup

	names := make(map[string]bool, len(stale))
	for n := range stale {
		names[strings.ToLower(strings.TrimSpace(n))] = true
	}
	removed := 0
	if err := rewriteCaches(dataDir, projectSlug, backup, func(c *graph.SessionCache) bool {
		kept := c.Entities[:0]
		for _, e := range c.Entities {
			if names[strings.ToLower(strings.TrimSpace(e.Name))] {
				removed++
				continue
			}
			kept = append(kept, e)
		}
		c.Entities = kept
		keptRels := c.Relations[:0]
		for _, r := range c.Relations {
			if names[strings.ToLower(strings.TrimSpace(r.From))] || names[strings.ToLower(strings.TrimSpace(r.To))] {
				continue
			}
			keptRels = append(keptRels, r)
		}
		c.Relations = keptRels
		return true
	}); err != nil {
		return res, err
	}
	res.Removed = removed
	return res, nil
}

func firstFact(e graph.Entity) string {
	if len(e.Observations) > 0 {
		s := e.Observations[0]
		if len(s) > 120 {
			s = s[:120] + "…"
		}
		return s
	}
	return ""
}

// rewriteCaches applies fn to every cache file of a namespace, backing up each
// file it changes. A cache whose contents end up empty is deleted.
func rewriteCaches(dataDir, namespace, backupDir string, fn func(*graph.SessionCache) bool) error {
	dir := filepath.Join(dataDir, "graphcache", namespace)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cache graph.SessionCache
		if json.Unmarshal(raw, &cache) != nil {
			continue
		}
		if !fn(&cache) {
			continue
		}
		if err := os.MkdirAll(backupDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(backupDir, entry.Name()), raw, 0o644)
		}
		if len(cache.Entities) == 0 && len(cache.Relations) == 0 && len(cache.Chunks) == 0 {
			_ = os.Remove(path)
			continue
		}
		out, _ := json.MarshalIndent(cache, "", "  ")
		_ = os.WriteFile(path, out, 0o644)
	}
	return nil
}

// RestampCacheTimes rewrites the observedAt of existing caches to the timestamp
// of the session they came from. Caches written before session-time provenance
// carry the distillation time, which makes every card look brand new and makes
// age-based pruning useless. Idempotent; safe to run on every startup.
func (a *App) RestampCacheTimes(projectSlug string) (int, error) {
	dataDir, err := db.AppDataDir()
	if err != nil {
		return 0, err
	}
	plan, err := planIndex(projectSlug)
	if err != nil {
		return 0, err
	}
	at := make(map[string]int64, len(plan.Sources))
	for _, src := range plan.Sources {
		at[src.Meta.ID] = src.Meta.UpdatedAt
	}

	touched := 0
	dir := filepath.Join(dataDir, "graphcache", plan.Namespace)
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		return 0, nil
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		var cache graph.SessionCache
		if json.Unmarshal(raw, &cache) != nil {
			continue
		}
		sid := strings.TrimSuffix(cache.SessionID, "-global")
		when, ok := at[sid]
		if !ok || when <= 0 {
			continue
		}
		changed := false
		for i := range cache.Entities {
			graph.NormalizeEntityAssertions(&cache.Entities[i])
			for j := range cache.Entities[i].Assertions {
				if cache.Entities[i].Assertions[j].ObservedAt != when {
					cache.Entities[i].Assertions[j].ObservedAt = when
					changed = true
				}
			}
		}
		if !changed {
			continue
		}
		out, _ := json.MarshalIndent(cache, "", "  ")
		if os.WriteFile(path, out, 0o644) == nil {
			touched++
		}
	}
	return touched, nil
}
