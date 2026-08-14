package content_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/core/content/blocktest"
)

// TestCloneBlockCoversEverySealedVariant is the exhaustiveness guard named in
// CloneBlock's doc comment. The fixture list is a bijection with the sealed
// union (blocktest checks that against content's own declarations), so a
// variant added without a clone arm reaches the default and panics here rather
// than in a consumer's production path.
//
// It also pins the two properties a copy has to have at once, which no single
// assertion covers: the clone must hold the same VALUE (DeepEqual, over fields
// blocktest populated by reflection, so a field added to content is compared
// without anyone editing this test) and must share none of the original's
// MEMORY (AssertIndependent, which reports any byte array or pointer still
// shared). Equality alone passes for a plain struct copy that aliases every
// backing array; independence alone passes for a clone that dropped a field.
func TestCloneBlockCoversEverySealedVariant(t *testing.T) {
	t.Parallel()

	for _, want := range blocktest.Blocks(t) {
		t.Run(reflect.TypeOf(want).Elem().Name(), func(t *testing.T) {
			t.Parallel()

			got := content.CloneBlock(want)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("CloneBlock() = %#v, want %#v", got, want)
			}
			blocktest.AssertIndependent(t, want, got)
		})
	}
}

// TestCloneBlocksIsIndependentOfTheOriginal proves the copy is a copy by
// mutating the original afterwards. DeepEqual on its own cannot see aliasing;
// this can, and it covers the nested slice on ToolResultBlock, which is where a
// shallow copy hands back the original's element pointers wholesale.
func TestCloneBlocksIsIndependentOfTheOriginal(t *testing.T) {
	t.Parallel()

	nested := &content.ToolUseBlock{
		ID:                  "call-1",
		Name:                "search",
		Input:               json.RawMessage(`{"q":"a"}`),
		ProviderState:       json.RawMessage(`{"sig":"a"}`),
		ProviderStateFormat: "anthropic",
	}
	original := []content.Block{
		&content.ToolResultBlock{ToolUseID: "call-1", Content: []content.Block{nested}},
		&content.ImageBlock{MediaType: "image/png", Source: content.ImageSource{Data: []byte{1, 2, 3}}},
	}

	cloned := content.CloneBlocks(original)
	snapshot := content.CloneBlocks(original)

	nested.Input[6] = 'z'
	nested.ProviderState[8] = 'z'
	nested.Name = "mutated"
	original[1].(*content.ImageBlock).Source.Data[0] = 9

	if !reflect.DeepEqual(cloned, snapshot) {
		t.Errorf("mutating the original changed the clone:\n got %#v\nwant %#v", cloned, snapshot)
	}
}

// TestCloneBlockPreservesNilVersusEmpty is the deliberate semantic decision in
// CloneBlock, and the one the five previous implementations disagreed on: the
// hook clone preserved an empty non-nil json.RawMessage, the loop runtime
// collapsed it to nil, and both were tested as correct.
//
// A clone is a copy, not a normalization. Routing through NewToolUseBlock or
// NewThinkingBlock would collapse empty to nil and would clear a
// ProviderStateFormat whose ProviderState is empty — repairs that belong at
// construction, where a block is built from untrusted parts, not at a copy,
// where the caller is entitled to get back what it put in.
func TestCloneBlockPreservesNilVersusEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		block content.Block
	}{
		{
			name:  "nil raw messages stay nil",
			block: &content.ToolUseBlock{ID: "a", Name: "b"},
		},
		{
			name: "empty non-nil raw messages stay empty and non-nil",
			block: &content.ToolUseBlock{
				ID: "a", Name: "b",
				Input:         json.RawMessage{},
				ProviderState: json.RawMessage{},
			},
		},
		{
			name: "a provider-state format with no provider state is not cleared",
			block: &content.ThinkingBlock{
				Thinking: "why", ProviderStateFormat: "gemini",
			},
		},
		{
			name: "provider state with no format is not cleared",
			block: &content.ThinkingBlock{
				Thinking: "why", ProviderState: json.RawMessage(`{"a":1}`),
			},
		},
		{
			name:  "an empty non-nil byte field stays empty and non-nil",
			block: &content.AudioBlock{MediaType: "audio/wav", Data: []byte{}},
		},
		{
			name:  "a nil nested content slice stays nil",
			block: &content.ToolResultBlock{ToolUseID: "a"},
		},
		{
			name:  "an empty non-nil nested content slice stays empty and non-nil",
			block: &content.ToolResultBlock{ToolUseID: "a", Content: []content.Block{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := content.CloneBlock(tt.block)
			// DeepEqual distinguishes a nil slice from an empty one, which is
			// exactly the distinction under test.
			if !reflect.DeepEqual(tt.block, got) {
				t.Fatalf("CloneBlock() = %#v, want %#v", got, tt.block)
			}
		})
	}
}

// TestCloneBlockNilCases pins what callers with their own fail-secure policy
// inspect. A nil interface clones to a nil interface; a typed-nil payload
// clones to the same typed nil and keeps a non-nil interface, so the two remain
// distinguishable by the consumers that escalate one and not the other.
func TestCloneBlockNilCases(t *testing.T) {
	t.Parallel()

	if got := content.CloneBlock(nil); got != nil {
		t.Errorf("CloneBlock(nil) = %#v, want nil", got)
	}

	typedNil := (*content.TextBlock)(nil)
	got := content.CloneBlock(typedNil)
	if got == nil {
		t.Fatal("CloneBlock((*TextBlock)(nil)) = nil interface, want a typed nil")
	}
	if block, ok := got.(*content.TextBlock); !ok || block != nil {
		t.Errorf("CloneBlock((*TextBlock)(nil)) = %#v, want (*content.TextBlock)(nil)", got)
	}
}

// TestCloneBlocksPreservesSliceIdentity covers the outer slice's own
// nil-versus-empty state, which every consumer's clone also has to agree on.
func TestCloneBlocksPreservesSliceIdentity(t *testing.T) {
	t.Parallel()

	if got := content.CloneBlocks(nil); got != nil {
		t.Errorf("CloneBlocks(nil) = %#v, want nil", got)
	}
	got := content.CloneBlocks([]content.Block{})
	if got == nil {
		t.Fatal("CloneBlocks([]Block{}) = nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("CloneBlocks([]Block{}) = %#v, want empty", got)
	}
}
