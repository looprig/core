package content_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/core/content/blocktest"
)

// A reasoning signature is provider-private continuation state that its issuer
// verifies cryptographically. It was the ONE such field in this package that
// travelled without a provenance label, so a signature minted by one endpoint
// could be handed to another that rejects it — and the two endpoints in
// question (api.anthropic.com and Bedrock Converse) serve the same models, so
// the blocks are otherwise indistinguishable.
//
// These tests pin the four properties the label needs in order to be worth
// anything: it scopes replay, it cannot be constructed labelling nothing, it
// survives the durable JSON round trip harness restores from, and a copy keeps
// it.

// TestThinkingBlockSignatureReplayableAs is the whole point of the label. Note
// the asymmetry with ReplayableAs that the doc comment calls out: a false
// result here is not licence to drop the signature, because an unsigned
// thinking block is rejected by the same API that rejects a foreign signature.
func TestThinkingBlockSignatureReplayableAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		block         *content.ThinkingBlock
		format        string
		wantSignature string
		wantOK        bool
	}{
		{
			name:          "own dialect replays the signature verbatim",
			block:         content.NewSignedThinkingBlock("why", "sig", "anthropic", nil, ""),
			format:        "anthropic",
			wantSignature: "sig",
			wantOK:        true,
		},
		{
			name:   "another dialect's signature is refused",
			block:  content.NewSignedThinkingBlock("why", "sig", "bedrock-converse", nil, ""),
			format: "anthropic",
		},
		{
			name:   "an untagged signature has no provable issuer",
			block:  &content.ThinkingBlock{Thinking: "why", Signature: "sig"},
			format: "anthropic",
		},
		{
			name:   "a label with no signature replays nothing",
			block:  &content.ThinkingBlock{Thinking: "why", SignatureFormat: "anthropic"},
			format: "anthropic",
		},
		{
			name:   "an empty format matches nothing, including an empty label",
			block:  &content.ThinkingBlock{Thinking: "why", Signature: "sig"},
			format: "",
		},
		{
			name:   "a nil block replays nothing",
			block:  nil,
			format: "anthropic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			signature, ok := tt.block.SignatureReplayableAs(tt.format)
			if ok != tt.wantOK || signature != tt.wantSignature {
				t.Errorf("SignatureReplayableAs(%q) = (%q, %v), want (%q, %v)",
					tt.format, signature, ok, tt.wantSignature, tt.wantOK)
			}
		})
	}
}

// TestThinkingChunkSignatureReplayableAs pins the delta's identical contract.
// The streaming and non-streaming paths must reconstruct the same continuation
// state, and provenance is part of that state.
func TestThinkingChunkSignatureReplayableAs(t *testing.T) {
	t.Parallel()

	own := &content.ThinkingChunk{Signature: "sig", SignatureFormat: "anthropic"}
	if got, ok := own.SignatureReplayableAs("anthropic"); !ok || got != "sig" {
		t.Errorf("SignatureReplayableAs(own) = (%q, %v), want (%q, true)", got, ok, "sig")
	}
	if _, ok := own.SignatureReplayableAs("bedrock-converse"); ok {
		t.Error("SignatureReplayableAs(foreign) = true, want false")
	}
	untagged := &content.ThinkingChunk{Signature: "sig"}
	if _, ok := untagged.SignatureReplayableAs("anthropic"); ok {
		t.Error("an untagged signature reported itself replayable")
	}
}

// TestNewSignedThinkingBlockNormalizesEachPairIndependently pins the two
// asymmetric normalizations, which are asymmetric on purpose:
//
//   - a LABEL with nothing to label is cleared, because it is meaningless and
//     because leaving it invites a later assignment to pair one dialect's
//     bytes with another's label; while
//   - a SIGNATURE with no label is KEPT, because dropping it at construction
//     would lose it silently at the one place nothing reports. The codecs
//     refuse it loudly at encode time instead.
func TestNewSignedThinkingBlockNormalizesEachPairIndependently(t *testing.T) {
	t.Parallel()

	labelWithNothingToLabel := content.NewSignedThinkingBlock("why", "", "anthropic", nil, "gemini")
	if labelWithNothingToLabel.SignatureFormat != "" {
		t.Errorf("SignatureFormat = %q, want cleared when there is no signature",
			labelWithNothingToLabel.SignatureFormat)
	}
	if labelWithNothingToLabel.ProviderStateFormat != "" {
		t.Errorf("ProviderStateFormat = %q, want cleared when there is no provider state",
			labelWithNothingToLabel.ProviderStateFormat)
	}

	unlabelled := content.NewSignedThinkingBlock("why", "sig", "", nil, "")
	if unlabelled.Signature != "sig" {
		t.Errorf("Signature = %q, want %q: an untagged signature is refused at encode, never dropped here",
			unlabelled.Signature, "sig")
	}

	// The two channels coexist without conflating: a signed visible block and
	// an opaque redacted one are different shapes of the same type, and the
	// constructor keeps each label attached to its own value.
	both := content.NewSignedThinkingBlock("why", "sig", "anthropic", json.RawMessage(`"opaque"`), "gemini")
	if _, ok := both.SignatureReplayableAs("gemini"); ok {
		t.Error("the provider-state label authorized a signature replay; the two channels are conflated")
	}
	if both.ReplayableAs("anthropic") {
		t.Error("the signature label authorized a provider-state replay; the two channels are conflated")
	}
}

