//go:build realdata

// Namespace-clustering probe (excluded from normal builds by the realdata tag).
//
//	go test -tags realdata ./internal/graph/ -run TestRealDataCluster -v
package graph

import (
	"fmt"
	"os"
	"testing"
)

func TestRealDataCluster(t *testing.T) {
	dir := realDataDir()
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no live axon data at %s", dir)
	}
	clusters, links, err := ClusterNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Printf("\n=== 自动归组结果（阈值 %.0f%% 重叠）===\n", ClusterMinOverlap*100)
	fmt.Printf("%-16s %-34s %7s %7s %8s\n", "知识域", "成员命名空间", "实体", "关系", "独立贡献者")
	for _, c := range clusters {
		members := ""
		for i, s := range c.Slugs {
			if i > 0 {
				members += " + "
			}
			members += s
		}
		note := ""
		if c.Redundant {
			note = "  ← 完全冗余，可合并存储"
		}
		fmt.Printf("%-16s %-34s %7d %7d %8d%s\n",
			c.Name, members, c.Entities, c.Relations, c.SoleSource, note)
	}

	fmt.Printf("\n=== 所有被比较的命名空间对（分组依据，可审计）===\n")
	for _, l := range links {
		mark := "分开"
		if l.Merged {
			mark = "合并"
		}
		fmt.Printf("  %-14s %-14s 共享 %5d / 较小侧 %5d = %5.1f%%  %s\n",
			l.A, l.B, l.Shared, l.Smaller, l.Overlap*100, mark)
	}

	// Summarize what the user stops having to do by hand.
	before, after := 0, len(clusters)
	for _, l := range links {
		_ = l
	}
	seen := map[string]bool{}
	for _, l := range links {
		seen[l.A] = true
		seen[l.B] = true
	}
	before = len(seen)
	fmt.Printf("\n=== 效果 ===\n")
	fmt.Printf("  命名空间 %d 个 → 自动归为 %d 个知识域\n", before, after)
	for _, c := range clusters {
		if len(c.Slugs) > 1 {
			fmt.Printf("  其中「%s」由 %d 个命名空间合成：%v\n", c.Name, len(c.Slugs), c.Slugs)
		}
	}
}
