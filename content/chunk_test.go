package content_test

import (
	"reflect"
	"testing"

	"github.com/looprig/core/content"
)

// TestChunk_ConcretePayloads verifies the concrete Chunk variants carry their
// delta text. The concrete type is the discriminator; there is no Type field.
func TestChunk_ConcretePayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		chunk     content.Chunk
		wantText  string               // expected text for a *TextChunk
		wantThink string               // expected thinking for a *ThinkingChunk
		wantSig   string               // expected signature for a *ThinkingChunk
		wantTool  content.ToolUseChunk // expected fields for a *ToolUseChunk
	}{
		{
			name:     "text chunk carries text payload",
			chunk:    &content.TextChunk{Text: "hello"},
			wantText: "hello",
		},
		{
			name:      "thinking chunk carries thinking payload",
			chunk:     &content.ThinkingChunk{Thinking: "reasoning", Signature: "sig"},
			wantThink: "reasoning",
			wantSig:   "sig",
		},
		{
			name:     "text chunk with empty string is a valid delta",
			chunk:    &content.TextChunk{Text: ""},
			wantText: "",
		},
		{
			name:      "thinking chunk with empty string is a valid delta",
			chunk:     &content.ThinkingChunk{Thinking: ""},
			wantThink: "",
		},
		{
			name:     "tool-use chunk carries index/id/name/inputjson payload",
			chunk:    &content.ToolUseChunk{Index: 1, ID: "call_1", Name: "read", InputJSON: `{"p":`},
			wantTool: content.ToolUseChunk{Index: 1, ID: "call_1", Name: "read", InputJSON: `{"p":`},
		},
		{
			name:     "tool-use chunk with empty id/name (non-first delta) carries only fragment",
			chunk:    &content.ToolUseChunk{Index: 0, InputJSON: `"x"}`},
			wantTool: content.ToolUseChunk{Index: 0, InputJSON: `"x"}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			switch c := tt.chunk.(type) {
			case *content.TextChunk:
				if c.Text != tt.wantText {
					t.Errorf("TextChunk.Text = %q, want %q", c.Text, tt.wantText)
				}
			case *content.ThinkingChunk:
				if c.Thinking != tt.wantThink {
					t.Errorf("ThinkingChunk.Thinking = %q, want %q", c.Thinking, tt.wantThink)
				}
				if c.Signature != tt.wantSig {
					t.Errorf("ThinkingChunk.Signature = %q, want %q", c.Signature, tt.wantSig)
				}
			case *content.ToolUseChunk:
				if !reflect.DeepEqual(*c, tt.wantTool) {
					t.Errorf("ToolUseChunk = %+v, want %+v", *c, tt.wantTool)
				}
			default:
				t.Fatalf("unexpected chunk type %T", c)
			}
		})
	}
}

// TestChunk_InterfaceCompliance is a compile-time check that the concrete chunk
// types satisfy the sealed Chunk interface.
// Acceptable exception to the table-driven rule: purely compile-time, no runtime path to branch.
func TestChunk_InterfaceCompliance(t *testing.T) {
	var _ content.Chunk = (*content.TextChunk)(nil)
	var _ content.Chunk = (*content.ThinkingChunk)(nil)
	var _ content.Chunk = (*content.ToolUseChunk)(nil)
	var _ content.Chunk = (*content.RefusalChunk)(nil)
	var _ content.Chunk = (*content.ImageChunk)(nil)
}

// TestRefusalChunk verifies the refusal delta carries its text payload and, like
// TextChunk, has no Index: refusal deltas fold into a single RefusalBlock.
func TestRefusalChunk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk content.RefusalChunk
		want  content.RefusalChunk
	}{
		{
			name:  "refusal chunk carries text payload",
			chunk: content.RefusalChunk{Text: "I'm sorry"},
			want:  content.RefusalChunk{Text: "I'm sorry"},
		},
		{
			name:  "empty refusal delta is valid",
			chunk: content.RefusalChunk{Text: ""},
			want:  content.RefusalChunk{Text: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !reflect.DeepEqual(tt.chunk, tt.want) {
				t.Errorf("RefusalChunk = %+v, want %+v", tt.chunk, tt.want)
			}
		})
	}
}

// TestImageChunk verifies the image delta carries an Index plus the same
// MediaType/Source pair as ImageBlock. The Index is load-bearing: image bytes
// from two different images must never be concatenated.
func TestImageChunk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chunk content.ImageChunk
		want  content.ImageChunk
	}{
		{
			name:  "inline data fragment at index 0",
			chunk: content.ImageChunk{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0x89, 0x50}}},
			want:  content.ImageChunk{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{Data: []byte{0x89, 0x50}}},
		},
		{
			name:  "URL arrives whole on a single delta",
			chunk: content.ImageChunk{Index: 1, MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{URL: "https://example.com/a.jpg"}},
			want:  content.ImageChunk{Index: 1, MediaType: content.MediaTypeImageJPEG, Source: content.ImageSource{URL: "https://example.com/a.jpg"}},
		},
		{
			name:  "continuation fragment carries no media type",
			chunk: content.ImageChunk{Index: 2, Source: content.ImageSource{Data: []byte{0x0D, 0x0A}}},
			want:  content.ImageChunk{Index: 2, Source: content.ImageSource{Data: []byte{0x0D, 0x0A}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !reflect.DeepEqual(tt.chunk, tt.want) {
				t.Errorf("ImageChunk = %+v, want %+v", tt.chunk, tt.want)
			}
		})
	}
}
