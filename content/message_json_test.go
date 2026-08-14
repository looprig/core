package content_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/looprig/core/content"
)

// TestToolResultMessageJSONPreservesToolUseID is the key regression:
// ToolResultMessage must NOT inherit Message's promoted MarshalJSON/UnmarshalJSON,
// which would silently drop ToolUseID. Marshal then unmarshal a ToolResultMessage
// and assert ToolUseID survives along with the blocks. It also pins the wire form:
// the rename is Go-type-only, so the JSON must still carry "role":"tool" and the
// "tool_use_id" field byte-for-byte.
func TestToolResultMessageJSONPreservesToolUseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           content.ToolResultMessage
		wantContains []string
	}{
		{
			name: "tool message with id and content block",
			in: content.ToolResultMessage{
				Message: content.Message{
					Role:   content.RoleTool,
					Blocks: []content.Block{&content.TextBlock{Text: "result"}},
				},
				ToolUseID: "tu_42",
			},
			wantContains: []string{`"role":"tool"`, `"tool_use_id":"tu_42"`},
		},
		{
			name: "tool message with id and no blocks",
			in: content.ToolResultMessage{
				Message:   content.Message{Role: content.RoleTool},
				ToolUseID: "tu_7",
			},
			wantContains: []string{`"role":"tool"`, `"tool_use_id":"tu_7"`},
		},
		{
			name: "tool message with nested tool_result block",
			in: content.ToolResultMessage{
				Message: content.Message{
					Role: content.RoleTool,
					Blocks: []content.Block{
						&content.ToolResultBlock{
							ToolUseID: "tu_inner",
							Content:   []content.Block{&content.TextBlock{Text: "nested"}},
						},
					},
				},
				ToolUseID: "tu_outer",
			},
			wantContains: []string{`"role":"tool"`, `"tool_use_id":"tu_outer"`},
		},
		{
			name: "error result preserves IsError true on the wire",
			in: content.ToolResultMessage{
				Message: content.Message{
					Role:   content.RoleTool,
					Blocks: []content.Block{&content.TextBlock{Text: "tool error: boom"}},
				},
				ToolUseID: "tu_err",
				IsError:   true,
			},
			// omitempty omits false; true is emitted as "is_error":true.
			wantContains: []string{`"role":"tool"`, `"tool_use_id":"tu_err"`, `"is_error":true`},
		},
		{
			name: "success result omits IsError on the wire (false)",
			in: content.ToolResultMessage{
				Message: content.Message{
					Role:   content.RoleTool,
					Blocks: []content.Block{&content.TextBlock{Text: "ok"}},
				},
				ToolUseID: "tu_ok",
				IsError:   false,
			},
			wantContains: []string{`"role":"tool"`, `"tool_use_id":"tu_ok"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("json.Marshal(ToolResultMessage) error = %v", err)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(string(data), want) {
					t.Errorf("marshalled JSON = %s, want it to contain %s", data, want)
				}
			}
			var got content.ToolResultMessage
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal(ToolResultMessage) error = %v", err)
			}
			if got.ToolUseID != tt.in.ToolUseID {
				t.Errorf("ToolUseID = %q, want %q (dropped by promoted method?)", got.ToolUseID, tt.in.ToolUseID)
			}
			if got.IsError != tt.in.IsError {
				t.Errorf("IsError = %v, want %v (dropped by codec?)", got.IsError, tt.in.IsError)
			}
			if !reflect.DeepEqual(got, tt.in) {
				t.Errorf("round trip = %#v, want %#v", got, tt.in)
			}
		})
	}
}

// TestMessageJSONRoundTrip verifies Message and the three message types that
// inherit its promoted codec (User/AI/System) round-trip through encoding/json.
func TestMessageJSONRoundTrip(t *testing.T) {
	t.Parallel()

	blocks := []content.Block{
		&content.TextBlock{Text: "hello"},
		&content.ThinkingBlock{Thinking: "hmm", Signature: "s"},
	}

	tests := []struct {
		name    string
		marshal func() ([]byte, error)
		decode  func([]byte) (any, error)
		want    any
	}{
		{
			name: "Message",
			marshal: func() ([]byte, error) {
				return json.Marshal(content.Message{Role: content.RoleUser, Blocks: blocks})
			},
			decode: func(b []byte) (any, error) {
				var m content.Message
				err := json.Unmarshal(b, &m)
				return m, err
			},
			want: content.Message{Role: content.RoleUser, Blocks: blocks},
		},
		{
			name: "UserMessage",
			marshal: func() ([]byte, error) {
				return json.Marshal(content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: blocks}})
			},
			decode: func(b []byte) (any, error) {
				var m content.UserMessage
				err := json.Unmarshal(b, &m)
				return m, err
			},
			want: content.UserMessage{Message: content.Message{Role: content.RoleUser, Blocks: blocks}},
		},
		{
			name: "AIMessage",
			marshal: func() ([]byte, error) {
				return json.Marshal(content.AIMessage{Message: content.Message{Role: content.RoleAssistant, Blocks: blocks}})
			},
			decode: func(b []byte) (any, error) {
				var m content.AIMessage
				err := json.Unmarshal(b, &m)
				return m, err
			},
			want: content.AIMessage{Message: content.Message{Role: content.RoleAssistant, Blocks: blocks}},
		},
		{
			name: "SystemMessage",
			marshal: func() ([]byte, error) {
				return json.Marshal(content.SystemMessage{Message: content.Message{Role: content.RoleSystem, Blocks: blocks}})
			},
			decode: func(b []byte) (any, error) {
				var m content.SystemMessage
				err := json.Unmarshal(b, &m)
				return m, err
			},
			want: content.SystemMessage{Message: content.Message{Role: content.RoleSystem, Blocks: blocks}},
		},
		{
			name: "Message with nil blocks",
			marshal: func() ([]byte, error) {
				return json.Marshal(content.Message{Role: content.RoleSystem})
			},
			decode: func(b []byte) (any, error) {
				var m content.Message
				err := json.Unmarshal(b, &m)
				return m, err
			},
			want: content.Message{Role: content.RoleSystem},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := tt.marshal()
			if err != nil {
				t.Fatalf("marshal error = %v", err)
			}
			got, err := tt.decode(data)
			if err != nil {
				t.Fatalf("decode error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("round trip = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestAIMessageJSONUsageRoundTrip(t *testing.T) {
	t.Parallel()

	allBlocks := []content.Block{
		&content.TextBlock{Text: "hello"},
		&content.ImageBlock{MediaType: content.MediaTypeImagePNG, Source: content.ImageSource{URL: "https://example.com/image.png"}},
		&content.AudioBlock{MediaType: content.MediaTypeAudioMPEG, Data: []byte("audio")},
		&content.DocumentBlock{MediaType: content.MediaTypeDocumentMarkdown, Name: "notes.md", Text: "notes"},
		&content.ThinkingBlock{Thinking: "considering", Signature: "sig_1"},
		&content.ToolUseBlock{ID: "tu_1", Name: "lookup", Input: json.RawMessage(`{"query":"tokens"}`)},
		&content.ToolResultBlock{
			ToolUseID: "tu_nested",
			Content: []content.Block{
				&content.TextBlock{Text: "nested result"},
				&content.ThinkingBlock{Thinking: "nested thought", Signature: "sig_nested"},
			},
		},
	}
	fullUsage := &content.Usage{
		InputTokens:         101,
		OutputTokens:        53,
		CacheReadTokens:     17,
		CacheCreationTokens: 19,
		ReasoningTokens:     23,
	}

	tests := []struct {
		name       string
		in         content.AIMessage
		wantUsage  *content.Usage
		wantBlocks []content.Block
		wantWire   string
		unwantWire string
		fixedPoint bool
	}{
		{
			name:       "nil usage is omitted and round trips nil",
			in:         content.AIMessage{Message: content.Message{Role: content.RoleAssistant}},
			wantUsage:  nil,
			unwantWire: `"usage"`,
			fixedPoint: true,
		},
		{
			name:       "present zero usage emits object and round trips non-nil",
			in:         content.AIMessage{Message: content.Message{Role: content.RoleAssistant}, Usage: &content.Usage{}},
			wantUsage:  &content.Usage{},
			wantWire:   `"usage":{}`,
			fixedPoint: true,
		},
		{
			name:       "all five usage fields survive",
			in:         content.AIMessage{Message: content.Message{Role: content.RoleAssistant}, Usage: fullUsage},
			wantUsage:  fullUsage,
			wantWire:   `"ReasoningTokens":23`,
			fixedPoint: true,
		},
		{
			name: "all supported tagged blocks remain faithful",
			in: content.AIMessage{
				Message: content.Message{Role: content.RoleAssistant, Blocks: allBlocks},
				Usage:   fullUsage,
			},
			wantUsage:  fullUsage,
			wantBlocks: allBlocks,
			wantWire:   `"type":"tool_use"`,
			fixedPoint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("json.Marshal(AIMessage) error = %v", err)
			}
			if tt.wantWire != "" && !strings.Contains(string(data), tt.wantWire) {
				t.Errorf("marshalled JSON = %s, want it to contain %s", data, tt.wantWire)
			}
			if tt.unwantWire != "" && strings.Contains(string(data), tt.unwantWire) {
				t.Errorf("marshalled JSON = %s, do not want it to contain %s", data, tt.unwantWire)
			}

			var got content.AIMessage
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal(AIMessage) error = %v", err)
			}
			if !reflect.DeepEqual(got.Usage, tt.wantUsage) {
				t.Errorf("Usage = %#v, want %#v", got.Usage, tt.wantUsage)
			}
			if !reflect.DeepEqual(got.Blocks, tt.wantBlocks) {
				t.Errorf("Blocks = %#v, want %#v", got.Blocks, tt.wantBlocks)
			}
			if tt.fixedPoint {
				remarshalled, err := json.Marshal(got)
				if err != nil {
					t.Fatalf("second json.Marshal(AIMessage) error = %v", err)
				}
				if !bytes.Equal(remarshalled, data) {
					t.Errorf("marshal fixed point = %s, want %s", remarshalled, data)
				}
			}
		})
	}
}

func TestAIMessageUnmarshalFreshState(t *testing.T) {
	t.Parallel()

	populated := content.AIMessage{
		Message: content.Message{
			Role:   content.RoleAssistant,
			Blocks: []content.Block{&content.TextBlock{Text: "stale"}},
		},
		Usage: &content.Usage{InputTokens: 10, OutputTokens: 5, ReasoningTokens: 2},
	}

	tests := []struct {
		name            string
		data            string
		want            content.AIMessage
		direct          bool
		wantSyntaxError bool
		wantTypeError   bool
	}{
		{
			name: "absent blocks and usage clear prior values",
			data: `{"role":"assistant"}`,
			want: content.AIMessage{Message: content.Message{Role: content.RoleAssistant}},
		},
		{
			name:            "direct method clears stale state for malformed top-level JSON",
			data:            `{"role":"assistant","usage":`,
			direct:          true,
			wantSyntaxError: true,
		},
		{
			name:          "usage object wrong type clears stale state and returns type error",
			data:          `{"role":"assistant","usage":"many"}`,
			wantTypeError: true,
		},
		{
			name:          "usage field wrong type clears stale state and returns type error",
			data:          `{"role":"assistant","usage":{"InputTokens":"many"}}`,
			wantTypeError: true,
		},
		{
			// A stored transcript must stay readable whatever the provider
			// reported. These are the live counts from an OpenRouter HTTP 200
			// against nvidia/nemotron-3-ultra-550b-a55b:free, which used to make
			// the message that carried them undecodable.
			name: "reasoning over output decodes as reported",
			data: `{"role":"assistant","usage":{"OutputTokens":216,"ReasoningTokens":226}}`,
			want: content.AIMessage{
				Message: content.Message{Role: content.RoleAssistant},
				Usage:   &content.Usage{OutputTokens: 216, ReasoningTokens: 226},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := populated
			var err error
			// encoding/json rejects malformed top-level syntax before it
			// dispatches to UnmarshalJSON, so only the direct method can clear
			// the receiver for that case. Semantic decode errors are valid JSON
			// and exercise the normal json.Unmarshal dispatch below.
			if tt.direct {
				err = got.UnmarshalJSON([]byte(tt.data))
			} else {
				err = json.Unmarshal([]byte(tt.data), &got)
			}
			if !tt.wantSyntaxError && !tt.wantTypeError {
				if err != nil {
					t.Fatalf("json.Unmarshal(AIMessage) error = %v", err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("decoded AIMessage = %#v, want %#v", got, tt.want)
				}
				return
			}

			if tt.wantSyntaxError {
				var target *json.SyntaxError
				if !errors.As(err, &target) {
					t.Fatalf("json.Unmarshal(AIMessage) error = %v, want *json.SyntaxError", err)
				}
			}
			if tt.wantTypeError {
				var target *json.UnmarshalTypeError
				if !errors.As(err, &target) {
					t.Fatalf("json.Unmarshal(AIMessage) error = %v, want *json.UnmarshalTypeError", err)
				}
			}
			if !reflect.DeepEqual(got, content.AIMessage{}) {
				t.Errorf("AIMessage after failed decode = %#v, want zero value", got)
			}
		})
	}
}

func TestAIMessageJSONErrorBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		run                  func() error
		wantInvalidUnmarshal bool
	}{
		{
			name: "nil receiver returns typed invalid unmarshal error",
			run: func() error {
				var dst *content.AIMessage
				return dst.UnmarshalJSON([]byte(`{"role":"assistant"}`))
			},
			wantInvalidUnmarshal: true,
		},
		{
			// This used to be the one place a *marshal* could fail on content
			// that had already been received and accepted. Usage is metrics; a
			// message that exists must always be writable.
			name: "marshal accepts reasoning metadata that diverges from the convention",
			run: func() error {
				_, err := json.Marshal(content.AIMessage{
					Message: content.Message{Role: content.RoleAssistant},
					Usage:   &content.Usage{OutputTokens: 216, ReasoningTokens: 226},
				})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.run()
			if !tt.wantInvalidUnmarshal {
				if err != nil {
					t.Fatalf("error = %v, want nil", err)
				}
				return
			}
			var target *json.InvalidUnmarshalError
			if !errors.As(err, &target) {
				t.Fatalf("error = %v, want *json.InvalidUnmarshalError", err)
			}
		})
	}
}

func FuzzAIMessageJSON(f *testing.F) {
	seeds := []string{
		`{"role":"assistant"}`,
		`{"role":"assistant","usage":{}}`,
		`{"role":"assistant","usage":{"InputTokens":8,"OutputTokens":5,"ReasoningTokens":3}}`,
		`{"role":"assistant","blocks":[{"type":"text","Text":"hello"}]}`,
		`{"role":"assistant","usage":{"ReasoningTokens":1}}`,
		`{"role":"assistant","usage":"invalid"}`,
		`{"role":"assistant","usage":`,
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		var message content.AIMessage
		if err := message.UnmarshalJSON(data); err != nil {
			return
		}

		encoded, err := message.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON() after successful UnmarshalJSON() error = %v", err)
		}
		var restored content.AIMessage
		if err := restored.UnmarshalJSON(encoded); err != nil {
			t.Fatalf("UnmarshalJSON(MarshalJSON()) error = %v", err)
		}
		if !reflect.DeepEqual(restored, message) {
			t.Fatalf("decode-encode-decode = %#v, want %#v", restored, message)
		}

		reencoded, err := restored.MarshalJSON()
		if err != nil {
			t.Fatalf("second MarshalJSON() error = %v", err)
		}
		if !bytes.Equal(reencoded, encoded) {
			t.Fatalf("marshal fixed point = %s, want %s", reencoded, encoded)
		}
	})
}
