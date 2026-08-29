package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestErrorEnvelopeUsesStableCodeAndPreservesResponseExtensions(t *testing.T) {
	t.Parallel()

	const body = `{"error":{"code":"gate_expired","message":"gate response deadline passed","retryable":false},"request_id":"opaque-request"}`
	var envelope sessionwire.ErrorEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("Unmarshal(ErrorEnvelope): %v", err)
	}
	if got, want := envelope.Error.Code, sessionwire.ErrorCodeGateExpired; got != want {
		t.Errorf("error code = %q, want %q", got, want)
	}
	if got := envelope.AdditionalFields()["request_id"]; !bytes.Equal(got, []byte(`"opaque-request"`)) {
		t.Errorf("request_id = %s, want preserved extension", got)
	}
	roundTrip, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("Marshal(ErrorEnvelope): %v", err)
	}
	if !bytes.Contains(roundTrip, []byte(`"request_id":"opaque-request"`)) {
		t.Errorf("round-trip JSON dropped response extension: %s", roundTrip)
	}
}

func TestRequestValidationErrorsDoNotEchoRawPayload(t *testing.T) {
	t.Parallel()

	const secret = "do-not-echo-private-gate-answer"
	var request sessionwire.GateResponseRequest
	err := json.Unmarshal([]byte(`{"version":1,"command_id":"cmd-1","session_id":"session-1","gate_id":"gate-1","action":"answer","values":{"answer":"`+secret+`"}}`), &request)
	if err == nil {
		t.Fatal("invalid request decoded successfully")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error type = %T, want *RequestValidationError", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte(secret)) {
		t.Errorf("validation error leaked raw payload: %q", err)
	}
}

// TestDuplicateObjectMemberSurfacesStableDuplicateFieldCode pins the promise of
// RequestValidationCodeDuplicateField. Duplicate JSON members were already
// rejected, but every caller collapsed the raw decoder error into
// RequestValidationCodeInvalidJSON, so a client branching on "duplicate_field"
// could never observe it and the exported constant was unreachable.
func TestDuplicateObjectMemberSurfacesStableDuplicateFieldCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  string
		field string
		into  func() any
	}{
		{
			name:  "error envelope detail",
			body:  `{"code":"invalid_request","code":"session_not_found","retryable":false}`,
			field: "code",
			into:  func() any { return new(sessionwire.ErrorDetail) },
		},
		{
			name:  "session reset control",
			body:  `{"type":"session.reset","tenant_id":"tenant-1","session_id":"session-1","last_contiguous":1,"journal_tip":2,"journal_tip":3}`,
			field: "journal_tip",
			into:  func() any { return new(sessionwire.SessionReset) },
		},
		{
			name:  "journal tip hint",
			body:  `{"type":"journal_tip","tenant_id":"tenant-1","tenant_id":"tenant-2","session_id":"session-1","journal_tip":2}`,
			field: "tenant_id",
			into:  func() any { return new(sessionwire.JournalTip) },
		},
		{
			name:  "enduring publication",
			body:  `{"type":"enduring_publication","tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"type":"public"},"body":{"type":"public"}}`,
			field: "body",
			into:  func() any { return new(sessionwire.EnduringPublication) },
		},
		{
			// RequestValidationError.Field names contract members, never
			// caller-supplied bytes, so an arbitrary duplicated member name
			// keeps the stable code but is reported unnamed.
			name:  "caller-chosen duplicate member name is not reflected",
			body:  `{"type":"journal_tip","tenant_id":"tenant-1","session_id":"session-1","journal_tip":2,"ünsafe key":1,"ünsafe key":2}`,
			field: "",
			into:  func() any { return new(sessionwire.JournalTip) },
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := json.Unmarshal([]byte(tt.body), tt.into())
			if err == nil {
				t.Fatalf("decoded a document with a duplicate member: %s", tt.body)
			}
			var validation *sessionwire.RequestValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("error = %T (%v), want RequestValidationError", err, err)
			}
			if got, want := validation.Code, sessionwire.RequestValidationCodeDuplicateField; got != want {
				t.Errorf("code = %q, want %q", got, want)
			}
			if got, want := validation.Field, tt.field; got != want {
				t.Errorf("field = %q, want %q", got, want)
			}
		})
	}
}
