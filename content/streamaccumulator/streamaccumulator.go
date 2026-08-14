// Package streamaccumulator folds streaming content chunks into complete
// content blocks. It is a pure converter shared by the loop and the TUI/CLI live
// display path:
//
//	ThinkingChunk -> ThinkingBlock
//	TextChunk     -> TextBlock
//	ToolUseChunk  -> ToolUseBlock
//	RefusalChunk  -> RefusalBlock
//	ImageChunk    -> ImageBlock
//
// It does NOT send events, validate tool permissions, decide turn failure, or
// know about the loop. Policy stays in the loop; this package only converts.
// It deliberately imports nothing beyond the standard library and
// internal/content (in particular, never internal/agent/loop or its event
// package) so it carries no dependency cycle.
package streamaccumulator

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/looprig/core/content"
)

// Thinking folds streaming ThinkingChunk deltas into complete ThinkingBlocks. It
// is keyed by the provider-supplied Index the same way ToolUses is, and for the
// same two reasons: a response may legitimately contain SEVERAL reasoning blocks
// (Anthropic interleaved thinking opens a fresh thinking / redacted_thinking
// block around every tool call), and the Index is provider/attacker-influenced,
// so a map — never slice indexing — keeps a negative or huge Index from panicking
// or allocating an unbounded slice.
//
// Per-block keying is load-bearing for continuation state, not a nicety. Each
// reasoning block carries its OWN signature or opaque provider state, and the
// replayed sequence must match the sequence the model generated block-for-block.
// Folding N blocks into one builder with a last-write-wins signature would
// silently rebind the last signature to the concatenation of every block's text,
// which the provider rejects and which the non-streaming decoders never produce —
// breaking the invariant that streaming preserves the same continuation state as
// non-streaming.
//
// Producers that emit a single reasoning block leave Index at zero, so every
// delta accumulates into that one block exactly as before. Blocks() emits the
// assembled blocks in ASCENDING Index order (the deterministic response order).
// The zero value is ready to use.
type Thinking struct {
	parts map[int]*thinkingPart
	order []int // Index values in first-seen order; sorted ascending by Blocks()
}

type thinkingPart struct {
	builder             strings.Builder
	signature           string
	signatureFormat     string
	providerState       json.RawMessage
	providerStateFormat string
}

// Add folds one delta into the accumulator, bounds-safe on any Index value.
func (a *Thinking) Add(chunk *content.ThinkingChunk) {
	if a.parts == nil {
		a.parts = make(map[int]*thinkingPart)
	}
	p, ok := a.parts[chunk.Index]
	if !ok {
		p = &thinkingPart{}
		a.parts[chunk.Index] = p
		a.order = append(a.order, chunk.Index)
	}
	p.builder.WriteString(chunk.Thinking)
	// The signature arrives as a terminal delta for its own Index; never overwrite
	// a set signature with a later empty one (last non-empty value wins). The
	// minting dialect's label travels WITH the value and never separately: a
	// signature and a format taken from two different deltas could pair one
	// dialect's bytes with another's label, which is worse than an untagged
	// signature because it would pass the replay check.
	if chunk.Signature != "" {
		p.signature = chunk.Signature
		p.signatureFormat = chunk.SignatureFormat
	}
	if len(chunk.ProviderState) > 0 && chunk.ProviderStateFormat != "" {
		p.providerState = append(p.providerState[:0], chunk.ProviderState...)
		p.providerStateFormat = chunk.ProviderStateFormat
	}
}

// Blocks returns the assembled ThinkingBlocks in ascending Index order, or nil if
// no chunk was received. Gaps in the Index sequence order the blocks but never
// materialize filler blocks: only indexes that actually received a delta appear.
func (a Thinking) Blocks() []content.ThinkingBlock {
	if len(a.order) == 0 {
		return nil
	}
	idx := make([]int, len(a.order))
	copy(idx, a.order)
	sort.Ints(idx)
	out := make([]content.ThinkingBlock, 0, len(idx))
	for _, i := range idx {
		out = append(out, *a.block(i))
	}
	return out
}

