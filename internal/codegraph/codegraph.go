// Package codegraph extracts a structural skeleton (files, packages, functions,
// types and their relations) from a source repository and maps it onto the
// existing graph.Entity/graph.Relation model, so code knowledge can be merged
// into the same per-project knowledge graph as conversation-distilled knowledge.
//
// Two layers: a language-agnostic layer (directory/file/package + import
// regexes) that never leaves a repo with an empty graph regardless of language,
// and a Go-only precise layer (go/ast) that additionally yields functions,
// types and call edges. LLM business enrichment and merging happen in the app
// layer; this package is pure, deterministic, and calls no model.
package codegraph

import (
	"path/filepath"
	"strings"

	"axon/internal/graph"
)

// Relation labels are Chinese to unify with conversation-sourced relations, so
// graph.Merge de-dupes them consistently and injected context reads uniformly.
const (
	labelContains = "包含" // package -> file, file -> function/type
	labelDepends  = "依赖" // file -> imported package/module
	labelCalls    = "调用" // function -> function (Go only)
)

// Entity types for code-sourced nodes (coexist with conversation types like
// module|service|concept|decision|constraint).
const (
	typeFile     = "file"
	typePackage  = "package"
	typeFunction = "function"
	typeType     = "type"
)

// BuildSkeleton scans repoDir and returns entities and relations ready to feed
// graph.Merge. It is deterministic and free (no model calls): the generic layer
// produces file/package entities and import ("依赖") relations for every
// language, and Go files additionally get function/type entities plus
// contains/calls relations via go/ast.
func BuildSkeleton(repoDir string) ([]graph.Entity, []graph.Relation, error) {
	files, err := ListSourceFiles(repoDir)
	if err != nil {
		return nil, nil, err
	}
	b := &builder{pkgs: map[string]bool{}}
	for _, rel := range files {
		b.addFile(repoDir, rel)
	}
	return b.ents, b.rels, nil
}

// builder accumulates entities/relations while de-duplicating package nodes.
type builder struct {
	pkgs map[string]bool // package name -> already emitted
	ents []graph.Entity
	rels []graph.Relation
}

// addFile emits the file entity, its owning package entity (once), the
// containment relation between them, and then language-specific detail.
func (b *builder) addFile(repoDir, rel string) {
	rel = filepath.ToSlash(rel)
	pkg := packageName(rel)
	b.ensurePackage(pkg)

	// File entity: name is the repo-relative path (unique); short forms go into
	// aliases so natural-language mentions ("merge.go", "merge") still hit it.
	base := filepath.Base(rel)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	b.ents = append(b.ents, graph.Entity{
		Name:    rel,
		Type:    typeFile,
		Aliases: dedupAliases(rel, base, stem, symbolAliases(stem)),
	})
	b.rels = append(b.rels, graph.Relation{From: pkg, To: rel, Label: labelContains})

	// Detail layer. Go gets precise AST extraction; every other language falls
	// back to import regexes so the graph is never empty.
	if strings.EqualFold(filepath.Ext(rel), ".go") {
		if b.addGoDetail(repoDir, rel) {
			return
		}
	}
	for _, imp := range regexImports(repoDir, rel) {
		b.rels = append(b.rels, graph.Relation{From: rel, To: imp, Label: labelDepends})
	}
}

// ensurePackage emits a package entity the first time a package is seen.
func (b *builder) ensurePackage(pkg string) {
	if pkg == "" || b.pkgs[pkg] {
		return
	}
	b.pkgs[pkg] = true
	base := filepath.Base(pkg)
	b.ents = append(b.ents, graph.Entity{
		Name:    pkg,
		Type:    typePackage,
		Aliases: dedupAliases(pkg, base, symbolAliases(base)),
	})
}

// packageName is the repo-relative directory of a file, used as the package
// entity name. Root-level files map to "." so they still have a container.
func packageName(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "" || dir == "." {
		return "."
	}
	return dir
}

// symbolAliases returns discovery aliases for an identifier or filename stem: a
// space-joined, lowercased camelCase/snake_case word split, so mentions like
// "payment service" hit PaymentService / payment_service. Empty when the name
// is a single word (no extra alias needed).
func symbolAliases(name string) []string {
	words := splitWords(name)
	if len(words) < 2 {
		return nil
	}
	return []string{strings.ToLower(strings.Join(words, " "))}
}

// splitWords breaks a camelCase / snake_case / kebab identifier into words.
func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == ' ' || r == '.':
			flush()
		case isUpper(r) && i > 0 && !isUpper(runes[i-1]):
			// camelCase boundary: lower|digit -> Upper
			flush()
			cur.WriteRune(r)
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

// dedupAliases flattens the given alias sources, drops empties, the canonical
// name itself, and case-insensitive duplicates, preserving order.
func dedupAliases(canonical string, parts ...any) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(canonical)): true}
	var out []string
	add := func(s string) {
		k := strings.ToLower(strings.TrimSpace(s))
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			add(v)
		case []string:
			for _, s := range v {
				add(s)
			}
		}
	}
	return out
}
