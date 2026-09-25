package v1_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

const testPrincipalJSON = `{"tenant":"tenant-1","subject":"subject-1","kind":"actor"}`

func decodeInto[T any](body string) (T, error) {
	var value T
	err := json.Unmarshal([]byte(body), &value)
	return value, err
}

func roundTrip[T any](body string) ([]byte, error) {
	value, err := decodeInto[T](body)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func TestMessageMembersRoundTripByteStable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		trip       func(string) ([]byte, error)
	}{
		{"create both", `{"version":1,"command_id":"c","session_id":"s","agent_id":"a","blocks":[{"type":"text","text":"x"}],"metadata":{"client":"oxy-ios","space":"family"},"principal":` + testPrincipalJSON + `}`, roundTrip[sessionwire.CreateRequest]},
		{"input both", `{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}],"metadata":{"space":"family"},"principal":` + testPrincipalJSON + `}`, roundTrip[sessionwire.InputRequest]},
		{"interrupt principal", `{"version":1,"command_id":"c","session_id":"s","principal":` + testPrincipalJSON + `}`, roundTrip[sessionwire.InterruptRequest]},
		{"restore principal", `{"version":1,"command_id":"c","session_id":"s","principal":` + testPrincipalJSON + `}`, roundTrip[sessionwire.RestoreRequest]},
		{"gate principal", `{"version":1,"command_id":"c","session_id":"s","gate_id":"g","action":"answer","values":{"answer":"yes"},"expected_open_event_id":"e","principal":` + testPrincipalJSON + `}`, roundTrip[sessionwire.GateResponseRequest]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.trip(tt.body)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.body {
				t.Errorf("round trip\n got: %s\nwant: %s", got, tt.body)
			}
		})
	}
}

func TestAbsentMessageMembersDecodeNilAndStayInvisible(t *testing.T) {
	t.Parallel()
	create, err := decodeInto[sessionwire.CreateRequest](`{"version":1,"command_id":"c","session_id":"s","agent_id":"a"}`)
	if err != nil || create.Metadata != nil || create.Principal != nil {
		t.Fatalf("create = %+v, %v", create, err)
	}
	interrupt, err := decodeInto[sessionwire.InterruptRequest](`{"version":1,"command_id":"c","session_id":"s"}`)
	if err != nil || interrupt.Principal != nil {
		t.Fatalf("interrupt = %+v, %v", interrupt, err)
	}
	for _, value := range []any{create, interrupt} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "metadata") || strings.Contains(string(encoded), "principal") {
			t.Errorf("absent member leaked into %s", encoded)
		}
	}
	create.Metadata = sessionwire.MessageMetadata{}
	if err := create.Validate(); err != nil {
		t.Errorf("Validate(empty metadata) = %v", err)
	}
}

