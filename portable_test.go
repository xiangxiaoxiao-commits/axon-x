package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"axon/internal/db"
	"axon/internal/graph"
	"axon/internal/sanitize"
)

// tempDataDir points db.AppDataDir at a throwaway directory. On macOS and Linux
// UserConfigDir derives from $HOME, so overriding HOME is enough; the directory
// is created eagerly so the caller can read it back.
func tempDataDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dir, err := db.AppDataDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// seedNamespace writes a plausible local graph: business knowledge plus the
// author's private context, and a session cache so the real assembly path runs.
func seedNamespace(t *testing.T, namespace string) {
	t.Helper()
	dir, err := db.AppDataDir()
	if err != nil {
		t.Fatal(err)
	}
	entities := []graph.Entity{
		{
			Name:         "mono-glite",
			Type:         "module",
			Aliases:      []string{"glite"},
			Observations: []string{"连接器后端，代码在 /Users/author/projects/mono-glite"},
			Assertions: []graph.Assertion{
				{Text: "连接器后端，代码在 /Users/author/projects/mono-glite", State: graph.RelationAsserted, Confidence: 0.9},
				{Text: "约束：跨域交付必须带租户标识", State: graph.RelationVerified, Confidence: 1},
			},
		},
		{
			Name:         "交付单",
			Type:         "concept",
			Observations: []string{"需求方提交的交付请求"},
			Assertions:   []graph.Assertion{{Text: "需求方提交的交付请求", State: graph.RelationAsserted, Confidence: 0.85}},
		},
	}
	relations := []graph.Relation{
		{From: "交付单", To: "mono-glite", Label: "由…实现", State: graph.RelationAsserted, Confidence: 0.8},
	}
	if err := graph.SaveCache(dir, namespace, &graph.SessionCache{
		SessionID: "11111111-2222-3333-4444-555555555555",
		Schema:    graph.CacheSchema,
		Mtime:     111,
		Entities:  entities,
		Relations: relations,
		Chunks:    []graph.Chunk{{ID: "c1", Text: "原始对话：他把 sk-ant-api03-AbCdEf0123456789 填进了配置", Source: "chat"}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&App{}).assembleGraph(namespace, ""); err != nil {
		t.Fatal(err)
	}
}

func exportToTempFile(t *testing.T, a *App, namespace, level string) (string, *graph.Bundle) {
	t.Helper()
	src, err := a.assembleGraph(namespace, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := graph.BuildBundle(src, graph.BuildOptions{Title: namespace, Level: sanitize.ParseLevel(level)})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), graph.PortableFileName(namespace))
	if err := graph.WriteBundleFile(path, b); err != nil {
		t.Fatal(err)
	}
	return path, b
}

// Exporting must be a pure read of the local graph: the author keeps working on
// the same knowledge afterwards.
func TestExportDoesNotTouchLocalGraph(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}

	before, err := a.assembleGraph("wanlian", "")
	if err != nil {
		t.Fatal(err)
	}
	// Compare the knowledge itself: assembleGraph legitimately refreshes
	// UpdatedAt on every call, so the timestamp is not part of the contract.
	beforeJSON := mustJSON(t, knowledgeOf(before))

	if _, err := a.PreviewExport("wanlian", "standard"); err != nil {
		t.Fatal(err)
	}
	exportToTempFile(t, a, "wanlian", "standard")

	after, err := a.assembleGraph("wanlian", "")
	if err != nil {
		t.Fatal(err)
	}
	if mustJSON(t, knowledgeOf(after)) != beforeJSON {
		t.Fatal("export/preview changed the local graph")
	}
	// The verbatim chunk the local graph relies on must still be on disk.
	dir, _ := db.AppDataDir()
	caches, err := graph.LoadAllCache(dir, "wanlian")
	if err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for _, c := range caches {
		chunks += len(c.Chunks)
	}
	if chunks == 0 {
		t.Fatal("export removed the local session chunks")
	}
}

func TestPreviewExportShowsWhatChangesAndLeaks(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}

	prev, err := a.PreviewExport("wanlian", "standard")
	if err != nil {
		t.Fatal(err)
	}
	if prev.SuggestedName != "wanlian.axon-graph.json" {
		t.Errorf("SuggestedName = %q", prev.SuggestedName)
	}
	if prev.Stats.Entities != 2 {
		t.Errorf("preview entities = %d, want 2", prev.Stats.Entities)
	}
	if len(prev.Entities) == 0 {
		t.Fatal("preview returned no entities to inspect")
	}
	var redactedOne PreviewEntity
	for _, e := range prev.Entities {
		if e.Redacted {
			redactedOne = e
		}
	}
	if redactedOne.Name == "" {
		t.Fatal("no entity was flagged as redacted, so the comparison is not working")
	}
	var sawPair bool
	for _, f := range redactedOne.Facts {
		if f.Before != "" && f.Before != f.After && strings.Contains(f.After, "<本地路径>") {
			sawPair = true
		}
	}
	if !sawPair {
		t.Fatalf("no before/after pair shown for %q: %+v", redactedOne.Name, redactedOne.Facts)
	}
	if len(prev.Redactions) == 0 {
		t.Fatal("preview reported no redactions")
	}
}

