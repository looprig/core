// Package content defines the unified content vocabulary shared across all
// internal packages. Block is a sealed interface; the concrete payload type is
// the discriminator. Only this package can add variants (unexported marker).
package content

import "encoding/json"

type BlockType string

const (
	TypeText       BlockType = "text"
	TypeImage      BlockType = "image"
	TypeAudio      BlockType = "audio"
	TypeDocument   BlockType = "document"
	TypeThinking   BlockType = "thinking"
	TypeToolUse    BlockType = "tool_use"
	TypeToolResult BlockType = "tool_result"
	TypeRefusal    BlockType = "refusal"
)

// Block is the sealed interface over all content block payloads. The concrete
// type is the discriminator; there is no Type field and no nil-able payload
// pointers. BlockType is retained only as the wire tag for the JSON codec
// (block_json.go, added in a later task), not as a field on any in-memory value.
type Block interface{ isBlock() }

func (*TextBlock) isBlock()       {}
func (*ImageBlock) isBlock()      {}
func (*AudioBlock) isBlock()      {}
func (*DocumentBlock) isBlock()   {}
func (*ThinkingBlock) isBlock()   {}
func (*ToolUseBlock) isBlock()    {}
func (*ToolResultBlock) isBlock() {}
func (*RefusalBlock) isBlock()    {}

type TextBlock struct {
	Text string
}

// RefusalBlock carries the model's stated reason for declining to answer.
//
// It is deliberately its OWN variant rather than a flavor of TextBlock. Every
// major provider models a refusal as a channel separate from the assistant's
// ordinary output — OpenAI declares `refusal` as a required member of the
// response message, emits a `refusal` streaming delta, and carries a `refusal`
// content part in the Responses API — and the two are not interchangeable: a
// refusal is the model saying it will not produce the requested output, whereas
// text is the output. Collapsing the two costs the caller the only signal that
// distinguishes "declined" from "answered", and the failure is silent in the
// worst direction: a structured-output refusal arrives with no text parts at
// all, so a decoder that has nowhere to put the refusal yields a zero-block
// SUCCESS. The caller then reports an empty answer for a request the model
// actively refused. Because a *RefusalBlock never matches a *TextBlock type
// switch arm, every exhaustive consumer is forced to decide what a refusal means
// instead of inheriting that failure by default.
//
// Text is the refusal message as the provider worded it. An empty Text is a
// meaningful value, not an absent one: a provider may report a refusal with no
// explanation, and the presence of the block — not its contents — is the signal.
type RefusalBlock struct {
	Text string
}

// ImageSource is a sum type for the origin of image data.
// Set exactly one of URL (remote) or Data (inline bytes).
type ImageSource struct {
	URL  string
	Data []byte
}

type ImageBlock struct {
	MediaType MediaType
	Source    ImageSource
}

type AudioBlock struct {
	MediaType MediaType
	Data      []byte
}

// DocumentBlock carries document data. Either Data (binary) or Text (extracted
// text) may be populated depending on how the document was provided.
type DocumentBlock struct {
	MediaType MediaType
	Name      string
	Data      []byte
	Text      string
}

// ThinkingBlock carries model reasoning text.
// Signature is empty during streaming and non-empty only on a complete block.
//
// # The two provider-private channels, and why both are tagged
//
// A reasoning block carries provider-private continuation state in EITHER of
// two shapes, and this type models both because a single dialect uses both:
//
//   - Signature is the dialect's native, cleartext-adjacent seal over the
//     visible reasoning text — Anthropic's `thinking.signature`, Bedrock
//     Converse's `reasoningText.signature`. It accompanies a NON-EMPTY
//     Thinking.
//   - ProviderState is a wholly opaque payload with no visible counterpart —
//     Anthropic's `redacted_thinking.data`, a Gemini thoughtSignature, an
//     OpenAI Responses encrypted_content. Thinking is empty when it is set.
//
// The two therefore COEXIST on one type but are never both populated by the
// same wire block, and neither may stand in for the other. Each carries its
// own format label, because a value with no label is a value whose issuer is
// unknown, and replaying provider-private state to an issuer that did not mint
// it is a guaranteed rejection.
//
// SignatureFormat is an opaque, codec-chosen label naming the dialect that
// MINTED Signature (for example "anthropic" or "bedrock-converse"). It is
// meaningless and unset whenever Signature is empty. It exists because a
// signature is cryptographically verified by its issuer: replaying a
// Bedrock-minted Claude signature to api.anthropic.com draws
// `messages.N.content.0: Invalid signature in thinking block` (HTTP 400), and
// Bedrock and Anthropic are two endpoints for the SAME model family, so the
// blocks are byte-identical in every respect except the one that matters.
// Dropping a foreign signature is not a safe degrade either: an unsigned
// thinking block draws the same 400. A codec that cannot claim a signature
// must therefore FAIL, not forward it and not silently strip it. Read it only
// through SignatureReplayableAs, never through the field.
//
// ProviderStateFormat is the same label for ProviderState (for example
// "gemini" or "openai-responses"). It is meaningless and unset whenever
// ProviderState is empty. This field exists to satisfy the inference gateway's
// first-milestone requirement that opaque replay state is never translated
// across provider dialects (see
// docs/plans/2026-07-31-inference-gateway-design.md, "Thinking" section):
// a codec MUST NEVER replay ProviderState toward a wire field it owns unless
// ProviderStateFormat equals that codec's own label; otherwise it MUST treat
// ProviderState as absent. This is the load-bearing invariant that prevents
// one provider's opaque bytes (e.g. a Gemini thoughtSignature) from being
// forwarded to a different provider (e.g. as an OpenAI Responses
// encrypted_content) as if it were that provider's own native state.
//
// Construct via NewSignedThinkingBlock (or NewThinkingBlock for a block with
// no signature) so the bytes are defensively copied and a label that labels
// nothing is normalized away; a bare struct literal aliases the caller's slice
// and can pair a signature with no format.
type ThinkingBlock struct {
	Thinking            string
	Signature           string
	SignatureFormat     string          `json:"SignatureFormat,omitempty"`
	ProviderState       json.RawMessage `json:"ProviderState,omitempty"`
	ProviderStateFormat string          `json:"ProviderStateFormat,omitempty"`
}