// Block returns the first assembled thinking block, or nil when empty.
// Deprecated: use Blocks to preserve multiple provider reasoning blocks.
// Single-block streams retain the exact behavior of the original API.
func (a Thinking) Block() *content.ThinkingBlock {
	blocks := a.Blocks()
	if len(blocks) == 0 {
		return nil
	}
	return &blocks[0]
}

// block materializes the part at an Index that is known to be present.
func (a Thinking) block(index int) *content.ThinkingBlock {
	p := a.parts[index]
	return content.NewSignedThinkingBlock(
		p.builder.String(),
		p.signature,
		p.signatureFormat,
		p.providerState,
		p.providerStateFormat,
	)
}

// Empty reports whether no chunk has been added yet.
func (a Thinking) Empty() bool { return len(a.order) == 0 }

// Text folds streamed TextChunk deltas into a single TextBlock.
// The zero value is ready to use.
type Text struct {
	builder  strings.Builder
	received bool
}

// Add appends one text delta to the accumulator.
func (a *Text) Add(chunk *content.TextChunk) {
	a.received = true
	a.builder.WriteString(chunk.Text)
}

// Block returns the accumulated TextBlock, or nil if no chunk was received.
func (a Text) Block() *content.TextBlock {
	if !a.received {
		return nil
	}
	return &content.TextBlock{Text: a.builder.String()}
}

// Empty reports whether no chunk has been added yet.
func (a Text) Empty() bool { return !a.received }

// Refusal folds streamed RefusalChunk deltas into a single RefusalBlock. It
// mirrors Text because RefusalChunk mirrors TextChunk: refusal deltas carry no
// Index and concatenate losslessly (see content.RefusalChunk for why).
//
// The received flag, not the accumulated string, decides whether a block exists.
// A provider may refuse with no explanation at all, and an empty refusal must
// still materialize a RefusalBlock: dropping it would restore the exact bug this
// type was added to fix — a refusal decoding as a successful empty reply.
// The zero value is ready to use.
type Refusal struct {
	builder  strings.Builder
	received bool
}

// Add appends one refusal delta to the accumulator.
func (a *Refusal) Add(chunk *content.RefusalChunk) {
	a.received = true
	a.builder.WriteString(chunk.Text)
}

// Block returns the accumulated RefusalBlock, or nil if no chunk was received.
func (a Refusal) Block() *content.RefusalBlock {
	if !a.received {
		return nil
	}
	return &content.RefusalBlock{Text: a.builder.String()}
}

// Empty reports whether no chunk has been added yet.
func (a Refusal) Empty() bool { return !a.received }

// Images folds streaming ImageChunk deltas into complete ImageBlocks. Like
// ToolUses it is keyed by the provider-supplied Index with a map rather than
// slice indexing, so a negative or huge Index — the value is
// provider/attacker-influenced — can NEVER panic or allocate an unbounded slice.
//
// Per-image keying is load-bearing for correctness, not ordering: appending one
// image's bytes to another's produces a corrupt file that nothing downstream can
// detect or recover. Data fragments for an Index append in arrival order; URL
// and MediaType take the last non-empty value, since they arrive whole rather
// than fragmented. Blocks() emits the assembled blocks in ASCENDING Index order
// (the deterministic response order). The zero value is ready to use.
type Images struct {
	parts map[int]*imagePart
	order []int // Index values in first-seen order; sorted ascending by Blocks()
}

type imagePart struct {
	mediaType content.MediaType
	url       string
	data      []byte
}

// Add folds one delta into the accumulator, bounds-safe on any Index value. The
// delta's bytes are appended into the part's own buffer, so the caller may reuse
// or mutate its slice after the call without rewriting accumulated bytes.
func (a *Images) Add(chunk *content.ImageChunk) {
	if a.parts == nil {
		a.parts = make(map[int]*imagePart)
	}
	p, ok := a.parts[chunk.Index]
	if !ok {
		p = &imagePart{}
		a.parts[chunk.Index] = p
		a.order = append(a.order, chunk.Index)
	}
	// MediaType/URL arrive whole, typically on the first delta for an Index;
	// never overwrite a set value with a later empty one (last non-empty wins).
	if chunk.MediaType != "" {
		p.mediaType = chunk.MediaType
	}
	if chunk.Source.URL != "" {
		p.url = chunk.Source.URL
		p.data = nil
	}
	if len(chunk.Source.Data) > 0 {
		p.url = ""
		p.data = append(p.data, chunk.Source.Data...)
	}
}