func TestPreviewExportRefusesEmptyNamespace(t *testing.T) {
	tempDataDir(t)
	a := &App{}
	if _, err := a.PreviewExport("", "standard"); err == nil {
		t.Fatal("empty namespace was accepted")
	}
	if _, err := a.PreviewExport("nothing-here", "standard"); err == nil {
		t.Fatal("empty namespace graph was accepted")
	}
}

// The colleague's side: import into a fresh namespace, read it back, and confirm
// the private context did not travel.
func TestImportBundleIntoFreshNamespace(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}

	path, _ := exportToTempFile(t, a, "wanlian", "standard")
	if strings.Contains(readFile(t, path), "/Users/author") {
		t.Fatal("the exported file still contains the author's home path")
	}

	res, err := a.ImportBundle(path, "wanlian-共享", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Namespace != "wanlian-共享" {
		t.Fatalf("Namespace = %q", res.Namespace)
	}
	if res.Replaced || res.MergedInto {
		t.Errorf("fresh import reported replaced=%v mergedInto=%v", res.Replaced, res.MergedInto)
	}
	if res.Added.Entities != 2 {
		t.Errorf("imported %d entities, want 2", res.Added.Entities)
	}

	imported, err := a.assembleGraph("wanlian-共享", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Entities) != 2 {
		t.Fatalf("imported graph has %d entities", len(imported.Entities))
	}
	blob := mustJSON(t, imported)
	for _, leak := range []string{"/Users/author", "sk-ant-api03-AbCdEf0123456789", "11111111-2222-3333-4444-555555555555"} {
		if strings.Contains(blob, leak) {
			t.Errorf("imported graph contains %q", leak)
		}
	}
	if !strings.Contains(blob, "跨域交付必须带租户标识") {
		t.Error("imported graph lost the business constraint")
	}
	// Both namespaces coexist: importing never disturbs the original.
	if orig, err := a.assembleGraph("wanlian", ""); err != nil || len(orig.Entities) != 2 {
		t.Fatalf("original namespace damaged: entities=%d err=%v", len(orig.Entities), err)
	}

	// The namespace picker is rendered from ListNamespaces, and that is where an
	// import used to disappear: the cache was written and assembleGraph could read
	// it, but the picker had been loaded once at startup and never learned about
	// the new namespace — so the knowledge was on disk and unreachable, which reads
	// exactly like "the import did nothing". Assert the listing path too, not just
	// the disk write.
	names, err := a.ListNamespaces()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n.Name != "wanlian-共享" {
			continue
		}
		found = true
		if n.Entities == 0 {
			t.Errorf("imported namespace %q is listed but reports 0 entities", n.Name)
		}
	}
	if !found {
		t.Errorf("the picker's namespace list does not include the imported namespace: %+v", names)
	}
}

// Re-importing the same file must not duplicate knowledge, and importing a
// different file must add to it rather than replace it.
func TestImportIsIdempotentAndAdditive(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}
	path, _ := exportToTempFile(t, a, "wanlian", "standard")

	if _, err := a.ImportBundle(path, "target", false); err != nil {
		t.Fatal(err)
	}
	first, _ := a.assembleGraph("target", "")
	firstCount := len(first.Entities)

	again, err := a.ImportBundle(path, "target", false)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replaced || !again.MergedInto {
		t.Errorf("re-import reported replaced=%v mergedInto=%v, want both true", again.Replaced, again.MergedInto)
	}
	second, _ := a.assembleGraph("target", "")
	if len(second.Entities) != firstCount {
		t.Fatalf("re-import grew the graph from %d to %d entities", firstCount, len(second.Entities))
	}

	// A second, different bundle adds new knowledge alongside the first.
	other := filepath.Join(t.TempDir(), "other.axon-graph.json")
	b, err := graph.BuildBundle(&graph.Graph{
		ProjectSlug: "other",
		Entities: []graph.Entity{{
			Name: "计费中心", Type: "service",
			Observations: []string{"按使用量计费"},
			Assertions:   []graph.Assertion{{Text: "按使用量计费", State: graph.RelationAsserted, Confidence: 0.9}},
		}},
	}, graph.BuildOptions{Title: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.WriteBundleFile(other, b); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ImportBundle(other, "target", false); err != nil {
		t.Fatal(err)
	}
	third, _ := a.assembleGraph("target", "")
	if len(third.Entities) <= firstCount {
		t.Fatalf("second import did not add knowledge: %d -> %d", firstCount, len(third.Entities))
	}
}

// An import must be undoable, or nobody will dare to use it.
func TestListAndRemoveImportedBundles(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}
	path, _ := exportToTempFile(t, a, "wanlian", "standard")

	if _, err := a.ImportBundle(path, "target", false); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListImportedBundles("target")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("ListImportedBundles returned %d entries, want 1", len(list))
	}
	if list[0].Entities != 2 || list[0].Relations != 1 {
		t.Errorf("imported bundle summary = %+v", list[0])
	}

	if err := a.RemoveImportedBundle("target", list[0].ID); err != nil {
		t.Fatal(err)
	}
	after, _ := a.assembleGraph("target", "")
	if len(after.Entities) != 0 {
		t.Fatalf("removal left %d entities behind", len(after.Entities))
	}
	if leftover, _ := a.ListImportedBundles("target"); len(leftover) != 0 {
		t.Fatalf("ListImportedBundles still reports %+v", leftover)
	}
}

