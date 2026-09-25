package v1_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestPrincipalStrictDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, body string
		code       sessionwire.RequestValidationCode
		field      string
	}{
		{"unknown member", `{"tenant":"t","subject":"s","kind":"actor","name":"Alex"}`, sessionwire.RequestValidationCodeUnknownField, "principal"},
		{"duplicate member", `{"tenant":"t","tenant":"u","subject":"s","kind":"actor"}`, sessionwire.RequestValidationCodeDuplicateField, "principal.tenant"},
		{"missing kind", `{"tenant":"t","subject":"s"}`, sessionwire.RequestValidationCodeMissingField, "principal.kind"},
		{"null tenant", `{"tenant":null,"subject":"s","kind":"actor"}`, sessionwire.RequestValidationCodeMissingField, "principal.tenant"},
		{"unknown kind", `{"tenant":"t","subject":"s","kind":"robot"}`, sessionwire.RequestValidationCodeInvalidField, "principal.kind"},
		{"non-string kind", `{"tenant":"t","subject":"s","kind":1}`, sessionwire.RequestValidationCodeInvalidField, "principal.kind"},
		{"empty subject", `{"tenant":"t","subject":"","kind":"actor"}`, sessionwire.RequestValidationCodeInvalidField, "principal.subject"},
		{"oversized subject", `{"tenant":"t","subject":"` + strings.Repeat("s", sessionwire.MaxIDBytes+1) + `","kind":"actor"}`, sessionwire.RequestValidationCodeInvalidField, "principal.subject"},
		{"not an object", `"actor"`, sessionwire.RequestValidationCodeInvalidField, "principal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var principal sessionwire.Principal
			assertValidationError(t, json.Unmarshal([]byte(tt.body), &principal), tt.code, tt.field)
		})
	}
}

func TestPrincipalRoundTripIsByteStableInMemberOrder(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"tenant":"tenant-1","subject":"subject-1","kind":"actor"}`,
		`{"tenant":"tenant-1","subject":"svc-1","kind":"service"}`,
	} {
		var principal sessionwire.Principal
		if err := json.Unmarshal([]byte(body), &principal); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(principal)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != body {
			t.Errorf("round trip = %s, want %s", got, body)
		}
	}
	var reordered sessionwire.Principal
	if err := json.Unmarshal([]byte(`{"kind":"actor","subject":"s","tenant":"t"}`), &reordered); err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(reordered); string(got) != `{"tenant":"t","subject":"s","kind":"actor"}` {
		t.Errorf("reordered = %s", got)
	}
}

func TestPrincipalMarshalRefusesAnInvalidPrincipal(t *testing.T) {
	t.Parallel()
	_, err := json.Marshal(sessionwire.Principal{Tenant: "t", Subject: "s", Kind: "robot"})
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) || validation.Field != "principal.kind" {
		t.Fatalf("Marshal(invalid kind) = %v", err)
	}
	if err := (sessionwire.Principal{}).Validate(); err == nil {
		t.Fatal("Validate(zero Principal) = nil")
	}
}