// Blocks returns the assembled ImageBlocks in ascending Index order, or nil if
// no chunk was received. Each block gets its OWN copy of the accumulated bytes,
// so mutating a returned block cannot corrupt the accumulator or a block handed
// to another caller. The bytes are used verbatim; this package never validates
// that they decode as the claimed MediaType.
func (a Images) Blocks() []content.ImageBlock {
	if len(a.order) == 0 {
		return nil
	}
	idx := make([]int, len(a.order))
	copy(idx, a.order)
	sort.Ints(idx)
	out := make([]content.ImageBlock, 0, len(idx))
	for _, i := range idx {
		p := a.parts[i]
		var data []byte
		if len(p.data) > 0 {
			data = append([]byte(nil), p.data...)
		}
		out = append(out, content.ImageBlock{
			MediaType: p.mediaType,
			Source:    content.ImageSource{URL: p.url, Data: data},
		})
	}
	return out
}

// Empty reports whether no chunk has been added yet.
func (a Images) Empty() bool { return len(a.order) == 0 }

// ToolUses folds streaming ToolUseChunk deltas into complete ToolUseBlocks. It is
// keyed by the provider-supplied Index (which is provider/attacker-influenced),
// so it uses a map rather than slice indexing: a negative or huge Index can NEVER
// panic or allocate an unbounded slice. The first delta for an Index typically
// carries ID/Name; later deltas carry InputJSON fragments to concatenate.
// Blocks() emits the assembled blocks in ASCENDING Index order (the deterministic
// response order). The zero value is ready to use.
type ToolUses struct {
	parts map[int]*toolPart
	order []int // Index values in first-seen order; sorted ascending by Blocks()
}

type toolPart struct {
	id                  string
	name                string
	input               strings.Builder
	providerState       json.RawMessage
	providerStateFormat string
}

// Add folds one delta into the accumulator, bounds-safe on any Index value.
func (a *ToolUses) Add(chunk *content.ToolUseChunk) {
	if a.parts == nil {
		a.parts = make(map[int]*toolPart)
	}
	p, ok := a.parts[chunk.Index]
	if !ok {
		p = &toolPart{}
		a.parts[chunk.Index] = p
		a.order = append(a.order, chunk.Index)
	}
	// ID/Name arrive on the first delta for an Index; never overwrite a set value
	// with a later empty fragment (last non-empty value wins).
	if chunk.ID != "" {
		p.id = chunk.ID
	}
	if chunk.Name != "" {
		p.name = chunk.Name
	}
	if len(chunk.ProviderState) > 0 && chunk.ProviderStateFormat != "" {
		p.providerState = append(p.providerState[:0], chunk.ProviderState...)
		p.providerStateFormat = chunk.ProviderStateFormat
	}
	p.input.WriteString(chunk.InputJSON)
}

// Blocks returns the assembled ToolUseBlocks in ascending Index order, or nil if
// no chunk was received. The raw concatenated Input is used verbatim; any
// validation or sanitization happens in the caller.
func (a ToolUses) Blocks() []content.ToolUseBlock {
	if len(a.order) == 0 {
		return nil
	}
	idx := make([]int, len(a.order))
	copy(idx, a.order)
	sort.Ints(idx)
	out := make([]content.ToolUseBlock, 0, len(idx))
	for _, i := range idx {
		p := a.parts[i]
		out = append(out, *content.NewToolUseBlock(
			p.id,
			p.name,
			json.RawMessage(p.input.String()),
			p.providerState,
			p.providerStateFormat,
		))
	}
	return out
}

// Empty reports whether no chunk has been added yet.
func (a ToolUses) Empty() bool { return len(a.order) == 0 }