func TestRemoveImportedBundleRejectsPathTraversal(t *testing.T) {
	tempDataDir(t)
	a := &App{}
	for _, bad := range []string{"../wanlian", `..\wanlian`, "..", "."} {
		if err := a.RemoveImportedBundle("target", bad); err == nil {
			t.Errorf("RemoveImportedBundle accepted %q", bad)
		}
	}
}

// A namespace becomes a directory name, so it has to be legal on Windows too.
func TestImportSanitisesTargetNamespace(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}
	path, _ := exportToTempFile(t, a, "wanlian", "standard")

	res, err := a.ImportBundle(path, "  wan:lian/共享  ", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(res.Namespace, `:/\`) {
		t.Fatalf("Namespace = %q still holds characters a file system rejects", res.Namespace)
	}
	if res.Namespace != "wan_lian-共享" {
		t.Fatalf("Namespace = %q, want a sanitised directory name", res.Namespace)
	}
}

func TestInspectBundleFileReportsContent(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}
	path, _ := exportToTempFile(t, a, "wanlian", "strict")

	info, err := a.inspectBundleFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "wanlian" || info.Version == 0 {
		t.Errorf("info = %+v", info)
	}
	if info.SuggestedNamespace != "wanlian" {
		t.Errorf("SuggestedNamespace = %q", info.SuggestedNamespace)
	}
	if info.Stats.Entities != 2 {
		t.Errorf("Stats = %+v", info.Stats)
	}
	for _, h := range info.Redactions {
		if len(h.Samples) != 0 {
			t.Errorf("bundle report carries samples for %q, which would leak what was removed", h.Rule)
		}
	}
	if len(info.Notes) == 0 {
		t.Error("bundle does not explain what it leaves out")
	}
}

func TestInspectBundleFileRejectsForeignJSON(t *testing.T) {
	tempDataDir(t)
	a := &App{}
	path := filepath.Join(t.TempDir(), "random.json")
	if err := os.WriteFile(path, []byte(`{"hello":"world"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.inspectBundleFile(path); err == nil {
		t.Fatal("a foreign JSON file was accepted as a knowledge bundle")
	}
}

// A bundle from the other operating system is a normal input: CRLF endings and a
// BOM must both be tolerated.
func TestImportAcceptsWindowsAuthoredFile(t *testing.T) {
	tempDataDir(t)
	seedNamespace(t, "wanlian")
	a := &App{}
	path, b := exportToTempFile(t, a, "wanlian", "standard")

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	win := append([]byte{0xEF, 0xBB, 0xBF}, []byte(strings.ReplaceAll(string(raw), "\n", "\r\n"))...)
	winPath := filepath.Join(t.TempDir(), "from-windows.axon-graph.json")
	if err := os.WriteFile(winPath, win, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := a.ImportBundle(winPath, "from-win", false)
	if err != nil {
		t.Fatalf("importing a Windows-authored file failed: %v", err)
	}
	if res.BundleID != b.Digest {
		t.Fatalf("BundleID = %q, want the same digest %q so both platforms agree on identity", res.BundleID, b.Digest)
	}
	imported, _ := a.assembleGraph("from-win", "")
	if len(imported.Entities) != 2 {
		t.Fatalf("imported %d entities from the Windows file", len(imported.Entities))
	}
}

// knowledgeOf projects a graph down to its knowledge, ignoring bookkeeping the
// assembly step refreshes on every call.
func knowledgeOf(g *graph.Graph) any {
	return struct {
		Entities  []graph.Entity   `json:"entities"`
		Relations []graph.Relation `json:"relations"`
	}{Entities: g.Entities, Relations: g.Relations}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
