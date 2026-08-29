package v1_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestImportAllowed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "standard library", path: "unicode/utf8", want: true},
		{name: "cgo pseudo-package", path: "C", want: false},
		{name: "HTTP", path: "net/http", want: false},
		{name: "Harness", path: "github.com/looprig/harness/pkg/serve", want: false},
		{name: "Storage", path: "github.com/looprig/storage", want: false},
		{name: "Factory", path: "github.com/looprig/factory", want: false},
		{name: "Host", path: "github.com/looprig/host", want: false},
		{name: "Centrifuge", path: "github.com/centrifugal/centrifuge-go", want: false},
		{name: "terminal UI", path: "github.com/looprig/tui", want: false},
		{name: "web UI", path: "github.com/looprig/wui", want: false},
		{name: "other external package", path: "github.com/looprig/core/uuid", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := importAllowed(tt.path); got != tt.want {
				t.Errorf("importAllowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestProductionImportsStayTransportNeutral parses every non-test source file in
// this package. sessionwire/v1 is Core's transport-neutral contract, so it may
// depend only on the standard library and must never reach outward into Harness,
// Storage, Factory, Host, Centrifuge, HTTP, or a UI package.
func TestProductionImportsStayTransportNeutral(t *testing.T) {
	t.Parallel()

	dir := packageDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read package directory %q: %v", dir, err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !isProductionFile(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse production file %q: %v", path, err)
		}
		for _, imp := range file.Imports {
			importPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %q in %s: %v", imp.Path.Value, path, err)
			}
			if !importAllowed(importPath) {
				t.Errorf("production file %s imports disallowed path %q; sessionwire/v1 may import only non-HTTP standard-library packages",
					entry.Name(), importPath)
			}
		}
	}
}

func importAllowed(importPath string) bool {
	return isStdlib(importPath) && !isForbiddenImport(importPath)
}

func isStdlib(importPath string) bool {
	// C is cgo's pseudo-package, not a standard-library import.
	if importPath == "C" {
		return false
	}
	first, _, _ := strings.Cut(importPath, "/")
	return !strings.Contains(first, ".")
}

func isForbiddenImport(importPath string) bool {
	if importPath == "net/http" || strings.HasPrefix(importPath, "net/http/") {
		return true
	}
	for _, part := range strings.Split(strings.ToLower(importPath), "/") {
		switch part {
		case "harness", "storage", "factory", "host", "ui", "tui", "wui", "webui":
			return true
		}
		if strings.Contains(part, "centrifuge") {
			return true
		}
	}
	return false
}

func isProductionFile(name string) bool {
	return strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
}

func packageDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve package directory: %v", err)
	}
	return dir
}
