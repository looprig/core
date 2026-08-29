package v1_test

import (
	"errors"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

type validatableID interface {
	Validate() error
}

func TestIdentityIDsAcceptBoundedUTF8(t *testing.T) {
	t.Parallel()
	for _, id := range []validatableID{
		sessionwire.TenantID("tenant-1"),
		sessionwire.SessionID("session-1"),
		sessionwire.AgentID("agent-1"),
		sessionwire.HostID("host-1"),
		sessionwire.CommandID(strings.Repeat("x", sessionwire.MaxIDBytes)),
		sessionwire.EventID("event-1"),
		sessionwire.GateID("gate-1"),
	} {
		if err := id.Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	}
}

func TestCommandIDValidationCodes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   sessionwire.CommandID
		want sessionwire.IDValidationCode
	}{
		{name: "empty", id: "", want: sessionwire.IDValidationCodeEmpty},
		{
			name: "too long",
			id:   sessionwire.CommandID(strings.Repeat("x", sessionwire.MaxIDBytes+1)),
			want: sessionwire.IDValidationCodeTooLong,
		},
		{
			name: "invalid UTF-8",
			id:   sessionwire.CommandID(string([]byte{0xff})),
			want: sessionwire.IDValidationCodeInvalidUTF8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.id.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
			var validationErr *sessionwire.IDValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("Validate() error = %T, want *IDValidationError", err)
			}
			if validationErr.Code != tt.want {
				t.Errorf("Validate() code = %q, want %q", validationErr.Code, tt.want)
			}
		})
	}
}

func TestCommandIDLengthUsesUTF8Bytes(t *testing.T) {
	t.Parallel()

	atLimit := sessionwire.CommandID(strings.Repeat("é", 128))
	if err := atLimit.Validate(); err != nil {
		t.Fatalf("Validate() for 128 é values = %v, want nil", err)
	}

	overLimit := sessionwire.CommandID(strings.Repeat("é", 129))
	err := overLimit.Validate()
	var validationErr *sessionwire.IDValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("Validate() for 129 é values = %T, want *IDValidationError", err)
	}
	if validationErr.Code != sessionwire.IDValidationCodeTooLong {
		t.Errorf("Validate() code = %q, want %q", validationErr.Code, sessionwire.IDValidationCodeTooLong)
	}
}
