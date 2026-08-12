package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseWikilinks(t *testing.T) {
	content := `# Note

See [[Payment Service]] and [[order|订单服务]] for details.
Also [[Design#Idempotency]] and a repeat [[Payment Service]].
A pathed link [[folder/Risk Gateway.md]] too.`
	got := parseWikilinks(content)
	want := []string{"Payment Service", "order", "Design", "Risk Gateway"}
	if len(got) != len(want) {
		t.Fatalf("parseWikilinks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseWikilinks[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNormalizeLinkTarget(t *testing.T) {
	cases := map[string]string{
		"Payment Service":        "Payment Service",
		"order|订单服务":             "order",
		"Design#Idempotency":     "Design",
		"folder/Risk Gateway.md": "Risk Gateway",
		"a#h|d":                  "a",
	}
	for in, want := range cases {
		if got := normalizeLinkTarget(in); got != want {
			t.Errorf("normalizeLinkTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseNote(t *testing.T) {
	content := "# 支付服务\n\n这篇笔记讲支付。它 [[订单服务]] 依赖，也 [[风控网关|风控]]。"
	ents, rels := parseNote("services/支付服务.md", content)
	if len(ents) != 1 {
		t.Fatalf("expected 1 note entity, got %d", len(ents))
	}
	e := ents[0]
	if e.Type != typeNote {
		t.Errorf("entity type = %q, want %q", e.Type, typeNote)
	}
	if e.Name != "支付服务" {
		t.Errorf("entity name = %q, want 支付服务", e.Name)
	}
	// Relative path is an alias so path mentions still hit the note.
	foundPathAlias := false
	for _, al := range e.Aliases {
		if al == "services/支付服务.md" {
			foundPathAlias = true
		}
	}
	if !foundPathAlias {
		t.Errorf("aliases %v missing relative path", e.Aliases)
	}
	if len(rels) != 2 {
		t.Fatalf("expected 2 link relations, got %d: %+v", len(rels), rels)
	}
	for _, r := range rels {
		if r.From != "支付服务" || r.Label != labelLinks {
			t.Errorf("bad relation: %+v", r)
		}
	}
	if rels[0].To != "订单服务" || rels[1].To != "风控网关" {
		t.Errorf("relation targets = %q,%q want 订单服务,风控网关", rels[0].To, rels[1].To)
	}
}

func TestChunkNote(t *testing.T) {
	body := ""
	for i := 0; i < 30; i++ {
		body += "这是一段关于支付服务幂等设计的高密度笔记内容，需要被切块并向量化召回。\n\n"
	}
	chunks := chunkNote("obsidian:notes/支付.md", body)
	if len(chunks) == 0 {
		t.Fatal("expected note chunks")
	}
	for _, ch := range chunks {
		if ch.Kind != "note" || ch.Source != "obsidian:notes/支付.md" {
			t.Errorf("bad note chunk metadata: %+v", ch)
		}
	}
}

func TestListVaultNotes_SkipsHiddenAndNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "A.md"), "# A\n[[B]]")
	mustWrite(t, filepath.Join(dir, "sub", "B.md"), "# B")
	mustWrite(t, filepath.Join(dir, "attach.png"), "binary")
	mustWrite(t, filepath.Join(dir, ".obsidian", "config.md"), "config, must be skipped")

	notes, err := listVaultNotes(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, n := range notes {
		got[n] = true
	}
	if !got["A.md"] || !got["sub/B.md"] {
		t.Errorf("expected A.md and sub/B.md, got %v", notes)
	}
	if got["attach.png"] {
		t.Error("non-markdown attachment should be skipped")
	}
	for n := range got {
		if len(n) >= 10 && n[:10] == ".obsidian/" {
			t.Errorf("hidden .obsidian dir should be skipped, got %q", n)
		}
	}
}

func TestCacheKeyFor(t *testing.T) {
	if got := cacheKeyFor("obsidian:sub/note.md"); got != "obsidian:sub_note.md" {
		t.Errorf("cacheKeyFor = %q, want obsidian:sub_note.md", got)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
