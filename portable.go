package main

// Knowledge-graph export / import.
//
// Export turns a namespace's curated graph into a portable file that can be
// handed to a colleague, with the author's private context stripped out. Import
// takes such a file and folds its knowledge into a namespace of the recipient's
// machine.
//
// The guarantees, in order of importance:
//
//  1. Export never modifies the local graph. It reads the assembled graph and
//     works on copies.
//  2. Import never overwrites knowledge. It writes one new cache file whose name
//     is derived from the bundle's content, so re-importing the same file is
//     idempotent and importing a different file adds to what is there.
//  3. Everything the file carries is scrubbed, and nothing that was scrubbed
//     rides along in the report.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"axon/internal/db"
	"axon/internal/graph"
	"axon/internal/sanitize"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// FactPair shows one fact before and after redaction. It exists so the operator
// can see exactly what would change before anything leaves the machine; it is
// only ever returned to the local UI and never written into the export.
type FactPair struct {
	Before string `json:"before"`
	After  string `json:"after"`
}

// PreviewEntity is one entity as it would appear in the export.
type PreviewEntity struct {
	Name     string     `json:"name"`
	Type     string     `json:"type"`
	Aliases  []string   `json:"aliases,omitempty"`
	Facts    []FactPair `json:"facts"`
	Redacted bool       `json:"redacted"`
}

// ExportPreview is the answer to "what would leaving my machine contain?".
type ExportPreview struct {
	Namespace  string            `json:"namespace"`
	Title      string            `json:"title"`
	Level      string            `json:"level"`
	Stats      graph.BundleStats `json:"stats"`
	Redactions []sanitize.Hit    `json:"redactions"`
	Entities   []PreviewEntity   `json:"entities"`
	Notes      []string          `json:"notes"`
	// SuggestedName is a file name that is valid on both macOS and Windows.
	SuggestedName string `json:"suggestedName"`
}

// ExportResult reports a completed export.
type ExportResult struct {
	Canceled   bool              `json:"canceled"`
	Path       string            `json:"path"`
	Namespace  string            `json:"namespace"`
	Level      string            `json:"level"`
	Stats      graph.BundleStats `json:"stats"`
	Redactions []sanitize.Hit    `json:"redactions"`
	SizeBytes  int64             `json:"sizeBytes"`
}

// BundleInfo summarises a bundle file before it is imported.
type BundleInfo struct {
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Version    int               `json:"version"`
	ExportedAt int64             `json:"exportedAt"`
	ScrubLevel string            `json:"scrubLevel"`
	Stats      graph.BundleStats `json:"stats"`
	Redactions []sanitize.Hit    `json:"redactions"`
	Notes      []string          `json:"notes"`
	// SuggestedNamespace defaults the import target.
	SuggestedNamespace string `json:"suggestedNamespace"`
}

// ImportResult reports a completed import.
type ImportResult struct {
	Namespace     string            `json:"namespace"`
	BundleID      string            `json:"bundleId"`
	Title         string            `json:"title"`
	Added         graph.BundleStats `json:"added"`
	Redactions    []sanitize.Hit    `json:"redactions"`
	Replaced      bool              `json:"replaced"`      // same bundle imported before
	MergedInto    bool              `json:"mergedInto"`    // namespace already had knowledge
	EmbeddedCount int               `json:"embeddedCount"` // entities re-embedded on import
	EmbedError    string            `json:"embedError,omitempty"`
}

// ImportedBundle describes one bundle already folded into a namespace.
type ImportedBundle struct {
	ID           string `json:"id"`
	SessionID    string `json:"sessionId"`
	Entities     int    `json:"entities"`
	Relations    int    `json:"relations"`
	Observations int    `json:"observations"`
}

// PreviewExport assembles the namespace's graph, applies the scrubber, and
// returns what would be exported. Nothing is written and the local graph is left
// exactly as it was.
func (a *App) PreviewExport(namespace, level string) (ExportPreview, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ExportPreview{}, fmt.Errorf("先选择一个命名空间再导出")
	}
	g, err := a.assembleGraph(namespace, "")
	if err != nil {
		return ExportPreview{}, err
	}
	if len(g.Entities) == 0 {
		return ExportPreview{}, fmt.Errorf("命名空间 %q 还没有知识可导出", namespace)
	}
	opts := a.buildOptions(namespace, level)
	b, err := graph.BuildBundle(g, opts)
	if err != nil {
		return ExportPreview{}, err
	}

	preview := ExportPreview{
		Namespace:     namespace,
		Title:         b.Title,
		Level:         b.ScrubLevel,
		Stats:         b.Stats,
		Redactions:    b.Redactions,
		Notes:         b.Notes,
		SuggestedName: graph.PortableFileName(namespace),
		Entities:      previewEntities(g, b),
	}
	return preview, nil
}

