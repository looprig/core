package blocktest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/looprig/core/content"
)

// TestCorePackageDirIsThisPackagesParent pins the resolution this package gained
// by moving inside core. go/build is still what locates the content package —
// a consumer in another module reaches it through a module cache or a vendor
// tree — but the answer is now checkable, because this package sits in a known
// place relative to it. A resolution that wandered to another checkout would
// parse declarations no test binary contains and report completeness about the
// wrong source.
func TestCorePackageDirIsThisPackagesParent(t *testing.T) {
	t.Parallel()

	dir, err := corePackageDir()
	if err != nil {
		t.Fatalf("corePackageDir() error = %v", err)
	}
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	if want := filepath.Dir(working); dir != want {
		t.Errorf("corePackageDir() = %q, want this package's parent %q", dir, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "block.go")); err != nil {
		t.Errorf("resolved content directory %q has no block.go: %v", dir, err)
	}
}

// TestCoreUnionsAreDiscoverable is the guard on the completeness guard. The
// variant check is driven by parsing core's own source, so a resolution or
// parse that quietly returned nothing would make every consumer's completeness
// assertion pass vacuously — the precise failure mode the check exists to
// remove. This pins that the declarations really were found, and that the two
// independent statements of the Block union (wire tags and marker methods)
// agree on its size.
func TestCoreUnionsAreDiscoverable(t *testing.T) {
	t.Parallel()

	unions, err := loadCoreUnions()
	if err != nil {
		t.Fatalf("loadCoreUnions() error = %v", err)
	}
	if len(unions.blockTags) != len(unions.blockVariants) {
		t.Errorf("%d BlockType constants for %d Block variants; the sealed union is not 1:1",
			len(unions.blockTags), len(unions.blockVariants))
	}
	if _, ok := unions.blockVariants["TextBlock"]; !ok {
		t.Errorf("Block variants %v do not include TextBlock; the parse found the wrong package", unions.blockVariants)
	}
	if _, ok := unions.chunkVariants["TextChunk"]; !ok {
		t.Errorf("Chunk variants %v do not include TextChunk; the parse found the wrong package", unions.chunkVariants)
	}
	if _, ok := unions.blockTags[content.TypeText]; !ok {
		t.Errorf("BlockType constants %v do not include %q", unions.blockTags, content.TypeText)
	}
}

// TestFixturesCoverEverySealedVariant runs both completeness checks against the
// real fixtures. Consumers reach them through Blocks/Chunks, but only when a
// consumer test runs; this keeps the package self-checking on its own.
func TestFixturesCoverEverySealedVariant(t *testing.T) {
	t.Parallel()

	if got := len(Blocks(t)); got == 0 {
		t.Fatal("Blocks() returned no fixtures")
	}
	if got := len(Chunks(t)); got == 0 {
		t.Fatal("Chunks() returned no fixtures")
	}
}

// TestSameSetReportsDrift exercises the comparison's failure path in both
// directions, which is the behavior every other test in the repository depends
// on and none of them can observe.
func TestSameSetReportsDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		declared map[string]struct{}
		covered  map[string]struct{}
		wantErr  bool
	}{
		{
			name:     "identical sets agree",
			declared: map[string]struct{}{"TextBlock": {}, "RefusalBlock": {}},
			covered:  map[string]struct{}{"TextBlock": {}, "RefusalBlock": {}},
		},
		{
			name:     "a declared variant with no fixture is drift",
			declared: map[string]struct{}{"TextBlock": {}, "RefusalBlock": {}},
			covered:  map[string]struct{}{"TextBlock": {}},
			wantErr:  true,
		},
		{
			name:     "a fixture for a removed variant is drift",
			declared: map[string]struct{}{"TextBlock": {}},
			covered:  map[string]struct{}{"TextBlock": {}, "GhostBlock": {}},
			wantErr:  true,
		},
		{
			name:     "empty sets agree",
			declared: map[string]struct{}{},
			covered:  map[string]struct{}{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := sameSet("test variant", tt.declared, tt.covered)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sameSet() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSharedReferencesSeesAnAliasedNestedBlock is the guard on AssertIndependent,
// and it exists because AssertIndependent could not previously see the aliasing
// that matters most.
//
// The only nested-block fixture was a &content.TextBlock{}, whose sole field is
// a string. The traversal reports on []uint8 alone, so a clone that assigned
// `cloned.Content = typed.Content` — sharing the slice header AND every element
// pointer — reached a nested value with no byte-backed field and was reported
// clean. Every consumer's independence assertion was passing on a clone that
// was not one.
//
// Two things had to change together: the nested fixture must CARRY bytes, and
// the traversal must check pointer identity before recursing. Either alone
// leaves a hole, so this drives the aliasing clone directly rather than trusting
// that a consumer would have caught it.
func TestSharedReferencesSeesAnAliasedNestedBlock(t *testing.T) {
	t.Parallel()

	orig := &content.ToolResultBlock{}
	Populate(t, orig)

	// The exact clone every helper in Harness is written to avoid: a struct
	// copy that reassigns nothing, so both the nested slice header and each
	// element pointer are the original's.
	aliased := *orig
	shared := sharedReferences(reflect.ValueOf(orig), reflect.ValueOf(&aliased), "ToolResultBlock")
	if len(shared) == 0 {
		t.Fatal("sharedReferences reported nothing for a clone that shares every nested block; " +
			"the nested fixture needs a byte-backed field and the traversal needs a pointer identity check")
	}
	if !strings.Contains(shared[0].Error(), "ToolResultBlock.Content[0]") {
		t.Errorf("shared reference = %v, want it to name the nested block that is shared", shared[0])
	}

	// Positive control: a real deep copy of the same fixture reports nothing,
	// so the check above is not simply always-fails.
	deep := *orig
	deep.Content = []content.Block{cloneToolUseForTest(t, orig.Content[0])}
	if shared := sharedReferences(reflect.ValueOf(orig), reflect.ValueOf(&deep), "ToolResultBlock"); len(shared) != 0 {
		t.Errorf("sharedReferences on a genuine deep copy = %v, want none", shared)
	}
}

// cloneToolUseForTest is the correct copy the positive control needs: core's own
// constructor, which copies both raw messages.
func cloneToolUseForTest(t *testing.T, block content.Block) content.Block {
	t.Helper()
	typed, ok := block.(*content.ToolUseBlock)
	if !ok {
		t.Fatalf("nested fixture = %T, want a byte-backed *content.ToolUseBlock; "+
			"a nested block with no byte-backed field cannot exercise the aliasing check", block)
	}
	return content.NewToolUseBlock(typed.ID, typed.Name, typed.Input, typed.ProviderState, typed.ProviderStateFormat)
}
