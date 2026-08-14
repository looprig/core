package content

import "fmt"

// CloneBlock returns a block that holds the same value as block and shares none
// of its memory.
//
// # Why this lives in content
//
// Copying a block is not a consumer concern. Before this function existed the
// workspace held five independent implementations — the loop runtime's message
// clone, the hook payload clone, the compaction wire adapter, the foreign-loop
// snapshot, and a product translator — written with three different techniques,
// and they had already drifted into disagreeing about the same input. Every one
// of them was a hand-maintained type switch over a union only this package can
// extend, which put the switch and the union in different modules with a
// release boundary between them: content could grow a variant, and the copy in
// another module would keep compiling while silently dropping it. With the
// switch here, a new variant and its copy arm land in the same commit, and this
// package's own test refuses a variant that has no arm.
//
// # How the copy is made
//
// Each arm copies the struct WHOLE and then re-copies the reference-backed
// fields, rather than naming every field in a literal. A field added to a block
// is therefore carried by the struct copy without touching this function, which
// is the drift a literal cannot survive: a literal that forgets a new field
// still compiles, still passes, and loses the field. That is exactly how
// ThinkingBlock.ProviderState and ToolUseBlock.ProviderState went missing from
// three separate copies at once.
//
// The residue that a struct copy alone cannot handle is a new field that is
// itself reference-backed: it would be copied by reference and alias the
// original. That is covered from the other side, by
// content/blocktest.AssertIndependent, which walks a cloned fixture with
// reflection and reports any byte array or pointer the copy still shares. The
// two together are what make this field-drift-proof: the struct copy carries
// unknown fields, and the reflection guard proves the ones that need deep
// copies got them.
//
// # Nil versus empty is preserved exactly
//
// A clone is a copy, not a normalization. CloneBlock reproduces a nil
// json.RawMessage as nil and an empty non-nil one as empty non-nil, and it
// leaves a half-set ProviderState/ProviderStateFormat pair exactly as it found
// it — it does NOT route the copy through NewThinkingBlock or NewToolUseBlock.
//
// Those constructors normalize on purpose: they are the boundary where a block
// is CREATED from untrusted parts, and clearing a format label that labels
// nothing belongs there. A copy is a different boundary. Three reasons this one
// must be faithful:
//
//   - reflect.DeepEqual(block, CloneBlock(block)) holds for every block. That
//     equality is the assertion every consumer's round-trip test wants to make,
//     and DeepEqual distinguishes a nil slice from an empty one. A normalizing
//     clone forces those tests to compare loosely instead, and a loose
//     comparison is precisely how a dropped field hides.
//   - The distinction is observable. A nil json.RawMessage marshals to null; an
//     empty non-nil one is not valid JSON and fails to marshal. A copy that
//     turns the second into the first repairs a broken value in transit, so the
//     defect surfaces somewhere other than where it was introduced.
//   - Callers who want normalization can still have it, by calling the
//     constructor themselves at the point they mean it. Callers who want a copy
//     cannot recover fidelity that a copy already threw away.
//
// # Return values
//
// A nil Block interface clones to a nil Block: nothing copied faithfully is
// still nothing. A typed-nil payload pointer such as (*TextBlock)(nil) clones to
// the same typed nil, because there is no struct to copy; the interface stays
// non-nil, so a caller can tell the two apart. Consumers that treat either case
// as a fault keep their own policy — hook panics, the foreign-loop snapshot
// returns an error, the loop runtime drops the block — by inspecting the result
// rather than by maintaining a switch of their own.
//
// A member of the sealed union with no arm below panics. It cannot be reached
// without editing this package: Block's marker method is unexported, so no type
// outside content can join the union, and TestCloneBlockCoversEverySealedVariant
// fails on a variant added here without an arm. The panic is the report of a
// bug in this file, not a runtime condition any caller can cause or handle.
func CloneBlock(block Block) Block {
	switch typed := block.(type) {
	case nil:
		return nil
	case *TextBlock:
		if typed == nil {
			return (*TextBlock)(nil)
		}
		cloned := *typed
		return &cloned
	case *RefusalBlock:
		if typed == nil {
			return (*RefusalBlock)(nil)
		}
		// Its own arm, never folded into TextBlock. The two structs are
		// identical, so a shared arm would compile and would hand back a
		// *TextBlock — turning "the model declined" into "the model answered"
		// at the one place nothing would notice.
		cloned := *typed
		return &cloned
	case *ImageBlock:
		if typed == nil {
			return (*ImageBlock)(nil)
		}
		cloned := *typed
		cloned.Source.Data = cloneSlice(typed.Source.Data)
		return &cloned
	case *AudioBlock:
		if typed == nil {
			return (*AudioBlock)(nil)
		}
		cloned := *typed
		cloned.Data = cloneSlice(typed.Data)
		return &cloned
	case *DocumentBlock:
		if typed == nil {
			return (*DocumentBlock)(nil)
		}
		cloned := *typed
		cloned.Data = cloneSlice(typed.Data)
		return &cloned
	case *ThinkingBlock:
		if typed == nil {
			return (*ThinkingBlock)(nil)
		}
		cloned := *typed
		cloned.ProviderState = cloneSlice(typed.ProviderState)
		return &cloned
	case *ToolUseBlock:
		if typed == nil {
			return (*ToolUseBlock)(nil)
		}
		cloned := *typed
		cloned.Input = cloneSlice(typed.Input)
		cloned.ProviderState = cloneSlice(typed.ProviderState)
		return &cloned
	case *ToolResultBlock:
		if typed == nil {
			return (*ToolResultBlock)(nil)
		}
		cloned := *typed
		cloned.Content = CloneBlocks(typed.Content)
		return &cloned
	default:
		panic(fmt.Sprintf(
			"content: CloneBlock has no arm for %T; a variant joined the sealed union without one", block))
	}
}

// CloneBlocks returns an independent copy of every block in blocks.
//
// The slice's own nil-versus-empty state is preserved for the same reason the
// blocks' fields are: a nil []Block and an empty one are different values, and
// DeepEqual says so.
func CloneBlocks(blocks []Block) []Block {
	if blocks == nil {
		return nil
	}
	cloned := make([]Block, len(blocks))
	for index, block := range blocks {
		cloned[index] = CloneBlock(block)
	}
	return cloned
}

// bytes constrains cloneSlice to the byte-backed field types content declares,
// so json.RawMessage clones as json.RawMessage rather than decaying to []byte.
type bytes interface{ ~[]byte }

// cloneSlice copies a byte-backed field, preserving nil-versus-empty. append to
// a nil slice is deliberately NOT used: appending nothing yields nil, which
// silently turns an empty non-nil field into a nil one — the exact
// normalization CloneBlock documents that it does not perform, and the
// difference the five previous implementations had already drifted apart on.
func cloneSlice[T bytes](value T) T {
	if value == nil {
		return nil
	}
	cloned := make(T, len(value))
	copy(cloned, value)
	return cloned
}
