package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestEmptyResponsePagesRoundTripWithJSONArrayRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		page      any
		arrayName string
		roundTrip func([]byte) ([]byte, error)
	}{
		{
			name:      "session page",
			page:      sessionwire.SessionPage{},
			arrayName: "sessions",
			roundTrip: func(data []byte) ([]byte, error) {
				var page sessionwire.SessionPage
				if err := json.Unmarshal(data, &page); err != nil {
					return nil, err
				}
				return json.Marshal(page)
			},
		},
		{
			name:      "journal page",
			page:      sessionwire.JournalPage{},
			arrayName: "events",
			roundTrip: func(data []byte) ([]byte, error) {
				var page sessionwire.JournalPage
				if err := json.Unmarshal(data, &page); err != nil {
					return nil, err
				}
				return json.Marshal(page)
			},
		},
		{
			name:      "gate page",
			page:      sessionwire.GatePage{},
			arrayName: "gates",
			roundTrip: func(data []byte) ([]byte, error) {
				var page sessionwire.GatePage
				if err := json.Unmarshal(data, &page); err != nil {
					return nil, err
				}
				return json.Marshal(page)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(tt.page)
			if err != nil {
				t.Fatalf("Marshal(%s): %v", tt.name, err)
			}
			assertJSONArrayMember(t, encoded, tt.arrayName)

			roundTripped, err := tt.roundTrip(encoded)
			if err != nil {
				t.Fatalf("round trip %s: %v\nJSON: %s", tt.name, err, encoded)
			}
			assertJSONArrayMember(t, roundTripped, tt.arrayName)
		})
	}
}

func TestNestedResponseExtensionsRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		roundTrip func([]byte) ([]byte, error)
		want      []string
	}{
		{
			name: "rejected command error retry guidance",
			body: `{"command_id":"cmd-1","status":"rejected","error":{"code":"gate_expired","retryable":true,"retry_after_ms":250}}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var status sessionwire.CommandStatus
				if err := json.Unmarshal(data, &status); err != nil {
					return nil, err
				}
				return json.Marshal(status)
			},
			want: []string{`"retry_after_ms":250`},
		},
		{
			name: "error envelope detail",
			body: `{"error":{"code":"runtime_unavailable","retryable":true,"error_detail_future":{"backoff":"slow"}}}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var envelope sessionwire.ErrorEnvelope
				if err := json.Unmarshal(data, &envelope); err != nil {
					return nil, err
				}
				return json.Marshal(envelope)
			},
			want: []string{`"error_detail_future":{"backoff":"slow"}`},
		},
		{
			name: "gate prompt and nested presentation records",
			body: `{"gate_id":"gate-1","kind":"harness.ask_user","prompt":{"title":"Choose","schema":{"fields":[{"name":"choice","label":"Choice","kind":"select","required":true,"options":[{"value":"yes","label":"Yes","option_future":true}],"field_future":true}],"schema_future":true},"controls":[{"action":"submit","label":"Submit","control_future":true}],"prompt_future":true},"opened_event_id":"event-1","opened_journal_seq":1,"deadline":"2026-08-29T12:00:00Z","answerability":"resident"}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var projection sessionwire.GateProjection
				if err := json.Unmarshal(data, &projection); err != nil {
					return nil, err
				}
				return json.Marshal(projection)
			},
			want: []string{
				`"prompt_future":true`,
				`"schema_future":true`,
				`"field_future":true`,
				`"option_future":true`,
				`"control_future":true`,
			},
		},
		{
			name: "public journal event",
			body: `{"events":[{"event_id":"event-1","journal_seq":1,"body":{"type":"public"},"event_future":{"stable":true}}],"journal_tip":1,"covered_through":1}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var page sessionwire.JournalPage
				if err := json.Unmarshal(data, &page); err != nil {
					return nil, err
				}
				return json.Marshal(page)
			},
			want: []string{`"event_future":{"stable":true}`},
		},
		{
			name: "agent and department capability summaries",
			body: `{"agents":[{"agent_id":"agent-1","runtime_compatibility_id":"runtime-1","agent_future":{"safe":true}}],"department_future":true}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var department sessionwire.DepartmentCapabilitySummary
				if err := json.Unmarshal(data, &department); err != nil {
					return nil, err
				}
				return json.Marshal(department)
			},
			want: []string{`"agent_future":{"safe":true}`, `"department_future":true`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			roundTripped, err := tt.roundTrip([]byte(tt.body))
			if err != nil {
				t.Fatalf("round trip response: %v\nJSON: %s", err, tt.body)
			}
			for _, want := range tt.want {
				if !bytes.Contains(roundTripped, []byte(want)) {
					t.Errorf("round-trip JSON dropped nested additive field %s: %s", want, roundTripped)
				}
			}
		})
	}
}

func TestObjectRecordsDoNotProxyUnknownMembersAcrossRedactionBoundary(t *testing.T) {
	t.Parallel()

	const body = `{"reference":{"object_id":"object-1","signed_url":"https://storage.example.test/object-1?signature=secret","reference_future":true},"size_bytes":42,"secret_bytes":"never durable","metadata_future":{"safe":true}}`
	var metadata sessionwire.ObjectMetadata
	if err := json.Unmarshal([]byte(body), &metadata); err != nil {
		t.Fatalf("Unmarshal(ObjectMetadata): %v", err)
	}
	roundTripped, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal(ObjectMetadata): %v", err)
	}
	for _, forbidden := range []string{"signed_url", "signature=secret", "secret_bytes", "never durable", "reference_future", "metadata_future"} {
		if bytes.Contains(roundTripped, []byte(forbidden)) {
			t.Errorf("object response re-emitted redaction-boundary member %q: %s", forbidden, roundTripped)
		}
	}
}

func TestGateResponseRequestStrictOptimisticVersionAndValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantField string
		wantCode  sessionwire.RequestValidationCode
	}{
		{
			name:      "explicit zero journal sequence",
			body:      `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{},"expected_open_journal_seq":0}`,
			wantField: "expected_open_journal_seq",
		},
		{
			name:      "zero journal sequence alongside event identity",
			body:      `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{},"expected_open_event_id":"event-1","expected_open_journal_seq":0}`,
			wantField: "expected_open_journal_seq",
		},
		{
			name:      "both optimistic versions",
			body:      `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{},"expected_open_event_id":"event-1","expected_open_journal_seq":1}`,
			wantField: "expected_open_version",
		},
		{
			// A duplicate key inside the caller-supplied answer map keeps the
			// stable duplicate_field code but reports only the enclosing
			// contract member, so no caller-chosen key is reflected back.
			name:      "duplicate answer value",
			body:      `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{"answer":"one","answer":"two"},"expected_open_event_id":"event-1"}`,
			wantField: "values",
			wantCode:  sessionwire.RequestValidationCodeDuplicateField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var request sessionwire.GateResponseRequest
			err := json.Unmarshal([]byte(tt.body), &request)
			if err == nil {
				t.Fatalf("GateResponseRequest decoded invalid request: %s", tt.body)
			}
			var validation *sessionwire.RequestValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T (%v), want RequestValidationError", err, err)
			}
			if got := validation.Field; got != tt.wantField {
				t.Errorf("validation Field = %q, want %q", got, tt.wantField)
			}
			if tt.wantCode != "" && validation.Code != tt.wantCode {
				t.Errorf("validation Code = %q, want %q", validation.Code, tt.wantCode)
			}
		})
	}

	const validBody = `{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"submit","values":{},"expected_open_journal_seq":1}`
	var request sessionwire.GateResponseRequest
	if err := json.Unmarshal([]byte(validBody), &request); err != nil {
		t.Fatalf("GateResponseRequest rejected valid journal-sequence version: %v", err)
	}
}

func TestGateResponseRequestRoundTripHasNoDecodeOnlyState(t *testing.T) {
	t.Parallel()

	for _, expectedOpenVersion := range []sessionwire.GateResponseRequest{
		{
			CommandEnvelope:     sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "cmd-1"},
			SessionID:           "session-1",
			GateID:              "gate-1",
			Action:              "submit",
			Values:              map[string]json.RawMessage{"answer": json.RawMessage(`"yes"`)},
			ExpectedOpenEventID: "event-1",
		},
		{
			CommandEnvelope:        sessionwire.CommandEnvelope{Version: sessionwire.CurrentWireVersion, CommandID: "cmd-1"},
			SessionID:              "session-1",
			GateID:                 "gate-1",
			Action:                 "submit",
			Values:                 map[string]json.RawMessage{"answer": json.RawMessage(`"yes"`)},
			ExpectedOpenJournalSeq: 1,
		},
	} {
		encoded, err := json.Marshal(expectedOpenVersion)
		if err != nil {
			t.Fatalf("Marshal(GateResponseRequest): %v", err)
		}
		var decoded sessionwire.GateResponseRequest
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("Unmarshal(GateResponseRequest): %v", err)
		}
		if !reflect.DeepEqual(decoded, expectedOpenVersion) {
			t.Errorf("GateResponseRequest round trip = %#v, want %#v", decoded, expectedOpenVersion)
		}
	}
}

func TestUnknownRequestFieldDoesNotEchoAttackerControlledName(t *testing.T) {
	t.Parallel()

	const secretField = "secret_access_token_do_not_echo"
	body := `{"version":1,"command_id":"cmd-1","session_id":"session-1","agent_id":"agent-1","` + secretField + `":"not-for-diagnostics"}`
	var request sessionwire.CreateRequest
	err := json.Unmarshal([]byte(body), &request)
	if err == nil {
		t.Fatal("CreateRequest decoded an unknown request field")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T (%v), want RequestValidationError", err, err)
	}
	if got, want := validation.Code, sessionwire.RequestValidationCodeUnknownField; got != want {
		t.Errorf("validation Code = %q, want %q", got, want)
	}
	if validation.Field != "" {
		t.Errorf("unknown request field diagnostic named attacker-controlled key %q", validation.Field)
	}
	if strings.Contains(err.Error(), secretField) {
		t.Errorf("unknown request field diagnostic echoed attacker-controlled key: %v", err)
	}
}

func assertJSONArrayMember(t *testing.T, data []byte, name string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("Unmarshal JSON object: %v\nJSON: %s", err, data)
	}
	if got := fields[name]; !bytes.Equal(got, []byte(`[]`)) {
		t.Errorf("JSON member %q = %s, want []\nJSON: %s", name, got, data)
	}
}
