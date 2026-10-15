//go:build realdata

// This probe is excluded from normal builds (see the realdata build tag). It
// copies the live Axon data directory to a temp dir, runs the cache migration on
// the COPY, and prints before/after quality metrics. It never writes to the real
// data directory.
//
//	go test -tags realdata ./internal/graph/ -run TestRealDataMigrationProbe -v
package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

type probeStats struct {
	relations    int
	withSources  int
	distinctLbl  map[string]bool
	oversized    int
	foldable     int
	foldEvidence int
	staleCaches  int
	allCaches    int
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyTree(srcRoot, dstRoot string) error {
	return filepath.Walk(srcRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(p) != ".json" {
			return nil
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return nil
		}
		return copyFile(p, filepath.Join(dstRoot, rel))
	})
}

func measure(dataDir string) (map[string]*probeStats, *probeStats) {
	out := map[string]*probeStats{}
	total := &probeStats{distinctLbl: map[string]bool{}}
	slugs, _ := CacheNamespaces(dataDir)
	for _, slug := range slugs {
		st := &probeStats{distinctLbl: map[string]bool{}}
		caches, _ := LoadAllCache(dataDir, slug)
		st.allCaches = len(caches)
		total.allCaches += len(caches)
		for _, c := range caches {
			if c.Schema < CacheSchema {
				st.staleCaches++
				total.staleCaches++
			}
			for _, r := range c.Relations {
				st.relations++
				total.relations++
				if len(r.Sources) > 0 {
					st.withSources++
					total.withSources++
				}
				lbl := r.Label
				st.distinctLbl[lbl] = true
				total.distinctLbl[lbl] = true
				if IsOversizedRelationLabel(lbl) {
					st.oversized++
					total.oversized++
				}
				if _, _, changed := CanonicalRelationLabel(lbl); changed {
					st.foldable++
					total.foldable++
				}
				for _, e := range r.Evidence {
					if _, _, changed := CanonicalRelationLabel(e); changed {
						st.foldEvidence++
						total.foldEvidence++
						break
					}
				}
			}
		}
		out[slug] = st
	}
	return out, total
}

func realDataDir() string {
	if v := os.Getenv("AXON_DATA_DIR"); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "axon")
}

func TestRealDataMigrationProbe(t *testing.T) {
	src := realDataDir()
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no live axon data at %s (set AXON_DATA_DIR to override)", src)
	}
	tmp := t.TempDir()
	started := time.Now()
	for _, sub := range []string{"graphcache", "graphs"} {
		if err := copyTree(filepath.Join(src, sub), filepath.Join(tmp, sub)); err != nil {
			t.Fatalf("copy %s: %v", sub, err)
		}
	}
	t.Logf("已复制真实数据到临时目录，用时 %s", time.Since(started).Round(time.Millisecond))

	before, beforeTotal := measure(tmp)
	upgraded, perNS := MigrateAllStaleCaches(tmp, time.Now())
	after, afterTotal := measure(tmp)

	fmt.Printf("\n=== 迁移前 ===\n")
	printProbe(before, beforeTotal)
	fmt.Printf("\n=== 迁移后（共升级 %d 个缓存文件）===\n", upgraded)
	printProbe(after, afterTotal)

	fmt.Printf("\n=== 每个命名空间升级数 ===\n")
	keys := make([]string, 0, len(perNS))
	for k := range perNS {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-14s %d\n", k, perNS[k])
	}

	fmt.Printf("\n=== 关键变化 ===\n")
	fmt.Printf("  关系带 sources   %d → %d  (%.1f%% → %.1f%%)\n",
		beforeTotal.withSources, afterTotal.withSources,
		pct(beforeTotal.withSources, beforeTotal.relations),
		pct(afterTotal.withSources, afterTotal.relations))
	fmt.Printf("  可折叠 label      %d → %d（迁移后应为 0）\n", beforeTotal.foldable, afterTotal.foldable)
	fmt.Printf("  被折叠的原文进 evidence  %d 条\n", afterTotal.foldEvidence)
	fmt.Printf("  distinct label    %d → %d\n", len(beforeTotal.distinctLbl), len(afterTotal.distinctLbl))
	fmt.Printf("  超长 label        %d → %d\n", beforeTotal.oversized, afterTotal.oversized)
	fmt.Printf("  陈旧缓存          %d → %d\n", beforeTotal.staleCaches, afterTotal.staleCaches)
	fmt.Printf("  关系总数          %d → %d（必须相等，否则迁移写重复了）\n",
		beforeTotal.relations, afterTotal.relations)
	fmt.Printf("  缓存文件数        %d → %d（必须相等，否则迁移写重复了）\n",
		beforeTotal.allCaches, afterTotal.allCaches)
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

// TestRealDataMigrateInPlace applies the migration to the LIVE data directory.
// It is opt-in via AXON_MIGRATE_LIVE=1 because it rewrites real caches; take a
// backup of the data directory first. The operation is additive (source stamps +
// label folds with the original kept as evidence) and idempotent, so re-running
// is a no-op.
//
//	AXON_MIGRATE_LIVE=1 go test -tags realdata ./internal/graph/ -run TestRealDataMigrateInPlace -v
func TestRealDataMigrateInPlace(t *testing.T) {
	if os.Getenv("AXON_MIGRATE_LIVE") != "1" {
		t.Skip("set AXON_MIGRATE_LIVE=1 to migrate the live data directory in place")
	}
	dir := realDataDir()
	before, beforeTotal := measure(dir)
	upgraded, perNS := MigrateAllStaleCaches(dir, time.Now())
	after, afterTotal := measure(dir)

	fmt.Printf("\n=== 原地迁移 %s ===\n", dir)
	fmt.Printf("升级缓存文件 %d 个\n", upgraded)
	for slug, n := range perNS {
		fmt.Printf("  %-14s %d\n", slug, n)
	}
	fmt.Printf("关系带 sources  %d → %d (%.1f%% → %.1f%%)\n",
		beforeTotal.withSources, afterTotal.withSources,
		pct(beforeTotal.withSources, beforeTotal.relations),
		pct(afterTotal.withSources, afterTotal.relations))
	fmt.Printf("陈旧缓存        %d → %d\n", beforeTotal.staleCaches, afterTotal.staleCaches)
	fmt.Printf("关系总数        %d → %d\n", beforeTotal.relations, afterTotal.relations)
	fmt.Printf("缓存文件数      %d → %d\n", beforeTotal.allCaches, afterTotal.allCaches)
	_ = before
	_ = after
}

func printProbe(per map[string]*probeStats, total *probeStats) {
	slugs := make([]string, 0, len(per))
	for k := range per {
		slugs = append(slugs, k)
	}
	sort.Strings(slugs)
	fmt.Printf("%-14s %7s %9s %10s %8s %9s %7s\n",
		"命名空间", "关系", "带sources", "distinct", "超长", "陈旧缓存", "缓存数")
	for _, s := range slugs {
		st := per[s]
		if st.relations == 0 && st.allCaches == 0 {
			continue
		}
		fmt.Printf("%-14s %7d %9d %10d %8d %9d %7d\n",
			s, st.relations, st.withSources, len(st.distinctLbl), st.oversized, st.staleCaches, st.allCaches)
	}
	fmt.Printf("%-14s %7d %9d %10d %8d %9d %7d\n",
		"合计", total.relations, total.withSources, len(total.distinctLbl), total.oversized, total.staleCaches, total.allCaches)
}

var _ = json.Marshal
