package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"axon/internal/graph"
)

// writeFile creates dir/name with content under root, making parent dirs.
func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// entityByType indexes entities by (type -> set of names).
func entityByType(ents []graph.Entity) map[string]map[string]graph.Entity {
	out := map[string]map[string]graph.Entity{}
	for _, e := range ents {
		if out[e.Type] == nil {
			out[e.Type] = map[string]graph.Entity{}
		}
		out[e.Type][e.Name] = e
	}
	return out
}

func hasRelation(rels []graph.Relation, from, to, label string) bool {
	for _, r := range rels {
		if r.From == from && r.To == to && r.Label == label {
			return true
		}
	}
	return false
}

func TestBuildSkeletonGo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/pay/service.go", `package pay

import "fmt"

// PaymentService handles payments.
type PaymentService struct{}

func NewPaymentService() *PaymentService { return &PaymentService{} }

func (s *PaymentService) Charge(amount int) {
	fmt.Println(amount)
	validate(amount)
}

func validate(n int) bool { return n > 0 }
`)
	// A non-Go file to exercise the generic import layer.
	writeFile(t, root, "web/app.ts", `import { thing } from './thing';
const x = require('lodash');
`)
	// A file that must be skipped.
	writeFile(t, root, "vendor/lib/dep.go", `package lib
func Unused() {}
`)

	ents, rels, err := BuildSkeleton(root)
	if err != nil {
		t.Fatal(err)
	}
	byType := entityByType(ents)

	// file entities exist for both source files, not the vendored one.
	if _, ok := byType[typeFile]["internal/pay/service.go"]; !ok {
		t.Errorf("missing file entity for service.go")
	}
	if _, ok := byType[typeFile]["web/app.ts"]; !ok {
		t.Errorf("missing file entity for app.ts")
	}
	if _, ok := byType[typeFile]["vendor/lib/dep.go"]; ok {
		t.Errorf("vendored file must be skipped")
	}

	// package entity for the Go dir, with the package base name as alias.
	pkg, ok := byType[typePackage]["internal/pay"]
	if !ok {
		t.Fatalf("missing package entity internal/pay; packages=%v", byType[typePackage])
	}
	if !containsFold(pkg.Aliases, "pay") {
		t.Errorf("package internal/pay should alias 'pay', got %v", pkg.Aliases)
	}

	// function + type entities (Go AST), prefixed with package name.
	fn, ok := byType[typeFunction]["pay.Charge"]
	if !ok {
		t.Fatalf("missing function entity pay.Charge; funcs=%v", byType[typeFunction])
	}
	if !containsFold(fn.Aliases, "Charge") {
		t.Errorf("function pay.Charge should alias short name 'Charge', got %v", fn.Aliases)
	}
	if _, ok := byType[typeType]["pay.PaymentService"]; !ok {
		t.Fatalf("missing type entity pay.PaymentService; types=%v", byType[typeType])
	}
	// discovery alias: camel split "payment service" on the type.
	if !containsFold(byType[typeType]["pay.PaymentService"].Aliases, "payment service") {
		t.Errorf("PaymentService should carry 'payment service' alias, got %v",
			byType[typeType]["pay.PaymentService"].Aliases)
	}

	// contains: package -> file, file -> function/type.
	if !hasRelation(rels, "internal/pay", "internal/pay/service.go", labelContains) {
		t.Errorf("missing package->file contains relation")
	}
	if !hasRelation(rels, "internal/pay/service.go", "pay.Charge", labelContains) {
		t.Errorf("missing file->function contains relation")
	}
	if !hasRelation(rels, "internal/pay/service.go", "pay.PaymentService", labelContains) {
		t.Errorf("missing file->type contains relation")
	}

	// depends: Go import via AST.
	if !hasRelation(rels, "internal/pay/service.go", "fmt", labelDepends) {
		t.Errorf("missing Go import 'fmt' dependency")
	}

	// calls: Charge -> validate (same-package, prefixed).
	if !hasRelation(rels, "pay.Charge", "pay.validate", labelCalls) {
		t.Errorf("missing call relation pay.Charge->pay.validate; rels=%v", rels)
	}

	// generic import layer for the TS file.
	if !hasRelation(rels, "web/app.ts", "./thing", labelDepends) {
		t.Errorf("missing TS import './thing'")
	}
	if !hasRelation(rels, "web/app.ts", "lodash", labelDepends) {
		t.Errorf("missing TS require 'lodash'")
	}
}

func TestSelectKeyFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	writeFile(t, root, "internal/util/helper.go", "package util\nfunc H() {}\n")

	files, err := ListSourceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	_, rels, err := BuildSkeleton(root)
	if err != nil {
		t.Fatal(err)
	}
	keys := SelectKeyFiles(root, files, rels, 1)
	if len(keys) != 1 {
		t.Fatalf("want 1 key file, got %d (%v)", len(keys), keys)
	}
	// main.go is an entry point -> should outrank a plain helper.
	if keys[0] != "main.go" {
		t.Errorf("expected main.go to rank first, got %q", keys[0])
	}
}

func containsFold(list []string, want string) bool {
	for _, s := range list {
		if len(s) == len(want) && equalFold(s, want) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
