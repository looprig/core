package v1_test

import (
	"bytes"
	"encoding/json"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestStateChangingRequestsRejectMissingCommandID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "create",
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "command_id",
			body:      `{"version":1,"session_id":"session-1","agent_id":"agent-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "input",
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "command_id",
			body:      `{"version":1,"session_id":"session-1","blocks":[{"type":"text","text":"hello"}]}`,
			decode: func(data []byte) error {
				var request sessionwire.InputRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "interrupt",
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "command_id",
			body:      `{"version":1,"session_id":"session-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.InterruptRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "restore compatibility",
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "command_id",
			body:      `{"version":1,"session_id":"session-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.RestoreRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "gate response",
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "command_id",
			body:      `{"version":1,"session_id":"session-1","gate_id":"gate-1","action":"answer","values":{},"expected_open_event_id":"event-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.GateResponseRequest
				return json.Unmarshal(data, &request)
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

func TestCreateRequestRequiresClientGeneratedSessionID(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{body: `{"version":1,"command_id":"cmd-1","agent_id":"agent-1"}`, wantCode: sessionwire.RequestValidationCodeMissingField, wantField: "session_id"},
		{body: `{"version":1,"command_id":"cmd-1","session_id":"","agent_id":"agent-1"}`, wantCode: sessionwire.RequestValidationCodeInvalidField, wantField: "session_id"},
	} {
		var request sessionwire.CreateRequest
		assertValidationError(t, json.Unmarshal([]byte(tt.body), &request), tt.wantCode, tt.wantField)
	}

	request := sessionwire.CreateRequest{
		CommandEnvelope: sessionwire.CommandEnvelope{
			Version:   sessionwire.CurrentWireVersion,
			CommandID: "cmd:/client-generated",
		},
		SessionID: "client-session:opaque/1",
		AgentID:   "agent-1",
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("CreateRequest.Validate() error = %v", err)
	}
}

func TestStateChangingRequestJSONRoundTrip(t *testing.T) {
	t.Parallel()

	input := sessionwire.InputRequest{
		CommandEnvelope: sessionwire.CommandEnvelope{
			Version:   sessionwire.CurrentWireVersion,
			CommandID: "command-1",
		},
		SessionID: "session-1",
		Blocks:    json.RawMessage(`[{"type":"text","text":"hello"}]`),
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("Marshal(InputRequest): %v", err)
	}
	var decoded sessionwire.InputRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(InputRequest): %v\nJSON: %s", err, data)
	}
	if got, want := decoded.CommandID, input.CommandID; got != want {
		t.Errorf("CommandID = %q, want %q", got, want)
	}
	if !bytes.Equal(decoded.Blocks, input.Blocks) {
		t.Errorf("Blocks = %s, want %s", decoded.Blocks, input.Blocks)
	}
}

func TestGateResponseRequiresExactlyOneOpenVersion(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{body: `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"answer","values":{}}`, wantCode: sessionwire.RequestValidationCodeInvalidField, wantField: "expected_open_version"},
		{body: `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"answer","values":{},"expected_open_event_id":"event-1","expected_open_journal_seq":4}`, wantCode: sessionwire.RequestValidationCodeInvalidField, wantField: "expected_open_version"},
	} {
		var request sessionwire.GateResponseRequest
		assertValidationError(t, json.Unmarshal([]byte(tt.body), &request), tt.wantCode, tt.wantField)
	}
}

func TestCommandStatusPreservesUnknownResponseFields(t *testing.T) {
	t.Parallel()

	const body = `{"command_id":"cmd-1","status":"accepted","accepted_order":7,"future":{"retry_after_ms":10}}`
	var status sessionwire.CommandStatus
	if err := json.Unmarshal([]byte(body), &status); err != nil {
		t.Fatalf("Unmarshal(CommandStatus): %v", err)
	}
	if got, want := status.State, sessionwire.CommandStateAccepted; got != want {
		t.Errorf("State = %q, want %q", got, want)
	}
	if got := status.AdditionalFields()["future"]; !bytes.Equal(got, []byte(`{"retry_after_ms":10}`)) {
		t.Errorf("unknown response field = %s, want future field", got)
	}

	roundTrip, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal(CommandStatus): %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(roundTrip, &fields); err != nil {
		t.Fatalf("Unmarshal round-trip JSON: %v", err)
	}
	if got := fields["future"]; !bytes.Equal(got, []byte(`{"retry_after_ms":10}`)) {
		t.Errorf("round-trip future field = %s, want preserved value", got)
	}
}