// previewEntities pairs each exported entity with the facts it started from, so
// the UI can show "this is what changed" rather than a bare count.
//
// Matching is positional within an entity, not by name: redaction can rewrite an
// entity's name, so the original and the copy are paired through the bundle's
// own entity entry, which keeps the two lists in the same order.
func previewEntities(src *graph.Graph, b *graph.Bundle) []PreviewEntity {
	// Index the source by every name and alias it is reachable under, so an
	// entity whose name was rewritten is still found.
	byKey := make(map[string]graph.Entity, len(src.Entities)*2)
	for _, e := range src.Entities {
		byKey[graph.NormName(e.Name)] = e
		for _, al := range e.Aliases {
			byKey[graph.NormName(al)] = e
		}
	}
	used := make(map[string]bool, len(src.Entities))

	out := make([]PreviewEntity, 0, len(b.Entities))
	for _, e := range b.Entities {
		pe := PreviewEntity{Name: e.Name, Type: e.Type, Aliases: e.Aliases}

		var original graph.Entity
		var found bool
		if oe, ok := byKey[graph.NormName(e.Name)]; ok && !used[graph.NormName(oe.Name)] {
			original, found = oe, true
		} else {
			// The name changed beyond recognition; fall back to the first source
			// entity not yet paired, which preserves the entity's own fact order.
			for _, se := range src.Entities {
				if !used[graph.NormName(se.Name)] {
					original, found = se, true
					break
				}
			}
		}
		if found {
			used[graph.NormName(original.Name)] = true
		}

		for i, a := range e.Assertions {
			pair := FactPair{After: a.Text}
			if found && i < len(original.Observations) {
				pair.Before = original.Observations[i]
			}
			pe.Facts = append(pe.Facts, pair)
			if pair.Before != "" && pair.Before != pair.After {
				pe.Redacted = true
			}
		}
		if found && nameWasRewritten(original.Name, e.Name) {
			pe.Redacted = true
		}
		out = append(out, pe)
	}
	return out
}

// nameWasRewritten reports whether redaction rewrote an entity's name.
func nameWasRewritten(before, after string) bool {
	return strings.TrimSpace(before) != "" && before != after
}

// ExportGraph writes the namespace's scrubbed graph to a file the user picks.
func (a *App) ExportGraph(namespace, level string) (ExportResult, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return ExportResult{}, fmt.Errorf("先选择一个命名空间再导出")
	}
	g, err := a.assembleGraph(namespace, "")
	if err != nil {
		return ExportResult{}, err
	}
	if len(g.Entities) == 0 {
		return ExportResult{}, fmt.Errorf("命名空间 %q 还没有知识可导出", namespace)
	}
	b, err := graph.BuildBundle(g, a.buildOptions(namespace, level))
	if err != nil {
		return ExportResult{}, err
	}

	path := ""
	if a.ctx != nil {
		path, err = wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
			Title:           "导出知识图谱",
			DefaultFilename: graph.PortableFileName(namespace),
			Filters: []wruntime.FileFilter{
				{DisplayName: "Axon 知识包 (*.json)", Pattern: "*.json"},
				{DisplayName: "所有文件", Pattern: "*.*"},
			},
		})
		if err != nil {
			return ExportResult{}, fmt.Errorf("打开保存对话框失败: %w", err)
		}
		if path == "" {
			return ExportResult{Canceled: true, Namespace: namespace}, nil
		}
	} else {
		// No window to ask (tests, headless): still write the file, into the app
		// data directory, rather than silently doing nothing.
		dataDir, dErr := db.AppDataDir()
		if dErr != nil {
			return ExportResult{}, dErr
		}
		path = filepath.Join(dataDir, graph.PortableFileName(namespace))
	}

	if err := graph.WriteBundleFile(path, b); err != nil {
		return ExportResult{}, err
	}
	info, _ := os.Stat(path)
	var size int64
	if info != nil {
		size = info.Size()
	}
	return ExportResult{
		Path:       path,
		Namespace:  namespace,
		Level:      b.ScrubLevel,
		Stats:      b.Stats,
		Redactions: b.Redactions,
		SizeBytes:  size,
	}, nil
}

