// Package blocktest builds fully populated content.Block fixtures by
// reflection so every copy, encode, and decode path in the workspace can be
// tested for FIELD-BY-FIELD completeness rather than for the handful of fields
// whose names a hand-written fixture happened to mention.
//
// It lives beside the sealed unions it guards rather than inside any one
// consumer. The defect it prevents is a property of content itself — a struct
// literal or a type switch that enumerates a union's members by hand — and it
// occurs wherever content blocks are copied or translated: Harness copies and
// re-encodes them in three independent places (the loop runtime's message
// clone, the hook payload clone, and the compaction wire codec), the inference
// codecs build them in every decode path and consume them in every encode
// path, and foreignloops, acp, tui and mcp each carry a copy of their own. A
// literal silently drops any field added to content afterwards: the code still
// compiles, the existing tests still pass, and the new field is simply gone.
// That is exactly how ThinkingBlock.ProviderState and ToolUseBlock.ProviderState
// — the provider-private reasoning state whose loss makes signature replay
// impossible — went missing from all three Harness copies.
//
// Being a sibling of content rather than an internal of one consumer is the
// whole point: an internal package can only defend the six packages that may
// import it, and the drift it detects is not confined to those six.
//
// The fixtures here are therefore built by walking the struct with reflect and
// assigning every exported field a distinctive non-zero value. A field added
// to core is populated automatically, so a copy or codec that forgets it fails
// the round-trip assertion loudly instead of dropping it silently. A field
// whose type this package cannot yet populate fails the test with an explicit
// "extend blocktest" message rather than being skipped.
//
// The fixture LIST is guarded the same way. A hand-maintained slice literal of
// variants is blind to a variant added to core in exactly the way a struct
// literal is blind to a field: nothing fails, the new variant is simply never
// exercised by any consumer's round-trip test. Blocks and Chunks therefore
// check themselves against the sealed unions as core's own source declares
// them — the content.BlockType constants and the isBlock/isChunk marker
// methods — so a new core variant fails here automatically instead of being
// noticed by luck.
//
// This package is test support only. It is deliberately not under a _test.go
// file because several separate packages, in several separate modules, consume
// it. It imports testing, so it must never be imported by non-test code.
package blocktest

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/looprig/core/content"
)

// rawMessageType is the one []byte-backed type that must hold VALID JSON: the
// compaction wire codec re-emits it verbatim rather than base64-encoding it.
var rawMessageType = reflect.TypeOf(json.RawMessage(nil))

// blockSliceType is the nested-block field type on ToolResultBlock.
var blockSliceType = reflect.TypeOf([]content.Block(nil))

// Blocks returns one fully populated value of every content.Block variant, in
// the order the sealed union declares them. Every exported field of every
// returned block is non-zero, so a copy or codec that fails to carry a field
// forward is detectable by comparing against the fixture.
//
// The list is checked against core's own declarations before it is returned
// (see assertBlocksComplete), so a variant added to core fails every consumer
// of this fixture rather than silently going untested.
func Blocks(t *testing.T) []content.Block {
	t.Helper()
	blocks := []content.Block{
		&content.TextBlock{},
		&content.ImageBlock{},
		&content.AudioBlock{},
		&content.DocumentBlock{},
		&content.ThinkingBlock{},
		&content.ToolUseBlock{},
		&content.ToolResultBlock{},
		&content.RefusalBlock{},
	}
	assertBlocksComplete(t, blocks)
	for _, block := range blocks {
		Populate(t, block)
	}
	return blocks
}

// Chunks returns one fully populated value of every content.Chunk variant, in
// the order the sealed union declares them. Chunks have no wire codec, but they
// are copied and re-dispatched by the loop's streaming fold and by the live SSE
// transport, so the same field- and variant-completeness guarantees matter: a
// chunk field a fold forgets is a token that never reaches the accumulated
// block, and a chunk variant a dispatcher forgets is a delta the loop drops
// while still emitting a live event for it.
//
// The list is checked against core's isChunk marker methods before it is
// returned (see assertChunksComplete).
func Chunks(t *testing.T) []content.Chunk {
	t.Helper()
	chunks := []content.Chunk{
		&content.TextChunk{},
		&content.ThinkingChunk{},
		&content.ToolUseChunk{},
		&content.RefusalChunk{},
		&content.ImageChunk{},
	}
	assertChunksComplete(t, chunks)
	for _, chunk := range chunks {
		Populate(t, chunk)
	}
	return chunks
}

