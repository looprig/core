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