// buildOptions collects the scrub settings shared by preview and export. The
// omitted-chunk count comes from the session caches, which a Graph does not
// carry, so the report can be honest about what stayed behind.
func (a *App) buildOptions(namespace, level string) graph.BuildOptions {
	opts := graph.BuildOptions{Title: namespace, Level: sanitize.ParseLevel(level)}
	if dataDir, err := db.AppDataDir(); err == nil {
		if caches, err := graph.LoadAllCache(dataDir, namespace); err == nil {
			for _, c := range caches {
				opts.OmittedChunks += len(c.Chunks)
			}
		}
		if ex, err := graph.LoadExclusions(dataDir, namespace); err == nil {
			opts.OmittedReviews = len(ex.ReviewHistory)
		}
	}
	return opts
}

// InspectBundle asks for a bundle file and reports what is inside it without
// importing anything.
func (a *App) InspectBundle() (BundleInfo, error) {
	path := ""
	if a.ctx != nil {
		p, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
			Title: "选择要导入的知识包",
			Filters: []wruntime.FileFilter{
				{DisplayName: "Axon 知识包 (*.json)", Pattern: "*.json"},
				{DisplayName: "所有文件", Pattern: "*.*"},
			},
		})
		if err != nil {
			return BundleInfo{}, fmt.Errorf("打开文件对话框失败: %w", err)
		}
		if p == "" {
			return BundleInfo{}, nil // cancelled
		}
		path = p
	} else {
		return BundleInfo{}, fmt.Errorf("导入需要在应用窗口内操作")
	}
	return a.inspectBundleFile(path)
}

func (a *App) inspectBundleFile(path string) (BundleInfo, error) {
	b, err := graph.ReadBundleFile(path)
	if err != nil {
		return BundleInfo{}, err
	}
	return BundleInfo{
		Path:               path,
		Title:              b.Title,
		Version:            b.Version,
		ExportedAt:         b.ExportedAt,
		ScrubLevel:         b.ScrubLevel,
		Stats:              b.Stats,
		Redactions:         b.Redactions,
		Notes:              b.Notes,
		SuggestedNamespace: safeNamespace(b.Title),
	}, nil
}

