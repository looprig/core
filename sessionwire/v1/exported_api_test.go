package v1_test

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// updateExportedAPI regenerates the golden listing. Regeneration is deliberately
// an explicit act: run
//
//	GOWORK=off go test ./sessionwire/v1 -run TestExportedAPIIsFrozen -update-api
//
// only after reviewing the public contract change it records.
var updateExportedAPI = flag.Bool("update-api", false, "rewrite the exported-API golden listing")

const exportedAPIGoldenPath = "testdata/exported_api.txt"

// TestExportedAPIIsFrozen is the F1.1 freeze. sessionwire/v1 is the wire
// contract every later lane (sessionstore, harness, host, factory, wui)
// compiles against, so an identifier that is added, renamed, removed, or has
// its shape changed must be a reviewed decision rather than a side effect.
//
// The listing is produced from the package's non-test files with go/ast, which
// keeps the freeze stdlib-only and independent of the compiled surface a
// consumer happens to touch.
func TestExportedAPIIsFrozen(t *testing.T) {
	t.Parallel()

	got := strings.Join(exportedAPIListing(t), "\n") + "\n"

	if *updateExportedAPI {
		if err := os.WriteFile(exportedAPIGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write exported-API golden: %v", err)
		}
		t.Logf("rewrote %s", exportedAPIGoldenPath)
		return
	}

	want, err := os.ReadFile(exportedAPIGoldenPath)
	if err != nil {
		t.Fatalf("read exported-API golden: %v", err)
	}
	if got == string(want) {
		return
	}

	t.Errorf(`the exported surface of sessionwire/v1 no longer matches %s.

This package is the frozen V1 wire contract; every later lane compiles against
it. Review the public contract change below, decide deliberately whether it is
intended, and only then regenerate with:

    GOWORK=off go test ./sessionwire/v1 -run TestExportedAPIIsFrozen -update-api

%s`, exportedAPIGoldenPath, diffExportedAPI(string(want), got))
}

// diffExportedAPI reports the added and removed listing lines.
func diffExportedAPI(want, got string) string {
	previous := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		previous[line] = struct{}{}
	}
	current := map[string]struct{}{}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		current[line] = struct{}{}
	}
	var report strings.Builder
	for _, line := range sortedKeys(previous) {
		if _, ok := current[line]; !ok {
			fmt.Fprintf(&report, "removed: %s\n", line)
		}
	}
	for _, line := range sortedKeys(current) {
		if _, ok := previous[line]; !ok {
			fmt.Fprintf(&report, "  added: %s\n", line)
		}
	}
	if report.Len() == 0 {
		return "(only line ordering changed)\n"
	}
	return report.String()
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// exportedAPIListing enumerates exported types, their exported fields and
// methods, functions, constants, and variables declared in this package.
func exportedAPIListing(t *testing.T) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if file.Name.Name != "v1" {
			t.Fatalf("%s declares package %q, want v1", name, file.Name.Name)
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		t.Fatal("no non-test Go files found; the exported-surface freeze would be vacuous")
	}

	render := func(node ast.Node) string {
		var buffer bytes.Buffer
		if err := printer.Fprint(&buffer, fileSet, node); err != nil {
			t.Fatalf("render %T: %v", node, err)
		}
		return strings.Join(strings.Fields(buffer.String()), " ")
	}

	var lines []string
	for _, file := range files {
		for _, decl := range file.Decls {
			switch typed := decl.(type) {
			case *ast.FuncDecl:
				lines = append(lines, exportedFuncLines(typed, render)...)
			case *ast.GenDecl:
				lines = append(lines, exportedGenDeclLines(typed, render)...)
			}
		}
	}
	sort.Strings(lines)
	return lines
}

func exportedFuncLines(decl *ast.FuncDecl, render func(ast.Node) string) []string {
	if !decl.Name.IsExported() {
		return nil
	}
	signature := render(decl.Type)
	signature = strings.TrimPrefix(signature, "func")
	if decl.Recv == nil {
		return []string{fmt.Sprintf("func %s%s", decl.Name.Name, signature)}
	}
	if len(decl.Recv.List) != 1 {
		return nil
	}
	receiver := render(decl.Recv.List[0].Type)
	base := strings.TrimPrefix(receiver, "*")
	if base == "" || !ast.IsExported(base) {
		return nil
	}
	return []string{fmt.Sprintf("method (%s) %s%s", receiver, decl.Name.Name, signature)}
}

func exportedGenDeclLines(decl *ast.GenDecl, render func(ast.Node) string) []string {
	var lines []string
	for _, spec := range decl.Specs {
		switch typed := spec.(type) {
		case *ast.TypeSpec:
			if !typed.Name.IsExported() {
				continue
			}
			alias := ""
			if typed.Assign.IsValid() {
				alias = "= "
			}
			lines = append(lines, fmt.Sprintf("type %s %s%s", typed.Name.Name, alias, exportedTypeShape(typed.Type, render)))
			lines = append(lines, exportedTypeMemberLines(typed, render)...)
		case *ast.ValueSpec:
			kind := "var"
			if decl.Tok == token.CONST {
				kind = "const"
			}
			for index, name := range typed.Names {
				if !name.IsExported() {
					continue
				}
				line := fmt.Sprintf("%s %s", kind, name.Name)
				if typed.Type != nil {
					line += " " + render(typed.Type)
				}
				// The on-the-wire string literals are the whole point of this
				// contract, so freeze the value as well as the name and type:
				// renaming CommandStateAccepted's "accepted" or bumping
				// CurrentWireVersion is a wire break, not an internal edit.
				if index < len(typed.Values) {
					line += " = " + render(typed.Values[index])
				}
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// exportedTypeShape names a composite type by its kind and records the full
// spelling of every other underlying type, so a widened or renamed underlying
// type is itself a reviewed contract change.
func exportedTypeShape(expr ast.Expr, render func(ast.Node) string) string {
	switch expr.(type) {
	case *ast.StructType:
		return "struct"
	case *ast.InterfaceType:
		return "interface"
	default:
		return render(expr)
	}
}

func exportedTypeMemberLines(spec *ast.TypeSpec, render func(ast.Node) string) []string {
	var lines []string
	switch underlying := spec.Type.(type) {
	case *ast.StructType:
		for _, field := range underlying.Fields.List {
			rendered := render(field.Type)
			tag := ""
			if field.Tag != nil {
				tag = " " + field.Tag.Value
			}
			if len(field.Names) == 0 {
				if base := strings.TrimPrefix(rendered, "*"); ast.IsExported(lastIdentifier(base)) {
					lines = append(lines, fmt.Sprintf("field %s.%s %s%s", spec.Name.Name, lastIdentifier(base), rendered, tag))
				}
				continue
			}
			for _, name := range field.Names {
				if !name.IsExported() {
					continue
				}
				lines = append(lines, fmt.Sprintf("field %s.%s %s%s", spec.Name.Name, name.Name, rendered, tag))
			}
		}
	case *ast.InterfaceType:
		for _, method := range underlying.Methods.List {
			rendered := render(method.Type)
			if len(method.Names) == 0 {
				lines = append(lines, fmt.Sprintf("embeds %s.%s", spec.Name.Name, rendered))
				continue
			}
			for _, name := range method.Names {
				if !name.IsExported() {
					continue
				}
				lines = append(lines, fmt.Sprintf("method %s.%s%s", spec.Name.Name, name.Name, strings.TrimPrefix(rendered, "func")))
			}
		}
	}
	return lines
}

func lastIdentifier(rendered string) string {
	if index := strings.LastIndex(rendered, "."); index >= 0 {
		return rendered[index+1:]
	}
	return rendered
}
