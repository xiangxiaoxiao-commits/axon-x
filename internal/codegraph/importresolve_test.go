package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"axon/internal/graph"
)

// writeModule lays out a tiny Go module so skeleton extraction can be exercised
// deterministically, without depending on the repository this test lives in.
func writeModule(t *testing.T, modulePath string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module "+modulePath+"\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// depsOf returns the 依赖 targets emitted for one file.
func depsOf(t *testing.T, rels []graph.Relation, from string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, r := range rels {
		if r.Label == labelDepends && r.From == from {
			out[r.To] = true
		}
	}
	return out
}

// An intra-repo import must be recorded as the package entity's own name. Package
// entities are named by repo-relative directory while an import statement spells
// the module-qualified path, so emitting the raw import path made every such edge
// dangle and be discarded at assembly.
func TestIntraRepoImportResolvesToPackageEntity(t *testing.T) {
	dir := writeModule(t, "example.com/app", map[string]string{
		"main.go": `package main

import (
	"fmt"

	"example.com/app/internal/svc"
	"example.com/app/internal/db"
)

func main() {
	fmt.Println(svc.Name, db.DSN)
}
`,
		"internal/svc/svc.go": "package svc\n\nconst Name = \"svc\"\n",
		"internal/db/db.go":   "package db\n\nconst DSN = \"dsn\"\n",
	})

	ents, rels, err := BuildSkeleton(dir)
	if err != nil {
		t.Fatal(err)
	}

	// The emitted package entities must be named by repo-relative directory.
	entNames := map[string]bool{}
	for _, e := range ents {
		entNames[e.Name] = true
	}
	for _, want := range []string{"internal/svc", "internal/db"} {
		if !entNames[want] {
			t.Fatalf("package entity %q missing; entities = %v", want, entNames)
		}
	}

	deps := depsOf(t, rels, "main.go")
	for _, want := range []string{"internal/svc", "internal/db"} {
		if !deps[want] {
			t.Errorf("intra-repo dependency on %q not recorded as the entity name; got %v", want, deps)
		}
	}
	// The module-qualified spelling must not appear: nothing would resolve it.
	for _, unwanted := range []string{"example.com/app/internal/svc", "example.com/app/internal/db"} {
		if deps[unwanted] {
			t.Errorf("raw module-qualified import %q was kept as a relation target", unwanted)
		}
	}
	// External imports stay as written — they are meant to fall away at
	// assembly, which is how the code graph filters stdlib noise.
	if !deps["fmt"] {
		t.Errorf("external import fmt was rewritten; got %v", deps)
	}

	// Every intra-repo dependency must resolve against an emitted entity, which
	// is the property that was actually broken.
	keys := map[string]bool{}
	for _, e := range ents {
		keys[graph.NormName(e.Name)] = true
		for _, a := range e.Aliases {
			keys[graph.NormName(a)] = true
		}
	}
	for _, r := range rels {
		if r.Label != labelDepends {
			continue
		}
		if r.To == "internal/svc" || r.To == "internal/db" {
			if !keys[graph.NormName(r.To)] {
				t.Errorf("dependency %s -> %s still dangles", r.From, r.To)
			}
		}
	}
}

// A module path may itself be a prefix of an external path, and a nested module
// path must only strip its own prefix.
func TestImportTargetOnlyStripsThisModule(t *testing.T) {
	b := &builder{pkgs: map[string]bool{}, module: "example.com/app"}
	cases := map[string]string{
		"example.com/app":              ".",
		"example.com/app/internal/db":  "internal/db",
		"example.com/appx/other":       "example.com/appx/other",
		"fmt":                          "fmt",
		"github.com/example.com/app/x": "github.com/example.com/app/x",
		"example.com/app/":             "example.com/app/",
		"":                             "",
	}
	for in, want := range cases {
		if got := b.importTarget(in); got != want {
			t.Errorf("importTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// Without a module path (a repo with no go.mod, or a non-Go tree) the mapping is
// the identity, so the generic import path still works.
func TestImportTargetIsIdentityWithoutModule(t *testing.T) {
	b := &builder{pkgs: map[string]bool{}}
	if got := b.importTarget("example.com/app/internal/db"); got != "example.com/app/internal/db" {
		t.Errorf("importTarget rewrote %q with no module known", got)
	}
}

func TestModulePathReadsGoMod(t *testing.T) {
	dir := writeModule(t, "example.com/app", nil)
	if got := modulePath(dir); got != "example.com/app" {
		t.Errorf("modulePath = %q, want example.com/app", got)
	}
	// A quoted module path is legal in go.mod.
	quoted := t.TempDir()
	if err := os.WriteFile(filepath.Join(quoted, "go.mod"),
		[]byte("module \"example.com/quoted\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := modulePath(quoted); got != "example.com/quoted" {
		t.Errorf("modulePath = %q, want example.com/quoted", got)
	}
	// No go.mod: empty, and the caller must cope.
	if got := modulePath(t.TempDir()); got != "" {
		t.Errorf("modulePath with no go.mod = %q, want empty", got)
	}
}

// Sanity check against the repository this test lives in: every intra-repo
// dependency the extractor emits must resolve to a package entity. Guards the
// real-data shape, not just the synthetic fixture.
func TestThisRepoDependencyEdgesResolve(t *testing.T) {
	repoDir := "../.."
	if _, err := os.Stat(filepath.Join(repoDir, "go.mod")); err != nil {
		t.Skip("no go.mod at module root")
	}
	module := modulePath(repoDir)
	if module == "" {
		t.Skip("could not read module path")
	}
	ents, rels, err := BuildSkeleton(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, e := range ents {
		keys[graph.NormName(e.Name)] = true
		for _, a := range e.Aliases {
			keys[graph.NormName(a)] = true
		}
	}
	var intra, resolved int
	for _, r := range rels {
		if r.Label != labelDepends {
			continue
		}
		// Only edges that were rewritten from this module's namespace count as
		// intra-repo; external ones are expected to dangle.
		if r.To == "fmt" || !keys[graph.NormName(r.To)] && !looksLikeRepoPackage(repoDir, r.To) {
			continue
		}
		intra++
		if keys[graph.NormName(r.To)] {
			resolved++
		} else {
			t.Errorf("intra-repo dependency dangles: %s -> %s", r.From, r.To)
		}
	}
	if intra == 0 {
		t.Fatal("no intra-repo dependency found; the extractor or the check is broken")
	}
	t.Logf("仓库内依赖边 %d 条，全部可解析", resolved)
}

// looksLikeRepoPackage reports whether target names a directory that exists in
// the repo, i.e. it is one of our own packages rather than an external one.
func looksLikeRepoPackage(repoDir, target string) bool {
	if target == "" || target == "." {
		return true
	}
	info, err := os.Stat(filepath.Join(repoDir, filepath.FromSlash(target)))
	return err == nil && info.IsDir()
}