// ImportBundle folds a bundle file into a namespace. It writes one new cache
// file and re-assembles the graph; no existing knowledge is overwritten, so the
// recipient can always delete the import and be exactly where they started.
func (a *App) ImportBundle(bundlePath, namespace string, reembed bool) (ImportResult, error) {
	if strings.TrimSpace(bundlePath) == "" {
		return ImportResult{}, fmt.Errorf("没有选择知识包文件")
	}
	b, err := graph.ReadBundleFile(bundlePath)
	if err != nil {
		return ImportResult{}, err
	}
	ns := safeNamespace(namespace)
	if ns == "" {
		ns = safeNamespace(b.Title)
	}
	if ns == "" {
		return ImportResult{}, fmt.Errorf("请填写一个目标命名空间（字母、数字、中文、- 和 _）")
	}

	dataDir, err := db.AppDataDir()
	if err != nil {
		return ImportResult{}, err
	}

	// Defence in depth: a bundle may be hand-edited, produced by an older build,
	// or come from a machine with different rules. Re-running the local rule set
	// over it costs nothing and cannot add knowledge back.
	scrubber := sanitize.New(sanitize.Options{Level: sanitize.ParseLevel(string(b.ScrubLevel))})
	for i := range b.Entities {
		b.Entities[i].Name = scrubber.Text(b.Entities[i].Name)
		b.Entities[i].Aliases = scrubber.Strings(b.Entities[i].Aliases)
		for j := range b.Entities[i].Assertions {
			b.Entities[i].Assertions[j].Text = scrubber.Text(b.Entities[i].Assertions[j].Text)
			b.Entities[i].Assertions[j].Sources = nil
		}
		b.Entities[i].ObsSources = nil
		b.Entities[i].Embedding = nil
		graph.NormalizeEntityAssertions(&b.Entities[i])
		b.Entities[i].ObsSources = nil
	}
	for i := range b.Relations {
		b.Relations[i].Sources = nil
		b.Relations[i].Evidence = scrubber.Strings(b.Relations[i].Evidence)
	}

	// Embed imported entities so semantic recall works for them too, without
	// shipping vectors from the author's machine. Failure is not fatal: the
	// knowledge is still imported and keyword recall still finds it.
	embedded, embedNote := 0, ""
	if reembed {
		if emb, embErr := a.newEmbedder(); embErr != nil {
			embedNote = embErr.Error()
		} else if emb != nil {
			a.embedEntities(emb, b.Entities)
			for i := range b.Entities {
				if len(b.Entities[i].Embedding) > 0 {
					embedded++
				}
			}
		}
	}

	preExisting := countCaches(dataDir, ns)
	cache := b.Cache()
	alreadyThere := false
	if _, statErr := os.Stat(graph.CachePath(dataDir, ns, cache.SessionID)); statErr == nil {
		alreadyThere = true
	}
	if err := graph.SaveCache(dataDir, ns, cache); err != nil {
		return ImportResult{}, fmt.Errorf("写入导入知识失败: %w", err)
	}
	if _, err := a.assembleGraph(ns, ""); err != nil {
		return ImportResult{}, fmt.Errorf("重建命名空间失败: %w", err)
	}

	return ImportResult{
		Namespace:     ns,
		BundleID:      strings.TrimPrefix(cache.SessionID, graph.ImportSourcePrefix),
		Title:         b.Title,
		Added:         b.Stats,
		Redactions:    scrubber.Report().Shareable().Hits,
		Replaced:      alreadyThere,
		MergedInto:    preExisting > 0,
		EmbeddedCount: embedded,
		EmbedError:    embedNote,
	}, nil
}

// ListImportedBundles reports which bundles a namespace has absorbed, so an
// import can be inspected and undone rather than being an irreversible act.
func (a *App) ListImportedBundles(namespace string) ([]ImportedBundle, error) {
	namespace = safeNamespace(namespace)
	if namespace == "" {
		return []ImportedBundle{}, nil
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return nil, err
	}
	caches, err := graph.LoadAllCache(dataDir, namespace)
	if err != nil {
		return nil, err
	}
	out := make([]ImportedBundle, 0)
	for _, c := range caches {
		if !strings.HasPrefix(c.SessionID, graph.ImportSourcePrefix) {
			continue
		}
		obs := 0
		for _, e := range c.Entities {
			obs += len(e.Assertions)
		}
		out = append(out, ImportedBundle{
			ID:           strings.TrimPrefix(c.SessionID, graph.ImportSourcePrefix),
			SessionID:    c.SessionID,
			Entities:     len(c.Entities),
			Relations:    len(c.Relations),
			Observations: obs,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RemoveImportedBundle deletes an imported bundle's knowledge and re-assembles
// the namespace. Only the import's own cache file is touched.
func (a *App) RemoveImportedBundle(namespace, bundleID string) error {
	namespace = safeNamespace(namespace)
	bundleID = strings.TrimSpace(bundleID)
	if namespace == "" || bundleID == "" {
		return fmt.Errorf("命名空间和知识包 ID 不能为空")
	}
	if strings.ContainsAny(bundleID, `/\`) || bundleID == "." || bundleID == ".." {
		return fmt.Errorf("知识包 ID 不合法")
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	if err := graph.DeleteCache(dataDir, namespace, graph.ImportSourcePrefix+bundleID); err != nil {
		return err
	}
	_, err = a.assembleGraph(namespace, "")
	return err
}

// countCaches reports how many caches a namespace already had, used to tell the
// user whether an import merged into existing knowledge or started fresh.
func countCaches(dataDir, namespace string) int {
	caches, err := graph.LoadAllCache(dataDir, namespace)
	if err != nil {
		return 0
	}
	return len(caches)
}

// safeNamespace validates a namespace used as a directory name. It must survive
// both macOS and Windows file systems, because the namespace becomes a directory
// under graphcache/.
func safeNamespace(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return graph.SafeFileComponent(name)
}