// TestNewThinkingBlockLeavesASignatureUntagged documents what the older
// four-argument constructor now means. It is the constructor for reasoning with
// no native signature (Gemini thought parts, Anthropic redacted_thinking); a
// signature passed through it carries no provenance and every codec refuses it.
func TestNewThinkingBlockLeavesASignatureUntagged(t *testing.T) {
	t.Parallel()

	block := content.NewThinkingBlock("why", "sig", nil, "")
	if block.SignatureFormat != "" {
		t.Errorf("SignatureFormat = %q, want empty", block.SignatureFormat)
	}
	if _, ok := block.SignatureReplayableAs("anthropic"); ok {
		t.Error("NewThinkingBlock produced a replayable signature; it cannot know the minting dialect")
	}
}

// TestThinkingBlockSignatureFormatSurvivesTheDurableRoundTrip is the harness
// requirement: sessions are persisted as marshalled blocks and replayed after
// restore, so a label that does not survive marshal/unmarshal turns every
// restored turn into an untagged — and therefore refused — signature.
func TestThinkingBlockSignatureFormatSurvivesTheDurableRoundTrip(t *testing.T) {
	t.Parallel()

	original := content.NewSignedThinkingBlock("why", "sig", "bedrock-converse", nil, "")

	encoded, err := content.MarshalBlock(original)
	if err != nil {
		t.Fatalf("MarshalBlock() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal encoded block: %v", err)
	}
	if got := string(fields["SignatureFormat"]); got != `"bedrock-converse"` {
		t.Errorf("durable JSON SignatureFormat = %s, want %q", got, "bedrock-converse")
	}

	decoded, err := content.UnmarshalBlock(encoded)
	if err != nil {
		t.Fatalf("UnmarshalBlock() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, original) {
		t.Errorf("round trip = %#v, want %#v", decoded, original)
	}
	restored := decoded.(*content.ThinkingBlock)
	if got, ok := restored.SignatureReplayableAs("bedrock-converse"); !ok || got != "sig" {
		t.Errorf("restored SignatureReplayableAs = (%q, %v), want (%q, true)", got, ok, "sig")
	}
	if _, ok := restored.SignatureReplayableAs("anthropic"); ok {
		t.Error("a restored Bedrock signature reported itself replayable as Anthropic's")
	}
}

// TestThinkingBlockSignatureFormatIsOmittedWhenUnset keeps the durable shape
// additive: a block with no signature serializes exactly as it did before this
// field existed, so nothing that reads the stored form has to learn a key that
// carries no information.
func TestThinkingBlockSignatureFormatIsOmittedWhenUnset(t *testing.T) {
	t.Parallel()

	encoded, err := content.MarshalBlock(&content.ThinkingBlock{Thinking: "why"})
	if err != nil {
		t.Fatalf("MarshalBlock() error = %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("unmarshal encoded block: %v", err)
	}
	if _, present := fields["SignatureFormat"]; present {
		t.Errorf("encoded block = %s, want no SignatureFormat key when there is no signature", encoded)
	}
}

// TestCloneBlockKeepsTheSignatureFormat is belt to blocktest's braces.
// CloneBlock copies the struct WHOLE precisely so a new field is carried
// without editing it, and the reflection-driven fixture below proves that for
// every field at once; this names the one field this change adds, so a
// regression reports itself by name.
func TestCloneBlockKeepsTheSignatureFormat(t *testing.T) {
	t.Parallel()

	original := content.NewSignedThinkingBlock("why", "sig", "anthropic", json.RawMessage(`"opaque"`), "gemini")
	cloned, ok := content.CloneBlock(original).(*content.ThinkingBlock)
	if !ok {
		t.Fatalf("CloneBlock() returned %T, want *content.ThinkingBlock", content.CloneBlock(original))
	}
	if cloned.SignatureFormat != original.SignatureFormat {
		t.Errorf("cloned SignatureFormat = %q, want %q", cloned.SignatureFormat, original.SignatureFormat)
	}
	if got, replayable := cloned.SignatureReplayableAs("anthropic"); !replayable || got != "sig" {
		t.Errorf("clone lost the signature's provenance: SignatureReplayableAs = (%q, %v)", got, replayable)
	}
}

// TestBlocktestPopulatesTheSignatureFormat verifies the claim CloneBlock's doc
// comment makes on blocktest's behalf, rather than assuming it. blocktest fills
// every exported field by reflection, so it is what makes a NEW field
// automatically covered by every consumer's round-trip test in the workspace —
// but only if it actually reaches this one. A field it leaves at zero is a
// field every one of those assertions passes on vacuously.
func TestBlocktestPopulatesTheSignatureFormat(t *testing.T) {
	t.Parallel()

	fixture := &content.ThinkingBlock{}
	blocktest.Populate(t, fixture)
	if fixture.SignatureFormat == "" {
		t.Error("blocktest left ThinkingBlock.SignatureFormat at its zero value; " +
			"every consumer's field-completeness assertion would pass on a copy that dropped it")
	}
	if fixture.SignatureFormat == fixture.Signature {
		t.Errorf("blocktest gave Signature and SignatureFormat the same value %q; "+
			"a copy that assigns one to the other would go unnoticed", fixture.Signature)
	}

	chunk := &content.ThinkingChunk{}
	blocktest.Populate(t, chunk)
	if chunk.SignatureFormat == "" {
		t.Error("blocktest left ThinkingChunk.SignatureFormat at its zero value")
	}
}
