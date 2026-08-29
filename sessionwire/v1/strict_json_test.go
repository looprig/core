package v1_test

import (
	"encoding/json"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestWireDecodersRejectMalformedJSONStringEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      []byte
		wantCode  sessionwire.RequestValidationCode
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "command id raw invalid utf8",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body: rawByteJSON(
				`{"version":1,"command_id":"cmd-`,
				`","session_id":"session-1","agent_id":"agent-1"}`,
			),
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "command id lone high surrogate",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      []byte(`{"version":1,"command_id":"cmd-\uD800","session_id":"session-1","agent_id":"agent-1"}`),
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "gate response value raw invalid utf8",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body: rawByteJSON(
				`{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{"answer":"`,
				`"},"expected_open_event_id":"event-1"}`,
			),
			decode: func(data []byte) error {
				var request sessionwire.GateResponseRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "gate response value lone low surrogate",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      []byte(`{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{"answer":"\uDC00"},"expected_open_event_id":"event-1"}`),
			decode: func(data []byte) error {
				var request sessionwire.GateResponseRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "input blocks raw invalid utf8",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body: rawByteJSON(
				`{"version":1,"command_id":"cmd-1","session_id":"session-1","blocks":[{"type":"text","text":"`,
				`"}]}`,
			),
			decode: func(data []byte) error {
				var request sessionwire.InputRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "input blocks lone high surrogate",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      []byte(`{"version":1,"command_id":"cmd-1","session_id":"session-1","blocks":[{"type":"text","text":"\uD800"}]}`),
			decode: func(data []byte) error {
				var request sessionwire.InputRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "journal event body raw invalid utf8",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body: rawByteJSON(
				`{"event_id":"event-1","journal_seq":1,"body":{"text":"`,
				`"}}`,
			),
			decode: func(data []byte) error {
				var event sessionwire.JournalEvent
				return json.Unmarshal(data, &event)
			},
		},
		{
			name:      "journal event body lone low surrogate",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      []byte(`{"event_id":"event-1","journal_seq":1,"body":{"text":"\uDC00"}}`),
			decode: func(data []byte) error {
				var event sessionwire.JournalEvent
				return json.Unmarshal(data, &event)
			},
		},
		{
			name:      "public gate prompt body lone high surrogate",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      []byte(`{"gate_id":"gate-1","kind":"harness.ask_user","prompt":{"body":"\uD800"},"opened_event_id":"event-1","opened_journal_seq":1,"deadline":"2026-08-29T12:00:00Z","answerability":"resident"}`),
			decode: func(data []byte) error {
				var projection sessionwire.GateProjection
				return json.Unmarshal(data, &projection)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertValidationError(t, tt.decode(tt.body), tt.wantCode, tt.wantField)
		})
	}
}

func TestRawWireBodiesRejectMalformedJSONStringEncodingDuringValidation(t *testing.T) {
	t.Parallel()

	invalidString := json.RawMessage([]byte{'"', 0xff, '"'})
	invalidBlocks := append(json.RawMessage(`[{"type":"text","text":`), invalidString...)
	invalidBlocks = append(invalidBlocks, []byte(`}]`)...)
	invalidBody := append(json.RawMessage(`{"text":`), invalidString...)
	invalidBody = append(invalidBody, '}')

	tests := []struct {
		name      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
		validate  func() error
	}{
		{
			name:      "input blocks",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "blocks",
			validate: func() error {
				return (sessionwire.InputRequest{
					CommandEnvelope: sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "cmd-1"},
					SessionID:       "session-1",
					Blocks:          invalidBlocks,
				}).Validate()
			},
		},
		{
			name:      "gate response value",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "values",
			validate: func() error {
				return (sessionwire.GateResponseRequest{
					CommandEnvelope:     sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "cmd-1"},
					SessionID:           "session-1",
					GateID:              "gate-1",
					Action:              "submit",
					Values:              map[string]json.RawMessage{"answer": invalidString},
					ExpectedOpenEventID: "event-1",
				}).Validate()
			},
		},
		{
			name:      "gate response value key",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "values",
			validate: func() error {
				return (sessionwire.GateResponseRequest{
					CommandEnvelope:     sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "cmd-1"},
					SessionID:           "session-1",
					GateID:              "gate-1",
					Action:              "submit",
					Values:              map[string]json.RawMessage{string([]byte{0xff}): json.RawMessage(`true`)},
					ExpectedOpenEventID: "event-1",
				}).Validate()
			},
		},
		{
			name:      "journal event body",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "body",
			validate: func() error {
				return (sessionwire.JournalEvent{
					EventID:    "event-1",
					JournalSeq: 1,
					Body:       invalidBody,
				}).Validate()
			},
		},
		{
			name:      "gate prompt default",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "prompt.schema.fields.default",
			validate: func() error {
				return (sessionwire.GatePromptField{
					Name:    "choice",
					Kind:    sessionwire.GateFieldKindText,
					Default: invalidString,
				}).Validate()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertValidationError(t, tt.validate(), tt.wantCode, tt.wantField)
		})
	}
}

func TestWireDecoderAcceptsValidSurrogatePair(t *testing.T) {
	t.Parallel()

	const body = `{"version":1,"command_id":"cmd-\uD83D\uDE00","session_id":"session-1","agent_id":"agent-1"}`
	var request sessionwire.CreateRequest
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatalf("CreateRequest rejected valid surrogate pair: %v", err)
	}
}

func TestStandaloneWireRecordsRejectMalformedJSONStringEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "command envelope command id",
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "command_id",
			body:      `{"version":1,"command_id":"cmd-\uD800"}`,
			decode: func(data []byte) error {
				var envelope sessionwire.CommandEnvelope
				return json.Unmarshal(data, &envelope)
			},
		},
		{
			name:      "object reference",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      `{"object_id":"object-\uD800"}`,
			decode: func(data []byte) error {
				var reference sessionwire.ObjectReference
				return json.Unmarshal(data, &reference)
			},
		},
		{
			name:      "object metadata media type",
			wantCode:  sessionwire.RequestValidationCodeInvalidJSON,
			wantField: "",
			body:      `{"reference":{"object_id":"object-1"},"size_bytes":1,"media_type":"application/\uD800"}`,
			decode: func(data []byte) error {
				var metadata sessionwire.ObjectMetadata
				return json.Unmarshal(data, &metadata)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assertValidationError(t, tt.decode([]byte(tt.body)), tt.wantCode, tt.wantField)
		})
	}
}

func rawByteJSON(prefix, suffix string) []byte {
	data := append([]byte(prefix), 0xff)
	return append(data, []byte(suffix)...)
}
