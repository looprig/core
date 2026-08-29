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
		name   string
		body   string
		decode func([]byte) error
	}{
		{
			name: "create",
			body: `{"version":1,"session_id":"session-1","agent_id":"agent-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name: "input",
			body: `{"version":1,"session_id":"session-1","blocks":[{"type":"text","text":"hello"}]}`,
			decode: func(data []byte) error {
				var request sessionwire.InputRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name: "interrupt",
			body: `{"version":1,"session_id":"session-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.InterruptRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name: "restore compatibility",
			body: `{"version":1,"session_id":"session-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.RestoreRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name: "gate response",
			body: `{"version":1,"session_id":"session-1","gate_id":"gate-1","action":"answer","values":{},"expected_open_event_id":"event-1"}`,
			decode: func(data []byte) error {
				var request sessionwire.GateResponseRequest
				return json.Unmarshal(data, &request)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.decode([]byte(tt.body)); err == nil {
				t.Fatal("request without command_id decoded successfully")
			}
		})
	}
}

func TestCreateRequestRequiresClientGeneratedSessionID(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"version":1,"command_id":"cmd-1","agent_id":"agent-1"}`,
		`{"version":1,"command_id":"cmd-1","session_id":"","agent_id":"agent-1"}`,
	} {
		var request sessionwire.CreateRequest
		if err := json.Unmarshal([]byte(body), &request); err == nil {
			t.Fatalf("CreateRequest decoded without a client session id: %s", body)
		}
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

	for _, body := range []string{
		`{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"answer","values":{}}`,
		`{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"answer","values":{},"expected_open_event_id":"event-1","expected_open_journal_seq":4}`,
	} {
		var request sessionwire.GateResponseRequest
		if err := json.Unmarshal([]byte(body), &request); err == nil {
			t.Fatalf("GateResponseRequest decoded with ambiguous/missing open version: %s", body)
		}
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