func TestMessageMembersAreRefusedWhenMalformed(t *testing.T) {
	t.Parallel()
	const inputHead = `{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}]`
	decodeInput := func(body string) error { _, err := decodeInto[sessionwire.InputRequest](body); return err }
	decodeCreate := func(body string) error { _, err := decodeInto[sessionwire.CreateRequest](body); return err }
	decodeInterrupt := func(body string) error { _, err := decodeInto[sessionwire.InterruptRequest](body); return err }
	decodeRestore := func(body string) error { _, err := decodeInto[sessionwire.RestoreRequest](body); return err }
	decodeGate := func(body string) error { _, err := decodeInto[sessionwire.GateResponseRequest](body); return err }
	tests := []struct {
		name, body string
		decode     func(string) error
		code       sessionwire.RequestValidationCode
		field      string
		meta       sessionwire.MetadataValidationCode
	}{
		{"null metadata", inputHead + `,"metadata":null}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "metadata", ""},
		{"null principal", inputHead + `,"principal":null}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "principal", ""},
		{"empty metadata", inputHead + `,"metadata":{}}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "metadata", ""},
		{"metadata array", inputHead + `,"metadata":["a"]}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "metadata", ""},
		{"non-string value", inputHead + `,"metadata":{"space":1}}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "metadata", sessionwire.MetadataValidationCodeInvalidValue},
		{"duplicate key", inputHead + `,"metadata":{"a":"x","a":"y"}}`, decodeInput, sessionwire.RequestValidationCodeDuplicateField, "metadata", ""},
		{"reserved key", inputHead + `,"metadata":{"looprig_x":"v"}}`, decodeInput, sessionwire.RequestValidationCodeInvalidField, "metadata", sessionwire.MetadataValidationCodeReservedKey},
		{"17 fields", `{"version":1,"command_id":"c","session_id":"s","agent_id":"a","metadata":{` + seventeenFields() + `}}`, decodeCreate, sessionwire.RequestValidationCodeInvalidField, "metadata", sessionwire.MetadataValidationCodeTooManyFields},
		{"invalid UTF-8", inputHead + `,"metadata":{"a":"` + string([]byte{0xff}) + `"}}`, decodeInput, sessionwire.RequestValidationCodeInvalidJSON, "", ""},
		{"principal unknown member", inputHead + `,"principal":{"tenant":"t","subject":"s","kind":"actor","name":"Alex"}}`, decodeInput, sessionwire.RequestValidationCodeUnknownField, "principal", ""},
		{"principal unknown kind", `{"version":1,"command_id":"c","session_id":"s","principal":{"tenant":"t","subject":"s","kind":"robot"}}`, decodeInterrupt, sessionwire.RequestValidationCodeInvalidField, "principal.kind", ""},
		{"metadata on interrupt", `{"version":1,"command_id":"c","session_id":"s","metadata":{"a":"b"}}`, decodeInterrupt, sessionwire.RequestValidationCodeUnknownField, "", ""},
		{"metadata on restore", `{"version":1,"command_id":"c","session_id":"s","metadata":{"a":"b"}}`, decodeRestore, sessionwire.RequestValidationCodeUnknownField, "", ""},
		{"metadata on gate", `{"version":1,"command_id":"c","session_id":"s","gate_id":"g","action":"answer","values":{},"expected_open_event_id":"e","metadata":{"a":"b"}}`, decodeGate, sessionwire.RequestValidationCodeUnknownField, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.decode(tt.body)
			assertValidationError(t, err, tt.code, tt.field)
			if tt.meta != "" {
				var metadataErr *sessionwire.MetadataValidationError
				if !errors.As(err, &metadataErr) || metadataErr.Code != tt.meta {
					t.Errorf("error = %v, want metadata code %q", err, tt.meta)
				}
			}
		})
	}
}

func TestMetadataNullValueIsRejectedByCreateAndInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		decode     func(string) error
	}{
		{"create", `{"version":1,"command_id":"c","session_id":"s","agent_id":"a","metadata":{"a":null}}`, func(body string) error { _, err := decodeInto[sessionwire.CreateRequest](body); return err }},
		{"input", `{"version":1,"command_id":"c","session_id":"s","blocks":[{"type":"text","text":"x"}],"metadata":{"a":null}}`, func(body string) error { _, err := decodeInto[sessionwire.InputRequest](body); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.decode(tt.body)
			assertValidationError(t, err, sessionwire.RequestValidationCodeInvalidField, "metadata")
			var metadataErr *sessionwire.MetadataValidationError
			if !errors.As(err, &metadataErr) || metadataErr.Code != sessionwire.MetadataValidationCodeInvalidValue || metadataErr.Key != "a" {
				t.Fatalf("decode = %v, want invalid_value for key a", err)
			}
		})
	}
}

func seventeenFields() string {
	parts := make([]string, 0, 17)
	for _, key := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q"} {
		parts = append(parts, `"`+key+`":"v"`)
	}
	return strings.Join(parts, ",")
}

func TestGoConstructedRequestsValidateNewMembers(t *testing.T) {
	t.Parallel()
	envelope := sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "c"}
	bad := &sessionwire.Principal{Tenant: "t", Subject: "", Kind: sessionwire.PrincipalKindActor}
	for name, err := range map[string]error{
		"create metadata": sessionwire.CreateRequest{CommandEnvelope: envelope, SessionID: "s", AgentID: "a", Metadata: sessionwire.MessageMetadata{"Bad": "v"}}.Validate(),
		"input principal": sessionwire.InputRequest{CommandEnvelope: envelope, SessionID: "s", Blocks: json.RawMessage(`[{"type":"text","text":"x"}]`), Principal: bad}.Validate(),
		"interrupt":       sessionwire.InterruptRequest{CommandEnvelope: envelope, SessionID: "s", Principal: bad}.Validate(),
		"restore":         sessionwire.RestoreRequest{CommandEnvelope: envelope, SessionID: "s", Principal: bad}.Validate(),
		"gate":            sessionwire.GateResponseRequest{CommandEnvelope: envelope, SessionID: "s", GateID: "g", Action: "answer", Values: map[string]json.RawMessage{}, ExpectedOpenEventID: "e", Principal: bad}.Validate(),
	} {
		if err == nil {
			t.Errorf("%s: Validate() = nil", name)
		}
	}
	var interruptA, interruptB sessionwire.InterruptRequest
	var restoreA, restoreB sessionwire.RestoreRequest
	_ = interruptA == interruptB
	_ = restoreA == restoreB
}