// corePackage is the import path of the sealed content unions this package
// mirrors — its own parent directory. It is resolved to a directory the same
// way the compiler resolves it, so the declarations checked against are always
// the ones actually compiled into the test binary (a vendored or workspace core
// alike).
const corePackage = "github.com/looprig/core/content"

// selfPackage is this package's own import path.
//
// Living inside core does not remove the need to resolve the content package
// through go/build: a consumer in another module reaches these declarations
// through a module-cache or vendored copy, and only the build's own resolution
// finds the copy that was actually compiled in. What living inside core adds is
// the ability to CHECK that resolution, which was previously unverifiable. The
// two paths are parent and child, so their resolved directories must be parent
// and child too; when they are not, go/build found a second core checkout and
// the parse would be describing declarations no test binary contains.
const selfPackage = corePackage + "/blocktest"

// assertBlocksComplete fails unless the fixture list is a BIJECTION with core's
// sealed Block union. It checks BOTH statements of that union's membership,
// because they can drift apart independently:
//
//   - every content.BlockType constant is produced by exactly one fixture, so a
//     new wire tag cannot exist with nothing exercising its codec path; and
//   - every isBlock marker method belongs to exactly one fixture's type, so a
//     new variant cannot exist with nothing exercising its copy paths, even
//     before anyone gives it a wire tag.
//
// Exactly one, not at least one: duplicate fixtures would let one variant's
// coverage stand in for another's in a set comparison, which is how a
// completeness check ends up reporting completeness it does not have.
//
// The tag of a fixture is read through content.MarshalBlock rather than from a
// table here, so the mapping under test is core's own discriminator. This is the
// only mechanism that stands between a variant added to core and a silent
// runtime drop: nothing about a new variant fails to compile, and Go type
// switches are not exhaustive, so a check that can be satisfied loosely is a
// check that will eventually be satisfied by accident.
func assertBlocksComplete(t *testing.T, blocks []content.Block) {
	t.Helper()
	declared := coreDeclarations(t)

	tags := make(map[content.BlockType]struct{}, len(blocks))
	for _, block := range blocks {
		tag := blockTag(t, block)
		if _, duplicate := tags[tag]; duplicate {
			t.Fatalf("blocktest: two fixtures encode to content.BlockType %q; each variant needs its own fixture", tag)
		}
		tags[tag] = struct{}{}
	}
	assertSameSet(t, "content.BlockType constant", declared.blockTags, tags)
	assertSameSet(t, "content.Block variant", declared.blockVariants, variantNames(t, blocks))
}

// assertChunksComplete is the Chunk half of the same guard, and equally strict.
// Chunks carry no wire tag (they are never serialized), so the isChunk marker
// methods are the only statement of the union's membership and therefore the
// only thing to check against.
func assertChunksComplete(t *testing.T, chunks []content.Chunk) {
	t.Helper()
	assertSameSet(t, "content.Chunk variant", coreDeclarations(t).chunkVariants, variantNames(t, chunks))
}

// blockTag returns the wire discriminator core assigns a fixture. A block that
// cannot be marshaled at all is a fixture bug, not a codec finding, so it fails
// here rather than inside a consumer's round-trip assertion.
func blockTag(t *testing.T, block content.Block) content.BlockType {
	t.Helper()
	encoded, err := content.MarshalBlock(block)
	if err != nil {
		t.Fatalf("blocktest: content.MarshalBlock(%T) error = %v", block, err)
	}
	var tagged struct {
		Type content.BlockType `json:"type"`
	}
	if err := json.Unmarshal(encoded, &tagged); err != nil {
		t.Fatalf("blocktest: %T encoded to unreadable JSON %s: %v", block, encoded, err)
	}
	return tagged.Type
}

// variantNames reports the concrete type name of each fixture, matching the
// receiver names on the sealed union's marker methods. A nil fixture or a
// repeated type fails: both would shrink the covered set without shrinking the
// list, hiding a variant that has no fixture of its own.
func variantNames[T any](t *testing.T, values []T) map[string]struct{} {
	t.Helper()
	names := make(map[string]struct{}, len(values))
	for _, value := range values {
		typ := reflect.TypeOf(value)
		for typ != nil && typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		if typ == nil {
			t.Fatalf("blocktest: a nil fixture cannot stand for a sealed-union variant")
		}
		if _, duplicate := names[typ.Name()]; duplicate {
			t.Fatalf("blocktest: two fixtures have type %s; each variant needs its own fixture", typ.Name())
		}
		names[typ.Name()] = struct{}{}
	}
	return names
}

