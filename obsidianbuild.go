package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"axon/internal/db"
	"axon/internal/graph"
)

// Obsidian entity type and relation label. The label is Chinese to unify with
// conversation/code-sourced relations so graph.Merge de-dupes them consistently
// and injected context reads uniformly.
const (
	typeNote   = "note"
	labelLinks = "链接" // note -> linked note (Obsidian [[wikilink]])
)

// wikilinkRe matches an Obsidian wikilink "[[target]]", "[[target|display]]" or
// "[[target#heading]]". Group 1 is the raw inner text (target[#heading][|display]).
var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// BuildGraphFromObsidian ingests an Obsidian vault as a knowledge source: every
// note becomes a "note" entity, its [[wikilinks]] become "链接" relations, and its
// prose is chunked + embedded into the raw-context channel. Notes are
// human-authored, high-density text, so unlike conversation transcripts the chunk
// channel carries the primary value here; wikilinks are human-confirmed relations
// mapped straight onto graph edges.
//
// Like IndexProject/BuildGraphFromCode it goes through the cache (SaveCache) and
// reassembles the graph, so obsidian knowledge fuses with conversation/code
// knowledge via alias normalization. Each note is cached under "obsidian:<rel>"
// and re-processed incrementally (skipped when its file mtime is unchanged).
func (a *App) BuildGraphFromObsidian(vaultDir, projectSlug string) error {
	if strings.TrimSpace(vaultDir) == "" || strings.TrimSpace(projectSlug) == "" {
		return fmt.Errorf("vaultDir 和 projectSlug 不能为空")
	}
	dataDir, err := db.AppDataDir()
	if err != nil {
		return err
	}
	notes, err := listVaultNotes(vaultDir)
	if err != nil {
		return fmt.Errorf("scan obsidian vault: %w", err)
	}

	// newEmbedder falls back to a local embedder, so an embedder is normally
	// always available; only a genuine misconfiguration returns an error.
	emb, embErr := a.newEmbedder()
	if embErr != nil {
		emb = nil
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "phase": "obsidian",
			"warning": "未配置 embedding provider：本次不生成向量与原文块，召回将退回关键词匹配",
		})
	}

	newly := 0
	for i, rel := range notes {
		full := filepath.Join(vaultDir, filepath.FromSlash(rel))
		info, statErr := os.Stat(full)
		if statErr != nil {
			continue
		}
		mtime := info.ModTime().UnixMilli()
		sessionID := "obsidian:" + rel
		if _, ok := graph.LoadCache(dataDir, projectSlug, cacheKeyFor(sessionID), mtime); ok {
			continue // fresh cache at current schema, skip entirely
		}
		a.emit(EventGraphProgress, map[string]any{
			"projectSlug": projectSlug, "current": i + 1, "total": len(notes),
			"title": rel, "phase": "obsidian",
		})

		data, readErr := os.ReadFile(full)
		if readErr != nil {
			continue
		}
		content := string(data)
		ents, rels := parseNote(rel, content)
		chunks := chunkNote(sessionID, content)
		if emb != nil {
			a.embedEntities(emb, ents) // fills each entity's Embedding in place
			a.embedChunks(emb, chunks)
		}
		if err := graph.SaveCache(dataDir, projectSlug, &graph.SessionCache{
			SessionID: cacheKeyFor(sessionID), Mtime: mtime, Schema: graph.CacheSchema,
			Entities: ents, Relations: rels, Chunks: chunks,
		}); err != nil {
			return err
		}
		newly++
	}

	// Reassemble the project graph from all caches (conversation, code, obsidian).
	if _, err := a.assembleGraph(projectSlug, ""); err != nil {
		return err
	}
	a.emit(EventGraphDone, map[string]any{
		"projectSlug": projectSlug, "processed": newly, "phase": "obsidian",
	})
	return nil
}

