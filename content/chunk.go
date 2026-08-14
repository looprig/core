package content

import "encoding/json"

// Chunk is the sealed interface over streaming content deltas. Separate from
// Block because complete blocks have fields that may arrive as terminal deltas
// (for example, a reasoning signature). Chunks are never serialized, so there
// is no codec and no ChunkType wire tag.
type Chunk interface{ isChunk() }

func (*TextChunk) isChunk()     {}
func (*ThinkingChunk) isChunk() {}
func (*ToolUseChunk) isChunk()  {}
func (*RefusalChunk) isChunk()  {}
func (*ImageChunk) isChunk()    {}

type TextChunk struct{ Text string }

// RefusalChunk is a streaming delta of a refusal (OpenAI's `refusal` delta on
// the chat-completions stream and its `response.refusal.delta` event on the
// Responses stream). Deltas accumulate into a single RefusalBlock; see that
// type for why a refusal is not modeled as text.
//
// It carries NO Index, and the omission is deliberate. Index exists on
// ThinkingChunk and ToolUseChunk because folding those blocks together destroys
// state that cannot be reconstructed — a per-block reasoning signature, or the
// boundary between two tool calls' argument JSON — and the damage is silent:
// the concatenated result is still well-formed but wrong. A refusal carries no
// signature, no opaque provider state, and no identity that is ever replayed to
// a provider; it is terminal output whose only content is prose. Concatenating
// refusal deltas is therefore lossless in exactly the way concatenating
// TextChunk deltas is, and TextChunk is the precedent this type follows. If a
// provider ever attaches per-refusal identity that must survive a round trip,
// adding an Index then is an additive change; adding one now would be an
// unused field that invites codecs to invent index semantics no provider
// supplies.
type RefusalChunk struct{ Text string }

// ImageChunk is a streaming delta of image output — the streaming counterpart of
// ImageBlock, which it mirrors field for field so a codec maps the two the same
// way. The accumulator folds deltas into ImageBlocks.
//
// Index identifies WHICH image of the response this delta belongs to, and it is
// load-bearing in a way TextChunk's absent index is not. Text and refusal deltas
// concatenate harmlessly, but image bytes do not: splicing the tail of one image
// onto another yields a corrupt file that no decoder can recover and that no
// validation in this package can detect. A single response may legitimately
// carry several images, so per-image identity has to survive the stream or the
// stream cannot be represented at all. Producers emitting one image may leave
// Index at its zero value; every delta then accumulates into that one image.
//
// Source is a DELTA, not a complete source, and its two arms accumulate
// differently because they arrive differently. Source.Data holds RAW (already
// base64-decoded) bytes that append, in arrival order, to the bytes previously
// seen for this Index — decoding must happen in the codec, since base64
// fragments only concatenate correctly on 4-character boundaries. Source.URL, by
// contrast, always arrives whole; it is never fragmented, so a later non-empty
// URL replaces an earlier one rather than extending it. MediaType typically
// arrives once on the first delta for an Index and likewise takes the last
// non-empty value.
//
// A provider that streams successive complete PREVIEWS of one image rather than
// byte fragments (OpenAI's `partial_image_b64`, where each event is a whole
// standalone image at increasing fidelity) MUST NOT map those previews onto a
// single Index. Doing so would concatenate several complete images into one
// corrupt blob. Such a codec either drops the previews and emits only the final
// image, or gives each preview its own Index so each materializes as its own
// ImageBlock.
type ImageChunk struct {
	Index     int         // image's position in the response
	MediaType MediaType   // typically set on the first delta for this Index
	Source    ImageSource // Data fragments append; URL arrives whole
}

// ThinkingChunk carries a reasoning-text, reasoning-signature, or opaque
// provider-state delta. Signature/SignatureFormat and
// ProviderState/ProviderStateFormat have the same replay-scoping semantics as
// ThinkingBlock: codecs must only replay state whose format matches their own
// dialect, and a signature they cannot claim is a hard error rather than a
// field to drop. The stream accumulator defensively copies ProviderState before
// retaining it, and carries both labels onto the block it folds, so the
// streaming path reconstructs exactly the continuation state the non-streaming
// decoder does — including the provenance of the signature.
//
// Index identifies WHICH reasoning block of the response this delta belongs to,
// exactly as ToolUseChunk.Index does for tool calls. A response may contain more
// than one reasoning block (Anthropic interleaved thinking emits a fresh
// thinking / redacted_thinking block around every tool call), and each block
// carries its OWN signature: the replayed sequence of thinking blocks must match
// the sequence the model generated, signature-for-block. Folding several blocks
// into one would therefore destroy continuation state that the non-streaming
// decoders preserve. Producers that emit a single reasoning block may leave
// Index at its zero value; every delta then accumulates into that one block.
type ThinkingChunk struct {
	Index               int // reasoning block's position in the response
	Thinking            string
	Signature           string
	SignatureFormat     string
	ProviderState       json.RawMessage
	ProviderStateFormat string
}

// SignatureReplayableAs is ThinkingBlock.SignatureReplayableAs for a delta, and
// carries the identical contract: a false result with a non-empty Signature
// means the chunk holds another dialect's signature, which the caller must
// refuse rather than forward or strip.
func (c *ThinkingChunk) SignatureReplayableAs(format string) (string, bool) {
	if c == nil || format == "" || c.Signature == "" || c.SignatureFormat != format {
		return "", false
	}
	return c.Signature, true
}

// ToolUseChunk is a streaming delta of a tool call. Providers emit these as they
// parse function-call deltas; the runner accumulates by Index into a ToolUseBlock.
type ToolUseChunk struct {
	Index               int    // tool call's position in the response
	ID                  string // tool_use id (may arrive only on the first delta for this Index)
	Name                string // tool name (likewise)
	InputJSON           string // partial JSON delta of the arguments
	ProviderState       json.RawMessage
	ProviderStateFormat string
}
