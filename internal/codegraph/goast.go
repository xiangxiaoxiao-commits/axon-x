package codegraph

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"

	"axon/internal/graph"
)

// addGoDetail parses a Go file with go/ast and emits function/type entities,
// contains relations (file -> symbol), import dependencies (file -> package)
// and intra-file/package call edges (function -> function). Returns false when
// the file can't be read or parsed, so the caller falls back to regex imports.
//
// Symbol Name uses a "pkg.Symbol" prefix (e.g. graph.Merge) to avoid same-name
// collisions across packages; the bare short name and word-split go into aliases
// so both precise lookup and natural-language recall work.
func (b *builder) addGoDetail(repoDir, rel string) bool {
	src := readFileBounded(repoDir, rel)
	if src == "" {
		return false
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Base(rel), src, parser.SkipObjectResolution)
	if err != nil {
		return false
	}
	pkg := f.Name.Name // Go package identifier, e.g. "graph"

	// Imports -> dependency relations (accurate, no regex needed).
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if p != "" {
			b.rels = append(b.rels, graph.Relation{From: rel, To: p, Label: labelDepends})
		}
	}

	// Top-level declarations: functions/methods and named types.
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			name := qualify(pkg, d.Name.Name)
			short := d.Name.Name
			aliases := dedupAliases(name, short, receiverForm(d), symbolAliases(short))
			b.ents = append(b.ents, graph.Entity{Name: name, Type: typeFunction, Aliases: aliases})
			b.rels = append(b.rels, graph.Relation{From: rel, To: name, Label: labelContains})
			// Calls made inside this function's body.
			for _, callee := range callees(pkg, d.Body) {
				b.rels = append(b.rels, graph.Relation{From: name, To: callee, Label: labelCalls})
			}
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				name := qualify(pkg, ts.Name.Name)
				b.ents = append(b.ents, graph.Entity{
					Name:    name,
					Type:    typeType,
					Aliases: dedupAliases(name, ts.Name.Name, symbolAliases(ts.Name.Name)),
				})
				b.rels = append(b.rels, graph.Relation{From: rel, To: name, Label: labelContains})
			}
		}
	}
	return true
}

// qualify prefixes a symbol with its Go package name (pkg.Symbol) for a unique,
// collision-resistant entity name.
func qualify(pkg, sym string) string {
	if pkg == "" {
		return sym
	}
	return pkg + "." + sym
}

// receiverForm renders a method's receiver form like "(*Graph).Merge" as an
// alias, so method mentions that include the type still resolve. Empty for
// plain functions.
func receiverForm(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return ""
	}
	recv := typeName(d.Recv.List[0].Type)
	if recv == "" {
		return ""
	}
	return "(" + recv + ")." + d.Name.Name
}

// typeName renders a receiver type expression to a short string (T or *T).
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeName(t.X)
	case *ast.IndexExpr: // generic receiver T[P]
		return typeName(t.X)
	case *ast.IndexListExpr:
		return typeName(t.X)
	}
	return ""
}

// callees walks a function body and returns the qualified names of the
// functions it calls. Same-package calls (bare identifiers) are prefixed with
// pkg so they match local function entities; selector calls on other packages
// (fmt.Println) are kept as "pkg.Sel" for readability but rarely resolve to a
// local node — that's fine, they add call-context observations, not hard edges.
func callees(pkg string, body *ast.BlockStmt) []string {
	if body == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident: // localFunc(...)
			name = qualify(pkg, fn.Name)
		case *ast.SelectorExpr: // x.Method(...) — best-effort "x.Method"
			if x, ok := fn.X.(*ast.Ident); ok {
				name = x.Name + "." + fn.Sel.Name
			}
		}
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return true
	})
	return out
}
