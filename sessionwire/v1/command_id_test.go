package v1_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

type commandIDVector struct {
	Name              string   `json:"name"`
	Domain            string   `json:"domain"`
	Fields            []string `json:"fields"`
	ExpectedCommandID string   `json:"expected_command_id"`
}

func TestDeterministicCommandIDGoldenVectors(t *testing.T) {
	vectors := loadCommandIDVectors(t)
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			got, err := sessionwire.DeterministicCommandID(vector.Domain, vector.Fields...)
			if err != nil {
				t.Fatalf("DeterministicCommandID(%q, %q) error = %v", vector.Domain, vector.Fields, err)
			}
			if got != sessionwire.CommandID(vector.ExpectedCommandID) {
				t.Errorf("DeterministicCommandID(%q, %q) = %q, want %q", vector.Domain, vector.Fields, got, vector.ExpectedCommandID)
			}
			if err := got.Validate(); err != nil {
				t.Errorf("derived CommandID %q Validate() error = %v", got, err)
			}
		})
	}
}

func TestDeterministicCommandIDLengthPrefixAntiCollision(t *testing.T) {
	left, err := sessionwire.DeterministicCommandID("command.concat", "ab", "c")
	if err != nil {
		t.Fatalf("DeterministicCommandID(left) error = %v", err)
	}
	right, err := sessionwire.DeterministicCommandID("command.concat", "a", "bc")
	if err != nil {
		t.Fatalf("DeterministicCommandID(right) error = %v", err)
	}
	if left == right {
		t.Fatalf("length-framed colliding concatenations produced the same CommandID %q", left)
	}
}

func TestDeterministicCommandIDDoesNotNormalizeUnicode(t *testing.T) {
	nfc, err := sessionwire.DeterministicCommandID("command.unicode", "\u00e9")
	if err != nil {
		t.Fatalf("DeterministicCommandID(NFC) error = %v", err)
	}
	nfd, err := sessionwire.DeterministicCommandID("command.unicode", "e\u0301")
	if err != nil {
		t.Fatalf("DeterministicCommandID(NFD) error = %v", err)
	}
	if nfc == nfd {
		t.Fatalf("DeterministicCommandID normalized distinct Unicode inputs to %q", nfc)
	}
}

func TestDeterministicCommandIDRejectsInvalidUTF8(t *testing.T) {
	tests := []struct {
		name   string
		domain string
		fields []string
		want   sessionwire.IDValidationCode
	}{
		{
			name:   "invalid domain",
			domain: string([]byte{0xff}),
			want:   sessionwire.IDValidationCodeInvalidUTF8,
		},
		{
			name:   "invalid field",
			domain: "command.valid",
			fields: []string{string([]byte{0xff})},
			want:   sessionwire.IDValidationCodeInvalidUTF8,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sessionwire.DeterministicCommandID(tt.domain, tt.fields...)
			var validationErr *sessionwire.IDValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("DeterministicCommandID(%q, %q) error = %T, want *IDValidationError", tt.domain, tt.fields, err)
			}
			if validationErr.Code != tt.want {
				t.Errorf("DeterministicCommandID(%q, %q) validation code = %q, want %q", tt.domain, tt.fields, validationErr.Code, tt.want)
			}
		})
	}
}

func TestCanonicalDeadlineUnixMillis(t *testing.T) {
	for _, tt := range []struct {
		name       string
		unixMillis int64
		want       string
	}{
		{name: "negative", unixMillis: -1, want: "-1"},
		{name: "zero", unixMillis: 0, want: "0"},
		{name: "positive", unixMillis: 1_700_000_000_123, want: "1700000000123"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := sessionwire.CanonicalDeadlineUnixMillis(time.UnixMilli(tt.unixMillis)); got != tt.want {
				t.Errorf("CanonicalDeadlineUnixMillis(%d) = %q, want %q", tt.unixMillis, got, tt.want)
			}
		})
	}
}

func loadCommandIDVectors(t *testing.T) []commandIDVector {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "command_id_vectors.json"))
	if err != nil {
		t.Fatalf("read command ID vectors: %v", err)
	}
	var vectors []commandIDVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("decode command ID vectors: %v", err)
	}
	return vectors
}
