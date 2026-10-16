//go:build realdata

// Alias-merge audit probe (excluded from normal builds by the realdata tag).
// It replays the real caches through the real merge path and reports what alias
// normalization folded together and what it refused, so the "silent sameAs"
// concern is sized against actual data instead of assumed.
//
//	go test -tags realdata ./internal/graph/ -run TestRealDataMergeAudit -v
package graph

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

func TestRealDataMergeAudit(t *testing.T) {
	dir := realDataDir()
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no live axon data at %s", dir)
	}
	slugs, err := CacheNamespaces(dir)
	if err != nil {
		t.Fatal(err)
	}

	type bucket struct {
		folds, refusals, before, after int
		cacheRels, assembledRels       int
		kinds                          map[string]int
	}
	per := map[string]*bucket{}
	total := &bucket{kinds: map[string]int{}}
	var refused, folds []MergeAudit

	for _, slug := range slugs {
		caches, err := LoadAllCache(dir, slug)
		if err != nil || len(caches) == 0 {
			continue
		}
		b := &bucket{kinds: map[string]int{}}
		g := &Graph{ProjectSlug: slug, Entities: []Entity{}, Relations: []Relation{}}
		for _, c := range caches {
			b.before += len(c.Entities)
			b.cacheRels += len(c.Relations)
			rep := g.MergeAudited(c.Entities, c.Relations)
			for _, a := range rep.Folds {
				b.folds++
				total.folds++
				b.kinds[a.MatchKind]++
				total.kinds[a.MatchKind]++
				if len(folds) < 10 {
					folds = append(folds, a)
				}
			}
			for _, a := range rep.Refused {
				b.refusals++
				total.refusals++
				if len(refused) < 25 {
					refused = append(refused, a)
				}
			}
		}
		b.after = len(g.Entities)
		b.assembledRels = len(g.Relations)
		per[slug] = b
		total.before += b.before
		total.after += b.after
		total.cacheRels += b.cacheRels
		total.assembledRels += b.assembledRels
	}

	fmt.Printf("\n=== 关系是否受影响（拒绝合并不应丢边）===\n")
	fmt.Printf("%-14s %10s %10s %10s\n", "命名空间", "缓存中", "组图后", "差")
	relKeys := make([]string, 0, len(per))
	for k := range per {
		relKeys = append(relKeys, k)
	}
	sort.Strings(relKeys)
	for _, k := range relKeys {
		b := per[k]
		if b.cacheRels == 0 {
			continue
		}
		fmt.Printf("%-14s %10d %10d %10d\n", k, b.cacheRels, b.assembledRels,
			b.cacheRels-b.assembledRels)
	}
	fmt.Printf("%-14s %10d %10d %10d\n", "合计", total.cacheRels, total.assembledRels,
		total.cacheRels-total.assembledRels)
	fmt.Printf("  （差额来自三元组去重与端点无法解析，属既有行为；\n")
	fmt.Printf("    拒绝合并只会让实体更多、解析键更全，不应增加丢失）\n")

	// Why does assembly drop relations? Classify the losses using the same
	// name/alias index Merge itself resolves through, so the answer is measured
	// rather than guessed. This is a pre-existing property of assembly (the
	// relation-resolution code is untouched by the merge change); it is reported
	// here because a 64% loss in a namespace is worth knowing about.
	fmt.Printf("\n=== 关系丢失归因（按命名空间，用与 Merge 相同的解析口径）===\n")
	fmt.Printf("%-14s %8s %9s %8s %8s %8s %9s\n",
		"命名空间", "缓存中", "端点缺From", "端点缺To", "自环", "重复三元组", "其他")
	bySlug := map[string][]Relation{}
	for _, slug := range slugs {
		caches, err := LoadAllCache(dir, slug)
		if err != nil {
			continue
		}
		for _, c := range caches {
			bySlug[slug] = append(bySlug[slug], c.Relations...)
		}
	}
	for _, slug := range relKeys {
		if per[slug] == nil || per[slug].cacheRels == 0 {
			continue
		}
		// Rebuild the canonical index exactly as Merge does.
		caches, _ := LoadAllCache(dir, slug)
		g := &Graph{ProjectSlug: slug, Entities: []Entity{}, Relations: []Relation{}}
		for _, c := range caches {
			g.Merge(c.Entities, nil) // entities only; relations handled below
		}
		canonical := map[string]string{}
		for _, e := range g.Entities {
			for _, k := range entityKeys(e) {
				canonical[k] = e.Name
			}
		}
		var missFrom, missTo, self, dup int
		seen := map[string]bool{}
		for _, r := range bySlug[slug] {
			f, okF := canonical[NormName(r.From)]
			t, okT := canonical[NormName(r.To)]
			switch {
			case !okF:
				missFrom++
			case !okT:
				missTo++
			case f == t:
				self++
			default:
				key := NormName(f) + "\x00" + NormName(t) + "\x00" + NormName(r.Label)
				if seen[key] {
					dup++
				} else {
					seen[key] = true
				}
			}
		}
		other := per[slug].cacheRels - missFrom - missTo - self - dup
		fmt.Printf("%-14s %8d %9d %8d %8d %8d %9d\n",
			slug, per[slug].cacheRels, missFrom, missTo, self, dup, other)
	}

	fmt.Printf("\n=== 别名归并审计（真实数据）===\n")
	fmt.Printf("%-14s %7s %7s %8s %8s %9s %11s\n",
		"命名空间", "折叠", "拒绝", "同名", "名/别", "别/名", "实体(前→后)")
	keys := make([]string, 0, len(per))
	for k := range per {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	row := func(name string, b *bucket) {
		if b.folds == 0 && b.before == 0 {
			return
		}
		fmt.Printf("%-14s %7d %7d %8d %8d %9d %5d→%-5d\n",
			name, b.folds, b.refusals,
			b.kinds[MatchSameName], b.kinds[MatchNameAsAlias], b.kinds[MatchAliasAsName],
			b.before, b.after)
	}
	for _, k := range keys {
		row(k, per[k])
	}
	row("合计", total)

	fmt.Printf("\n=== 关键比例 ===\n")
	fmt.Printf("  实体输入        %d\n", total.before)
	fmt.Printf("  折叠            %d  (%.1f%%)\n", total.folds,
		float64(total.folds)/float64(total.before)*100)
	fmt.Printf("  拒绝（仅同名巧合） %d  (%.1f%%)\n", total.refusals,
		float64(total.refusals)/float64(total.before)*100)
	fmt.Printf("  合并后实体      %d\n", total.after)
	fmt.Printf("  折叠构成: 同名 %d / 名撞别名 %d / 别名撞名 %d\n",
		total.kinds[MatchSameName], total.kinds[MatchNameAsAlias], total.kinds[MatchAliasAsName])

	fmt.Printf("\n=== 被拒绝的折叠（原先会误合，现保持分开）===\n")
	for _, a := range refused {
		mark := ""
		if a.TypeChanged {
			mark = "  ← 类型也不符"
		}
		fmt.Printf("  「%s」 ⊅ 「%s」  经键=%s%s\n", a.Folded, a.Into, a.Via, mark)
	}

	fmt.Printf("\n=== 仍然折叠的样例 ===\n")
	for _, a := range folds {
		fmt.Printf("  「%s」 ⊂ 「%s」  经键=%s  (%s)\n", a.Folded, a.Into, a.Via, a.MatchKind)
	}
}