// cacheKeyFor turns a chunk source id into a flat, filesystem-safe cache filename
// stem: SaveCache writes "<key>.json" in one directory and does not create nested
// dirs, so slashes in an "obsidian:<rel>" id (rel may contain "/") must be
// flattened. Colons are kept (safe on the target filesystems) so the "obsidian:"
// prefix stays visible on disk.
func cacheKeyFor(id string) string {
	return strings.ReplaceAll(id, "/", "_")
}

// listVaultNotes walks vaultDir and returns vault-relative ".md" note paths
// (forward-slashed), skipping hidden directories (.obsidian config, .trash, .git)
// and non-markdown files (attachments). Order is directory-walk order.
func listVaultNotes(vaultDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(vaultDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, keep walking
		}
		if d.IsDir() {
			// Skip hidden dirs (.obsidian holds config/plugins; .trash, .git etc.).
			if d.Name() != "." && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(vaultDir, p)
		if relErr != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// noteTitle is the note's display name: its filename without the ".md"
// extension. This is what other notes reference in a [[wikilink]].
func noteTitle(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// parseNote builds the structure-channel output for one note: a "note" entity
// (Name = title, aliases = relative path + basename so mentions still hit it) and
// one "链接" relation per [[wikilink]] to the linked note's title. Link targets are
// normalized (display alias, "#heading" and ".md"/path stripped) down to the bare
// note title so they line up with the target note's entity Name for merging.
func parseNote(rel, content string) ([]graph.Entity, []graph.Relation) {
	title := noteTitle(rel)
	ent := graph.Entity{
		Name:    title,
		Type:    typeNote,
		Aliases: noteAliases(title, rel),
	}
	var rels []graph.Relation
	seen := map[string]bool{}
	for _, target := range parseWikilinks(content) {
		if target == "" || strings.EqualFold(target, title) || seen[strings.ToLower(target)] {
			continue
		}
		seen[strings.ToLower(target)] = true
		rels = append(rels, graph.Relation{From: title, To: target, Label: labelLinks})
	}
	return []graph.Entity{ent}, rels
}

// noteAliases returns discovery aliases for a note: its vault-relative path and
// basename, so a mention of the path or "note.md" still resolves to the entity.
// The title itself is excluded (it is the canonical name, not an alias).
func noteAliases(title, rel string) []string {
	seen := map[string]bool{strings.ToLower(title): true}
	var out []string
	add := func(s string) {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	add(rel)
	add(filepath.Base(rel))
	return out
}

// parseWikilinks extracts the linked note titles from every [[wikilink]] in the
// text, normalizing each: strip a "|display" alias, a "#heading" anchor and any
// ".md" extension or folder path, leaving the bare note title. De-duped, order
// preserved.
func parseWikilinks(content string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range wikilinkRe.FindAllStringSubmatch(content, -1) {
		target := normalizeLinkTarget(m[1])
		if target == "" || seen[strings.ToLower(target)] {
			continue
		}
		seen[strings.ToLower(target)] = true
		out = append(out, target)
	}
	return out
}

// normalizeLinkTarget reduces a wikilink inner text ("target#heading|display",
// "folder/note.md", "note") to the bare note title used as the relation target.
func normalizeLinkTarget(inner string) string {
	s := inner
	if i := strings.Index(s, "|"); i >= 0 { // drop display alias
		s = s[:i]
	}
	if i := strings.Index(s, "#"); i >= 0 { // drop heading/block anchor
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ".md")
	s = filepath.Base(filepath.ToSlash(s)) // drop any folder path
	return strings.TrimSpace(s)
}

// chunkNote splits a note's prose into verbatim chunks for the raw-context
// channel, packing paragraphs up to ~chunkTargetChars (the same size used for
// conversation chunks) with a one-paragraph overlap. Source is "obsidian:<rel>",
// kind "note". The shared packer drops tiny/greeting residual blocks; note prose
// is otherwise kept verbatim.
func chunkNote(sessionID, content string) []graph.Chunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	paras := splitParagraphs(content)
	return assembleChunks(paras, sessionID, "note", chunkTargetChars)
}