// assertSameSet fails when declared and covered differ. The comparison itself
// is a plain function so it can be tested directly: a completeness check whose
// failure path is never exercised is a check nobody knows still works.
func assertSameSet[K comparable](t *testing.T, subject string, declared, covered map[K]struct{}) {
	t.Helper()
	if err := sameSet(subject, declared, covered); err != nil {
		t.Fatal(err)
	}
}

// sameSet reports the first difference between the members core declares and
// the members the fixtures cover, naming the offender. A member core declares
// but no fixture covers is the drift this package exists to catch; the reverse
// means a fixture outlived its variant.
func sameSet[K comparable](subject string, declared, covered map[K]struct{}) error {
	for member := range declared {
		if _, ok := covered[member]; !ok {
			return fmt.Errorf("blocktest: core declares %s %v with no fixture; add it to blocktest in sealed-union declaration order",
				subject, member)
		}
	}
	for member := range covered {
		if _, ok := declared[member]; !ok {
			return fmt.Errorf("blocktest: fixture covers %s %v that core no longer declares; remove it from blocktest",
				subject, member)
		}
	}
	return nil
}

// coreUnions is the parsed sealed-union membership of the content package,
// read once per test binary.
type coreUnions struct {
	blockTags     map[content.BlockType]struct{}
	blockVariants map[string]struct{}
	chunkVariants map[string]struct{}
}

var loadCoreUnions = sync.OnceValues(readCoreUnions)

// coreDeclarations returns core's declared union membership, failing the test
// when it cannot be read. It deliberately never SKIPS: a completeness check that
// silently opts out when it cannot see the declarations is exactly as blind as
// the hand-maintained list it replaced.
func coreDeclarations(t *testing.T) coreUnions {
	t.Helper()
	unions, err := loadCoreUnions()
	if err != nil {
		t.Fatalf("blocktest: cannot read the %s sealed unions: %v", corePackage, err)
	}
	return unions
}

// readCoreUnions parses the content package's own source for the two things
// that define its sealed unions: the BlockType constants and the isBlock /
// isChunk marker methods. Locating the package through go/build (not a relative
// path) resolves it exactly as the build does, so a vendored core is checked
// against its own declarations rather than against a newer checkout's.
func readCoreUnions() (coreUnions, error) {
	dir, err := corePackageDir()
	if err != nil {
		return coreUnions{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return coreUnions{}, err
	}
	unions := coreUnions{
		blockTags:     map[content.BlockType]struct{}{},
		blockVariants: map[string]struct{}{},
		chunkVariants: map[string]struct{}{},
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return coreUnions{}, err
		}
		if err := collectUnions(file, &unions); err != nil {
			return coreUnions{}, err
		}
	}
	// An empty result means the parse walked the wrong tree or the declaration
	// shape changed; either way the check would pass vacuously, which is the one
	// outcome it must never have.
	if len(unions.blockTags) == 0 || len(unions.blockVariants) == 0 || len(unions.chunkVariants) == 0 {
		return coreUnions{}, fmt.Errorf("no sealed-union declarations found in %s", dir)
	}
	return unions, nil
}

// corePackageDir resolves the content package to the directory the current
// build would compile. go/build consults the module graph (and the vendor
// directory when the build does), so workspace, module-cache, and vendored
// layouts all resolve correctly without this package knowing which is in use.
//
// It then resolves THIS package the same way and requires the two to be parent
// and child of one directory. See selfPackage: that equality is the only
// available evidence that the source about to be parsed belongs to the same
// checkout as the content package linked into the test binary, and a
// completeness check parsed from the wrong checkout is worse than none.
func corePackageDir() (string, error) {
	working, err := os.Getwd()
	if err != nil {
		return "", err
	}
	corePkg, err := build.Import(corePackage, working, build.FindOnly)
	if err != nil {
		return "", err
	}
	selfPkg, err := build.Import(selfPackage, working, build.FindOnly)
	if err != nil {
		return "", err
	}
	if parent := filepath.Dir(selfPkg.Dir); parent != corePkg.Dir {
		return "", fmt.Errorf("%s resolved to %s, which is not the parent of %s (%s); "+
			"go/build found a different checkout than the one compiled in",
			corePackage, corePkg.Dir, selfPackage, selfPkg.Dir)
	}
	return corePkg.Dir, nil
}