// NewThinkingBlock builds a ThinkingBlock whose signature, if any, carries NO
// dialect label. It exists for the decoders that produce reasoning with no
// native signature at all (Gemini's thought parts, Anthropic's
// redacted_thinking), where the opaque payload travels in providerState.
//
// A non-empty signature passed here is UNTAGGED and every codec will refuse to
// put it on the wire, because an unlabelled signature has no provable issuer.
// Use NewSignedThinkingBlock whenever a signature is present.
func NewThinkingBlock(thinking, signature string, providerState json.RawMessage, providerStateFormat string) *ThinkingBlock {
	return NewSignedThinkingBlock(thinking, signature, "", providerState, providerStateFormat)
}

// NewSignedThinkingBlock builds a ThinkingBlock, defensively copying
// providerState so the caller cannot mutate the retained block through its
// input slice. signatureFormat tags which dialect minted signature and
// providerStateFormat tags which dialect encoded providerState; see the
// ThinkingBlock doc comment for the invariant those labels enforce.
//
// Each pair is normalized independently: a label with nothing to label is
// cleared, so "format, but no value" cannot be constructed here. The reverse —
// a value with no label — is preserved rather than dropped, because discarding
// a signature at construction would lose it silently at the one place nothing
// reports; the codecs refuse it loudly at encode time instead.
func NewSignedThinkingBlock(thinking, signature, signatureFormat string, providerState json.RawMessage, providerStateFormat string) *ThinkingBlock {
	var state json.RawMessage
	if len(providerState) > 0 && providerStateFormat != "" {
		state = append(json.RawMessage(nil), providerState...)
	} else {
		providerStateFormat = ""
	}
	if signature == "" {
		signatureFormat = ""
	}
	return &ThinkingBlock{
		Thinking:            thinking,
		Signature:           signature,
		SignatureFormat:     signatureFormat,
		ProviderState:       state,
		ProviderStateFormat: providerStateFormat,
	}
}

// SignatureReplayableAs returns the reasoning signature b carries and whether
// it is safe to replay toward a wire field owned by the dialect labeled format.
// It is false for a nil receiver, an empty Signature, an empty format, or a
// SignatureFormat that does not exactly match format.
//
// A false result with a NON-EMPTY Signature is not a "treat as absent" degrade,
// which is where this method's contract differs from ReplayableAs: the caller
// holds a signature minted by somebody else, and both available degrades —
// forwarding it, or stripping it and sending an unsigned thinking block — are
// rejected by the issuing API. Such a caller must fail closed with a diagnostic
// naming the foreign format. See the SignatureFormat field doc.
func (b *ThinkingBlock) SignatureReplayableAs(format string) (string, bool) {
	if b == nil || format == "" || b.Signature == "" || b.SignatureFormat != format {
		return "", false
	}
	return b.Signature, true
}

// ReplayableAs reports whether b carries provider-opaque state safe to
// replay toward a wire field owned by the dialect labeled format. False for
// a nil receiver, an empty ProviderState, or a ProviderStateFormat that does
// not exactly match format — the same "treat as absent" degrade every caller
// of this method must already apply on a false result. See the
// ProviderStateFormat field doc for the cross-dialect-replay invariant this
// method exists to let every call site enforce identically.
func (b *ThinkingBlock) ReplayableAs(format string) bool {
	return b != nil && format != "" && len(b.ProviderState) > 0 && b.ProviderStateFormat != "" && b.ProviderStateFormat == format
}

type ToolUseBlock struct {
	ID                  string
	Name                string
	Input               json.RawMessage
	ProviderState       json.RawMessage `json:"ProviderState,omitempty"`
	ProviderStateFormat string          `json:"ProviderStateFormat,omitempty"`
}

// NewToolUseBlock builds a ToolUseBlock, defensively copying both raw-message
// inputs so callers cannot mutate the retained block through their slices.
// ProviderStateFormat scopes providerState to its issuing codec dialect.
func NewToolUseBlock(id, name string, input, providerState json.RawMessage, providerStateFormat string) *ToolUseBlock {
	var inputCopy json.RawMessage
	if input != nil {
		inputCopy = append(json.RawMessage(nil), input...)
	}
	var stateCopy json.RawMessage
	if len(providerState) > 0 && providerStateFormat != "" {
		stateCopy = append(json.RawMessage(nil), providerState...)
	} else {
		providerStateFormat = ""
	}
	return &ToolUseBlock{
		ID:                  id,
		Name:                name,
		Input:               inputCopy,
		ProviderState:       stateCopy,
		ProviderStateFormat: providerStateFormat,
	}
}

// ReplayableAs reports whether b carries provider-opaque state safe to replay
// toward a wire field owned by the dialect labeled format.
func (b *ToolUseBlock) ReplayableAs(format string) bool {
	return b != nil && format != "" && len(b.ProviderState) > 0 && b.ProviderStateFormat != "" && b.ProviderStateFormat == format
}

// ToolResultBlock nests its own []Block, so it implements json.Marshaler /
// json.Unmarshaler in block_json.go (a later task). Do not add a Type field.
type ToolResultBlock struct {
	ToolUseID string
	Content   []Block
	IsError   bool
}
