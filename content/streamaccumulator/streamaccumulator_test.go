package streamaccumulator_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/looprig/core/content"
	"github.com/looprig/core/content/streamaccumulator"
)

func TestToolUses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.ToolUseChunk
		want   []content.ToolUseBlock
	}{
		{
			name:   "empty: no chunks yields nil",
			chunks: nil,
			want:   nil,
		},
		{
			name: "single index, multi-fragment InputJSON concatenates",
			chunks: []*content.ToolUseChunk{
				{Index: 0, ID: "call_1", Name: "search", InputJSON: `{"q":`},
				{Index: 0, InputJSON: `"hi"}`},
			},
			want: []content.ToolUseBlock{
				{ID: "call_1", Name: "search", Input: []byte(`{"q":"hi"}`)},
			},
		},
		{
			name: "multi-index returns ascending order regardless of arrival order",
			chunks: []*content.ToolUseChunk{
				{Index: 2, ID: "c2", Name: "two", InputJSON: `{}`},
				{Index: 0, ID: "c0", Name: "zero", InputJSON: `{}`},
				{Index: 1, ID: "c1", Name: "one", InputJSON: `{}`},
			},
			want: []content.ToolUseBlock{
				{ID: "c0", Name: "zero", Input: []byte(`{}`)},
				{ID: "c1", Name: "one", Input: []byte(`{}`)},
				{ID: "c2", Name: "two", Input: []byte(`{}`)},
			},
		},
		{
			name: "negative and huge indexes do not panic and sort ascending",
			chunks: []*content.ToolUseChunk{
				{Index: 1 << 30, ID: "big", Name: "big", InputJSON: `{}`},
				{Index: -5, ID: "neg", Name: "neg", InputJSON: `{}`},
				{Index: 0, ID: "mid", Name: "mid", InputJSON: `{}`},
			},
			want: []content.ToolUseBlock{
				{ID: "neg", Name: "neg", Input: []byte(`{}`)},
				{ID: "mid", Name: "mid", Input: []byte(`{}`)},
				{ID: "big", Name: "big", Input: []byte(`{}`)},
			},
		},
		{
			name: "ID and Name arriving on a later delta are captured",
			chunks: []*content.ToolUseChunk{
				{Index: 0, InputJSON: `{"a":`},
				{Index: 0, ID: "late_id", Name: "late_name", InputJSON: `1}`},
			},
			want: []content.ToolUseBlock{
				{ID: "late_id", Name: "late_name", Input: []byte(`{"a":1}`)},
			},
		},
		{
			name: "last non-empty ID/Name wins; empty fragment never clears a set value",
			chunks: []*content.ToolUseChunk{
				{Index: 0, ID: "first", Name: "first", InputJSON: `{`},
				{Index: 0, ID: "", Name: "", InputJSON: `"k":1`},
				{Index: 0, ID: "second", Name: "second", InputJSON: `}`},
			},
			want: []content.ToolUseBlock{
				{ID: "second", Name: "second", Input: []byte(`{"k":1}`)},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.ToolUses
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Blocks()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Blocks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestToolUsesPreservesProviderStateWithoutAliasing(t *testing.T) {
	t.Parallel()

	firstState := json.RawMessage(`{"thoughtSignature":"first"}`)
	finalState := json.RawMessage(`{"thoughtSignature":"final"}`)
	var acc streamaccumulator.ToolUses
	acc.Add(&content.ToolUseChunk{
		Index:               0,
		ID:                  "call_1",
		Name:                "search",
		InputJSON:           `{"q":`,
		ProviderState:       firstState,
		ProviderStateFormat: "gemini",
	})
	acc.Add(&content.ToolUseChunk{
		Index:               0,
		InputJSON:           `"go"}`,
		ProviderState:       finalState,
		ProviderStateFormat: "gemini",
	})

	for i := range finalState {
		finalState[i] = 'x'
	}
	got := acc.Blocks()
	if len(got) != 1 {
		t.Fatalf("len(Blocks()) = %d, want 1", len(got))
	}
	if got[0].ID != "call_1" || got[0].Name != "search" || string(got[0].Input) != `{"q":"go"}` {
		t.Fatalf("Blocks()[0] identity/input = (%q, %q, %q), want (%q, %q, %q)", got[0].ID, got[0].Name, got[0].Input, "call_1", "search", `{"q":"go"}`)
	}
	wantState := `{"thoughtSignature":"final"}`
	if string(got[0].ProviderState) != wantState || got[0].ProviderStateFormat != "gemini" {
		t.Fatalf("Blocks()[0] provider state = (%q, %q), want (%q, %q)", got[0].ProviderState, got[0].ProviderStateFormat, wantState, "gemini")
	}

	got[0].ProviderState[0] = 'x'
	again := acc.Blocks()
	if string(again[0].ProviderState) != wantState {
		t.Fatalf("second Blocks()[0].ProviderState = %q, want independent copy %q", again[0].ProviderState, wantState)
	}
	if !again[0].ReplayableAs("gemini") || again[0].ReplayableAs("openai-responses") {
		t.Fatalf("replay scope = matching:%v foreign:%v, want true/false", again[0].ReplayableAs("gemini"), again[0].ReplayableAs("openai-responses"))
	}
}

func TestToolUsesIgnoresUnscopedProviderStateAfterScopedState(t *testing.T) {
	t.Parallel()

	var acc streamaccumulator.ToolUses
	acc.Add(&content.ToolUseChunk{
		Index:               0,
		ProviderState:       json.RawMessage(`{"thoughtSignature":"scoped"}`),
		ProviderStateFormat: "gemini",
	})
	acc.Add(&content.ToolUseChunk{
		Index:         0,
		ProviderState: json.RawMessage(`{"thoughtSignature":"unscoped"}`),
	})

	got := acc.Blocks()[0]
	if string(got.ProviderState) != `{"thoughtSignature":"scoped"}` || got.ProviderStateFormat != "gemini" {
		t.Fatalf("provider state pair = (%q, %q), want retained scoped pair", got.ProviderState, got.ProviderStateFormat)
	}
}

func TestToolUsesEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunks    []*content.ToolUseChunk
		wantEmpty bool
	}{
		{name: "empty before any add", chunks: nil, wantEmpty: true},
		{
			name:      "not empty after one add",
			chunks:    []*content.ToolUseChunk{{Index: 0, ID: "x", Name: "x"}},
			wantEmpty: false,
		},
		{
			name:      "not empty after add at negative index",
			chunks:    []*content.ToolUseChunk{{Index: -1, ID: "x", Name: "x"}},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.ToolUses
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			if got := acc.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.TextChunk
		want   *content.TextBlock
	}{
		{
			name:   "empty yields nil block",
			chunks: nil,
			want:   nil,
		},
		{
			name:   "single chunk",
			chunks: []*content.TextChunk{{Text: "hello"}},
			want:   &content.TextBlock{Text: "hello"},
		},
		{
			name: "multiple chunks fold into one block",
			chunks: []*content.TextChunk{
				{Text: "hel"},
				{Text: "lo "},
				{Text: "world"},
			},
			want: &content.TextBlock{Text: "hello world"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Text
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Block()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Block() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTextEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunks    []*content.TextChunk
		wantEmpty bool
	}{
		{name: "empty before any add", chunks: nil, wantEmpty: true},
		{name: "not empty after add", chunks: []*content.TextChunk{{Text: "x"}}, wantEmpty: false},
		{
			name:      "not empty after empty-string add",
			chunks:    []*content.TextChunk{{Text: ""}},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Text
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			if got := acc.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

// TestSingularBlockAccessorsExistOnlyOnSingleBlockAccumulators pins which
// accumulators may expose a singular Block().
//
// Text and Refusal are single-block accumulators: their chunks carry no Index
// and concatenate losslessly, so Block() is the whole result and their only
// accessor. Thinking is multi-block — a response legitimately carries several
// reasoning blocks, each with its OWN signature or opaque provider state — so a
// singular accessor can only return one of them and silently drop blocks 2..N,
// which is the defect Blocks() was added to fix. Thinking retains its deprecated
// Block method only for source compatibility with the prior minor release;
// multi-block consumers must use Blocks.
func TestSingularBlockAccessorsExistOnlyOnSingleBlockAccumulators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		acc       any
		wantBlock bool
	}{
		{name: "Text keeps Block", acc: streamaccumulator.Text{}, wantBlock: true},
		{name: "Refusal keeps Block", acc: streamaccumulator.Refusal{}, wantBlock: true},
		{name: "Thinking keeps deprecated Block", acc: streamaccumulator.Thinking{}, wantBlock: true},
		{name: "ToolUses has no Block", acc: streamaccumulator.ToolUses{}, wantBlock: false},
		{name: "Images has no Block", acc: streamaccumulator.Images{}, wantBlock: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			typ := reflect.TypeOf(tt.acc)
			_, byValue := typ.MethodByName("Block")
			_, byPointer := reflect.PointerTo(typ).MethodByName("Block")
			if got := byValue || byPointer; got != tt.wantBlock {
				t.Errorf("%s has Block() = %v, want %v", typ, got, tt.wantBlock)
			}
		})
	}
}

func TestRefusal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.RefusalChunk
		want   *content.RefusalBlock
	}{
		{
			name:   "empty yields nil block",
			chunks: nil,
			want:   nil,
		},
		{
			name:   "single chunk",
			chunks: []*content.RefusalChunk{{Text: "I'm sorry"}},
			want:   &content.RefusalBlock{Text: "I'm sorry"},
		},
		{
			name: "multiple chunks fold into one block",
			chunks: []*content.RefusalChunk{
				{Text: "I'm "},
				{Text: "sorry, I "},
				{Text: "can't help."},
			},
			want: &content.RefusalBlock{Text: "I'm sorry, I can't help."},
		},
		{
			// A provider that streams a refusal with no text at all still
			// refused; the accumulator must materialize the block so the caller
			// never mistakes a refusal for an ordinary empty reply.
			name:   "empty-string delta still materializes a refusal block",
			chunks: []*content.RefusalChunk{{Text: ""}},
			want:   &content.RefusalBlock{Text: ""},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Refusal
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Block()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Block() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestRefusalEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunks    []*content.RefusalChunk
		wantEmpty bool
	}{
		{name: "empty before any add", chunks: nil, wantEmpty: true},
		{name: "not empty after add", chunks: []*content.RefusalChunk{{Text: "x"}}, wantEmpty: false},
		{
			name:      "not empty after empty-string add",
			chunks:    []*content.RefusalChunk{{Text: ""}},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Refusal
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			if got := acc.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.ImageChunk
		want   []content.ImageBlock
	}{
		{
			name:   "empty: no chunks yields nil",
			chunks: nil,
			want:   nil,
		},
		{
			name: "single index, multi-fragment data concatenates in arrival order",
			chunks: []*content.ImageChunk{
				{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0x89, 0x50}}},
				{Index: 0, Source: content.ImageSource{Data: []byte{0x4E, 0x47}}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0x89, 0x50, 0x4E, 0x47}}},
			},
		},
		{
			name: "URL-only image needs no data",
			chunks: []*content.ImageChunk{
				{Index: 0, MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{URL: "https://example.com/a.jpg"}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{URL: "https://example.com/a.jpg"}},
			},
		},
		{
			name: "two images never share bytes",
			chunks: []*content.ImageChunk{
				{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0xAA}}},
				{Index: 1, MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{Data: []byte{0xBB}}},
				{Index: 0, Source: content.ImageSource{Data: []byte{0xCC}}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0xAA, 0xCC}}},
				{MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{Data: []byte{0xBB}}},
			},
		},
		{
			name: "multi-index returns ascending order regardless of arrival order",
			chunks: []*content.ImageChunk{
				{Index: 2, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{2}}},
				{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0}}},
				{Index: 1, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{1}}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0}}},
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{1}}},
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{2}}},
			},
		},
		{
			name: "negative and huge indexes do not panic and sort ascending",
			chunks: []*content.ImageChunk{
				{Index: 1 << 30, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("big")}},
				{Index: -5, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("neg")}},
				{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("mid")}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("neg")}},
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("mid")}},
				{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte("big")}},
			},
		},
		{
			name: "last non-empty MediaType/URL wins; an empty fragment never clears a set value",
			chunks: []*content.ImageChunk{
				{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{URL: "https://example.com/first.png"}},
				{Index: 0},
				{Index: 0, MediaType: content.MediaTypeImageWebP, Source: content.ImageSource{URL: "https://example.com/second.webp"}},
			},
			want: []content.ImageBlock{
				{MediaType: content.MediaTypeImageWebP, Source: content.ImageSource{URL: "https://example.com/second.webp"}},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Images
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Blocks()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Blocks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestImagesDoesNotAliasChunkOrResultData locks the defensive-copy contract on
// both sides of the accumulator: mutating a caller's delta slice after Add must
// not rewrite accumulated bytes, and mutating one returned block must not
// corrupt a later Blocks() call.
func TestImagesDoesNotAliasChunkOrResultData(t *testing.T) {
	t.Parallel()

	fragment := []byte{0x89, 0x50}
	var acc streamaccumulator.Images
	acc.Add(&content.ImageChunk{Index: 0, MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: fragment}})
	acc.Add(&content.ImageChunk{Index: 0, Source: content.ImageSource{Data: []byte{0x4E, 0x47}}})

	for i := range fragment {
		fragment[i] = 0xFF
	}

	want := []byte{0x89, 0x50, 0x4E, 0x47}
	got := acc.Blocks()
	if len(got) != 1 {
		t.Fatalf("len(Blocks()) = %d, want 1", len(got))
	}
	if !reflect.DeepEqual(got[0].Source.Data, want) {
		t.Fatalf("Blocks()[0].Source.Data = %v, want %v", got[0].Source.Data, want)
	}

	got[0].Source.Data[0] = 0x00
	again := acc.Blocks()
	if !reflect.DeepEqual(again[0].Source.Data, want) {
		t.Fatalf("second Blocks()[0].Source.Data = %v, want independent copy %v", again[0].Source.Data, want)
	}
}

func TestImagesEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunks    []*content.ImageChunk
		wantEmpty bool
	}{
		{name: "empty before any add", chunks: nil, wantEmpty: true},
		{
			name:      "not empty after one add",
			chunks:    []*content.ImageChunk{{Index: 0, MediaType: content.MediaTypeImagePNG}},
			wantEmpty: false,
		},
		{
			name:      "not empty after add at negative index",
			chunks:    []*content.ImageChunk{{Index: -1, MediaType: content.MediaTypeImagePNG}},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Images
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			if got := acc.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}

func TestThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.ThinkingChunk
		want   []content.ThinkingBlock
	}{
		{
			name:   "empty yields no blocks",
			chunks: nil,
			want:   nil,
		},
		{
			name:   "single chunk, signature stays empty",
			chunks: []*content.ThinkingChunk{{Thinking: "reasoning"}},
			want:   []content.ThinkingBlock{{Thinking: "reasoning", Signature: ""}},
		},
		{
			name: "multiple chunks fold into one block with empty signature",
			chunks: []*content.ThinkingChunk{
				{Thinking: "step "},
				{Thinking: "one "},
				{Thinking: "two"},
			},
			want: []content.ThinkingBlock{{Thinking: "step one two", Signature: ""}},
		},
		{
			name: "signature-only delta is retained",
			chunks: []*content.ThinkingChunk{
				{Thinking: "reasoning"},
				{Signature: "sig"},
			},
			want: []content.ThinkingBlock{{Thinking: "reasoning", Signature: "sig"}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Thinking
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Blocks()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Blocks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestThinkingBlocks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		chunks []*content.ThinkingChunk
		want   []content.ThinkingBlock
	}{
		{
			name:   "empty: no chunks yields nil",
			chunks: nil,
			want:   nil,
		},
		{
			name: "unset Index: every delta folds into the single block at index 0",
			chunks: []*content.ThinkingChunk{
				{Thinking: "step "},
				{Thinking: "one"},
				{Signature: "sig"},
			},
			want: []content.ThinkingBlock{{Thinking: "step one", Signature: "sig"}},
		},
		{
			name: "anthropic interleaved thinking: each block keeps its own signature",
			chunks: []*content.ThinkingChunk{
				{Index: 0, Thinking: "first"},
				{Index: 0, Signature: "SIG-ONE"},
				{
					Index:               1,
					ProviderState:       json.RawMessage(`{"data":"REDACTED"}`),
					ProviderStateFormat: "anthropic",
				},
				{Index: 2, Thinking: "second"},
				{Index: 2, Signature: "SIG-TWO"},
			},
			want: []content.ThinkingBlock{
				{Thinking: "first", Signature: "SIG-ONE"},
				{
					ProviderState:       json.RawMessage(`{"data":"REDACTED"}`),
					ProviderStateFormat: "anthropic",
				},
				{Thinking: "second", Signature: "SIG-TWO"},
			},
		},
		{
			name: "multi-index returns ascending order regardless of arrival order",
			chunks: []*content.ThinkingChunk{
				{Index: 2, Thinking: "two", Signature: "s2"},
				{Index: 0, Thinking: "zero", Signature: "s0"},
				{Index: 1, Thinking: "one", Signature: "s1"},
			},
			want: []content.ThinkingBlock{
				{Thinking: "zero", Signature: "s0"},
				{Thinking: "one", Signature: "s1"},
				{Thinking: "two", Signature: "s2"},
			},
		},
		{
			name: "index gaps are preserved as ordering only, never as empty filler blocks",
			chunks: []*content.ThinkingChunk{
				{Index: 7, Thinking: "late", Signature: "s7"},
				{Index: 3, Thinking: "early", Signature: "s3"},
			},
			want: []content.ThinkingBlock{
				{Thinking: "early", Signature: "s3"},
				{Thinking: "late", Signature: "s7"},
			},
		},
		{
			name: "negative and huge indexes do not panic and sort ascending",
			chunks: []*content.ThinkingChunk{
				{Index: 1 << 30, Thinking: "big"},
				{Index: -5, Thinking: "neg"},
				{Index: 0, Thinking: "mid"},
			},
			want: []content.ThinkingBlock{
				{Thinking: "neg"},
				{Thinking: "mid"},
				{Thinking: "big"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Thinking
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			got := acc.Blocks()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Blocks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestThinkingDeprecatedBlockReturnsFirstBlock(t *testing.T) {
	var acc streamaccumulator.Thinking
	acc.Add(&content.ThinkingChunk{Index: 1, Thinking: "second"})
	acc.Add(&content.ThinkingChunk{Index: 0, Thinking: "first"})
	got := acc.Block()
	if got == nil || got.Thinking != "first" {
		t.Fatalf("Block() = %#v, want first indexed block", got)
	}
}

func TestImagesSourceSwitchKeepsExactlyOneSourceArm(t *testing.T) {
	var acc streamaccumulator.Images
	acc.Add(&content.ImageChunk{Index: 0, Source: content.ImageSource{URL: "https://example.test/image.png"}})
	acc.Add(&content.ImageChunk{Index: 0, Source: content.ImageSource{Data: []byte("inline")}})
	blocks := acc.Blocks()
	if len(blocks) != 1 || blocks[0].Source.URL != "" || string(blocks[0].Source.Data) != "inline" {
		t.Fatalf("URL then data = %#v, want data-only source", blocks)
	}

	acc.Add(&content.ImageChunk{Index: 0, Source: content.ImageSource{URL: "https://example.test/final.png"}})
	blocks = acc.Blocks()
	if len(blocks) != 1 || blocks[0].Source.URL == "" || len(blocks[0].Source.Data) != 0 {
		t.Fatalf("data then URL = %#v, want URL-only source", blocks)
	}
}

// TestThinkingKeepsEveryIndexedBlock is what replaced the former
// TestThinkingBlockReturnsLowestIndexBlock: that test asserted the singular
// Block()'s lowest-index behaviour, which silently discarded reasoning blocks
// 2..N together with their signatures. Blocks() must surface both blocks, each
// still bound to its own signature.
func TestThinkingKeepsEveryIndexedBlock(t *testing.T) {
	t.Parallel()

	var acc streamaccumulator.Thinking
	acc.Add(&content.ThinkingChunk{Index: 2, Thinking: "second", Signature: "SIG-TWO"})
	acc.Add(&content.ThinkingChunk{Index: 0, Thinking: "first", Signature: "SIG-ONE"})

	got := acc.Blocks()
	want := []content.ThinkingBlock{
		{Thinking: "first", Signature: "SIG-ONE"},
		{Thinking: "second", Signature: "SIG-TWO"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Blocks() = %#v, want %#v", got, want)
	}
}

func TestThinkingPreservesProviderStateWithoutAliasing(t *testing.T) {
	t.Parallel()

	firstState := json.RawMessage(`{"encrypted_content":"first"}`)
	finalState := json.RawMessage(`{"encrypted_content":"final"}`)
	var acc streamaccumulator.Thinking
	acc.Add(&content.ThinkingChunk{
		Thinking:            "reasoning",
		ProviderState:       firstState,
		ProviderStateFormat: "openai-responses",
	})
	acc.Add(&content.ThinkingChunk{
		Signature:           "sig",
		ProviderState:       finalState,
		ProviderStateFormat: "openai-responses",
	})

	// Mutating the source after Add must not alter the accumulated state.
	for i := range finalState {
		finalState[i] = 'x'
	}
	blocks := acc.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("Blocks() = %#v, want exactly one block", blocks)
	}
	got := blocks[0]
	wantState := `{"encrypted_content":"final"}`
	if got.Thinking != "reasoning" || got.Signature != "sig" {
		t.Fatalf("Blocks()[0] text/signature = (%q, %q), want (%q, %q)", got.Thinking, got.Signature, "reasoning", "sig")
	}
	if string(got.ProviderState) != wantState {
		t.Fatalf("Blocks()[0].ProviderState = %q, want %q", got.ProviderState, wantState)
	}
	if got.ProviderStateFormat != "openai-responses" {
		t.Fatalf("Blocks()[0].ProviderStateFormat = %q, want %q", got.ProviderStateFormat, "openai-responses")
	}

	// Mutating one returned block must not alter a later Blocks result.
	got.ProviderState[0] = 'x'
	again := acc.Blocks()[0]
	if string(again.ProviderState) != wantState {
		t.Fatalf("second Blocks()[0].ProviderState = %q, want independent copy %q", again.ProviderState, wantState)
	}
	if !again.ReplayableAs("openai-responses") || again.ReplayableAs("gemini") {
		t.Fatalf("replay scope = matching:%v foreign:%v, want true/false", again.ReplayableAs("openai-responses"), again.ReplayableAs("gemini"))
	}
}

func TestThinkingIgnoresUnscopedProviderStateAfterScopedState(t *testing.T) {
	t.Parallel()

	var acc streamaccumulator.Thinking
	acc.Add(&content.ThinkingChunk{
		ProviderState:       json.RawMessage(`{"encrypted_content":"scoped"}`),
		ProviderStateFormat: "openai-responses",
	})
	acc.Add(&content.ThinkingChunk{
		ProviderState: json.RawMessage(`{"encrypted_content":"unscoped"}`),
	})

	blocks := acc.Blocks()
	if len(blocks) != 1 {
		t.Fatalf("Blocks() = %#v, want exactly one block", blocks)
	}
	got := blocks[0]
	if string(got.ProviderState) != `{"encrypted_content":"scoped"}` || got.ProviderStateFormat != "openai-responses" {
		t.Fatalf("provider state pair = (%q, %q), want retained scoped pair", got.ProviderState, got.ProviderStateFormat)
	}
}

func TestThinkingEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunks    []*content.ThinkingChunk
		wantEmpty bool
	}{
		{name: "empty before any add", chunks: nil, wantEmpty: true},
		{name: "not empty after add", chunks: []*content.ThinkingChunk{{Thinking: "x"}}, wantEmpty: false},
		{
			name:      "not empty after empty-string add",
			chunks:    []*content.ThinkingChunk{{Thinking: ""}},
			wantEmpty: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var acc streamaccumulator.Thinking
			for _, c := range tt.chunks {
				acc.Add(c)
			}
			if got := acc.Empty(); got != tt.wantEmpty {
				t.Errorf("Empty() = %v, want %v", got, tt.wantEmpty)
			}
		})
	}
}