// collectUnions accumulates one parsed file's contribution to the union
// membership: `Type<X> BlockType = "<tag>"` constants and `func (*<X>) isBlock()`
// / `isChunk()` marker methods.
func collectUnions(file *ast.File, unions *coreUnions) error {
	for _, decl := range file.Decls {
		switch typed := decl.(type) {
		case *ast.GenDecl:
			if typed.Tok != token.CONST {
				continue
			}
			if err := collectBlockTags(typed, unions.blockTags); err != nil {
				return err
			}
		case *ast.FuncDecl:
			receiver := markerReceiver(typed)
			if receiver == "" {
				continue
			}
			switch typed.Name.Name {
			case "isBlock":
				unions.blockVariants[receiver] = struct{}{}
			case "isChunk":
				unions.chunkVariants[receiver] = struct{}{}
			}
		}
	}
	return nil
}

// collectBlockTags reads the string value of every BlockType constant. A
// constant whose value is not a plain string literal cannot be resolved without
// evaluating core's constant expressions, so it is an error rather than a skip:
// a silently skipped tag is an uncovered variant.
func collectBlockTags(decl *ast.GenDecl, tags map[content.BlockType]struct{}) error {
	for _, spec := range decl.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		ident, ok := value.Type.(*ast.Ident)
		if !ok || ident.Name != "BlockType" {
			continue
		}
		for _, expression := range value.Values {
			literal, ok := expression.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return fmt.Errorf("BlockType constant %v is not a string literal; extend blocktest to resolve it", value.Names)
			}
			tag, err := strconv.Unquote(literal.Value)
			if err != nil {
				return err
			}
			tags[content.BlockType(tag)] = struct{}{}
		}
	}
	return nil
}

// markerReceiver returns the pointer-receiver type name of a sealed-union
// marker method, or "" when the declaration is not one.
func markerReceiver(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) != 1 {
		return ""
	}
	pointer, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return ""
	}
	ident, ok := pointer.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// Populate fills every exported field of the struct behind ptr with a
// distinctive non-zero value derived from the field's path, so a copy that
// assigns the right type to the wrong field is caught alongside one that drops
// the field entirely. It fails the test when a field's type is not yet
// supported: an unsupported field must never be silently left at its zero
// value, because that is the failure mode this package exists to prevent.
func Populate(t *testing.T, ptr any) {
	t.Helper()
	value := reflect.ValueOf(ptr)
	if value.Kind() != reflect.Pointer || value.IsNil() || value.Elem().Kind() != reflect.Struct {
		t.Fatalf("blocktest.Populate: want a non-nil pointer to a struct, got %T", ptr)
	}
	fill(t, value.Elem(), value.Elem().Type().Name())
	assertFullyPopulated(t, value.Elem(), value.Elem().Type().Name())
}

// fill assigns target a non-zero value derived from path. It recurses into
// nested structs (content.ImageSource) and into the nested block slice on
// ToolResultBlock, both of which carry fields of their own.
func fill(t *testing.T, target reflect.Value, path string) {
	t.Helper()
	switch {
	case target.Type() == rawMessageType:
		// Valid JSON: the compaction wire codec passes RawMessage through
		// unquoted, so arbitrary bytes here would produce an invalid payload
		// and mask the field-preservation assertion behind a parse error.
		target.SetBytes([]byte(fmt.Sprintf(`{"blocktest":%q}`, path)))
		return
	case target.Type() == blockSliceType:
		// The nested fixture MUST carry a byte-backed field. AssertIndependent
		// reports on []uint8 and on pointer identity, so a nested
		// &content.TextBlock{} — a single string — gave a clone that shares the
		// whole nested block nothing to trip over, and every consumer's
		// independence assertion passed on a clone that was not one.
		// ToolUseBlock is the variant whose Input and ProviderState are the
		// bytes this package exists to protect.
		nested := &content.ToolUseBlock{}
		fill(t, reflect.ValueOf(nested).Elem(), path+".ToolUseBlock")
		target.Set(reflect.ValueOf([]content.Block{nested}))
		return
	}

	switch target.Kind() {
	case reflect.String:
		target.SetString(path)
	case reflect.Bool:
		target.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		target.SetInt(int64(len(path)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		target.SetUint(uint64(len(path)))
	case reflect.Slice:
		if target.Type().Elem().Kind() != reflect.Uint8 {
			t.Fatalf("blocktest: %s has unsupported slice element type %s; extend blocktest for the new core field",
				path, target.Type().Elem())
		}
		target.SetBytes([]byte(path))
	case reflect.Struct:
		for i := range target.NumField() {
			field := target.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			fill(t, target.Field(i), path+"."+field.Name)
		}
	default:
		t.Fatalf("blocktest: %s has unsupported kind %s; extend blocktest for the new core field",
			path, target.Kind())
	}
}

// assertFullyPopulated proves fill left no exported field at its zero value.
// It is the guard on the guard: fill could grow a branch that matches a type
// without assigning it, which would reintroduce exactly the silent gap this
// package removes.
func assertFullyPopulated(t *testing.T, target reflect.Value, path string) {
	t.Helper()
	if target.Kind() != reflect.Struct {
		if target.IsZero() {
			t.Fatalf("blocktest: %s was left at its zero value; extend blocktest for the new core field", path)
		}
		return
	}
	for i := range target.NumField() {
		field := target.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		assertFullyPopulated(t, target.Field(i), path+"."+field.Name)
	}
}

// AssertIndependent fails when clone reaches any mutable byte slice — or any
// nested block — that orig also reaches. Field equality alone does not prove a
// clone: a struct copy compares equal while still aliasing the original's
// backing arrays and sharing its nested pointers, which is the mutation leak
// every clone in Harness exists to prevent.
//
// The parameters are untyped on purpose. The traversal is driven entirely by
// reflection and is indifferent to the sealed union a value belongs to, so
// narrowing it to content.Block would exclude content.Chunk values for no
// reason beyond the signature.
func AssertIndependent(t *testing.T, orig, clone any) {
	t.Helper()
	for _, err := range sharedReferences(reflect.ValueOf(orig), reflect.ValueOf(clone), fmt.Sprintf("%T", orig)) {
		t.Error(err)
	}
}

// sharedReferences reports every place clone reaches the same memory orig
// reaches. It is a plain function rather than an assertion for the same reason
// sameSet is: an assertion whose failure path is never exercised is an
// assertion nobody knows still works, and this one is the guard the whole
// provider-state bundle rests on. blocktest_test.go drives it directly with a
// deliberately aliasing clone.
//
// Two kinds of sharing are reported, and BOTH are needed:
//
//   - a shared byte backing array, which is the mutation leak itself; and
//   - a shared POINTER, which is how a nested block gets shared wholesale. A
//     clone that writes `cloned.Content = typed.Content` hands back the very
//     same *content.ToolUseBlock values, so every byte-backed field inside them
//     is shared too. Recursing without checking identity first would walk that
//     value against ITSELF and report each byte field independently at best —
//     and report nothing at all when the shared element happens to have no
//     byte-backed field, which is exactly how this guard used to pass a
//     completely aliasing clone.
func sharedReferences(orig, clone reflect.Value, path string) []error {
	switch orig.Kind() {
	case reflect.Pointer, reflect.Interface:
		if orig.IsNil() || clone.IsNil() {
			return nil
		}
		if orig.Kind() == reflect.Pointer && orig.Pointer() == clone.Pointer() {
			return []error{fmt.Errorf("%s: clone shares the original's %s, not a copy of it", path, orig.Type())}
		}
		return sharedReferences(orig.Elem(), clone.Elem(), path)
	case reflect.Struct:
		var shared []error
		for i := range orig.NumField() {
			if !orig.Type().Field(i).IsExported() {
				continue
			}
			shared = append(shared, sharedReferences(orig.Field(i), clone.Field(i), path+"."+orig.Type().Field(i).Name)...)
		}
		return shared
	case reflect.Slice:
		if orig.Len() == 0 || clone.Len() == 0 {
			return nil
		}
		if orig.Type().Elem().Kind() == reflect.Uint8 {
			if orig.Index(0).Addr().Pointer() == clone.Index(0).Addr().Pointer() {
				return []error{fmt.Errorf("%s: clone aliases the original's backing array", path)}
			}
			return nil
		}
		var shared []error
		for i := range min(orig.Len(), clone.Len()) {
			shared = append(shared, sharedReferences(orig.Index(i), clone.Index(i), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return shared
	}
	return nil
}
