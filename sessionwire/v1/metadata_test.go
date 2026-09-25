package v1_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func metadataOfEncodedSize(t *testing.T, size int) sessionwire.MessageMetadata {
	t.Helper()
	remaining := size - 29
	metadata := sessionwire.MessageMetadata{}
	for _, key := range []string{"a", "b", "c", "d"} {
		n := min(remaining, sessionwire.MaxMetadataValueBytes)
		metadata[key] = strings.Repeat("v", n)
		remaining -= n
	}
	encoded, err := json.Marshal(map[string]string(metadata))
	if err != nil || len(encoded) != size {
		t.Fatalf("fixture built %d bytes (%v), want %d", len(encoded), err, size)
	}
	return metadata
}

func manyFields(n int) sessionwire.MessageMetadata {
	metadata := sessionwire.MessageMetadata{}
	for i := range n {
		metadata[fmt.Sprintf("k%02d", i)] = "v"
	}
	return metadata
}

func TestMessageMetadataValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		metadata sessionwire.MessageMetadata
		code     sessionwire.MetadataValidationCode
		key      string
	}{
		{"nil", nil, "", ""},
		{"one field", sessionwire.MessageMetadata{"space": "family"}, "", ""},
		{"empty value", sessionwire.MessageMetadata{"space": ""}, "", ""},
		{"tab and newline", sessionwire.MessageMetadata{"note": "a\tb\nc"}, "", ""},
		{"16 fields", manyFields(sessionwire.MaxMetadataFields), "", ""},
		{"17 fields", manyFields(sessionwire.MaxMetadataFields + 1), sessionwire.MetadataValidationCodeTooManyFields, ""},
		{"64-byte key", sessionwire.MessageMetadata{"a" + strings.Repeat("b", 63): "v"}, "", ""},
		{"65-byte key", sessionwire.MessageMetadata{"a" + strings.Repeat("b", 64): "v"}, sessionwire.MetadataValidationCodeInvalidKey, ""},
		{"empty key", sessionwire.MessageMetadata{"": "v"}, sessionwire.MetadataValidationCodeInvalidKey, ""},
		{"uppercase key", sessionwire.MessageMetadata{"Space": "v"}, sessionwire.MetadataValidationCodeInvalidKey, ""},
		{"leading digit", sessionwire.MessageMetadata{"1space": "v"}, sessionwire.MetadataValidationCodeInvalidKey, ""},
		{"hyphen", sessionwire.MessageMetadata{"a-b": "v"}, sessionwire.MetadataValidationCodeInvalidKey, ""},
		{"reserved looprig", sessionwire.MessageMetadata{"looprig": "v"}, sessionwire.MetadataValidationCodeReservedKey, "looprig"},
		{"reserved looprig_x", sessionwire.MessageMetadata{"looprig_x": "v"}, sessionwire.MetadataValidationCodeReservedKey, "looprig_x"},
		{"1024-byte value", sessionwire.MessageMetadata{"note": strings.Repeat("v", sessionwire.MaxMetadataValueBytes)}, "", ""},
		{"1025-byte value", sessionwire.MessageMetadata{"note": strings.Repeat("v", sessionwire.MaxMetadataValueBytes+1)}, sessionwire.MetadataValidationCodeValueTooLarge, "note"},
		{"NUL", sessionwire.MessageMetadata{"note": "a\x00b"}, sessionwire.MetadataValidationCodeInvalidValue, "note"},
		{"carriage return", sessionwire.MessageMetadata{"note": "a\rb"}, sessionwire.MetadataValidationCodeInvalidValue, "note"},
		{"invalid UTF-8", sessionwire.MessageMetadata{"note": string([]byte{0xff})}, sessionwire.MetadataValidationCodeInvalidValue, "note"},
		{"4096 bytes", metadataOfEncodedSize(t, sessionwire.MaxMetadataBytes), "", ""},
		{"4097 bytes", metadataOfEncodedSize(t, sessionwire.MaxMetadataBytes+1), sessionwire.MetadataValidationCodeTooLarge, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.metadata.Validate()
			if tt.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var metadataErr *sessionwire.MetadataValidationError
			if !errors.As(err, &metadataErr) || metadataErr.Code != tt.code || metadataErr.Key != tt.key {
				t.Fatalf("error = %v, want code %q key %q", err, tt.code, tt.key)
			}
			assertValidationError(t, err, sessionwire.RequestValidationCodeInvalidField, "metadata")
		})
	}
}

func TestMetadataValidationErrorNeverEchoesKeyOrValue(t *testing.T) {
	t.Parallel()
	err := sessionwire.MessageMetadata{"secret_key": "a\x00hunter2"}.Validate()
	if err == nil {
		t.Fatal("Validate() = nil")
	}
	for _, forbidden := range []string{"secret_key", "hunter2"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("Error() echoes %q", forbidden)
		}
	}
	var metadataErr *sessionwire.MetadataValidationError
	if !errors.As(sessionwire.MessageMetadata{"AKIAEXAMPLE": "v"}.Validate(), &metadataErr) || metadataErr.Key != "" {
		t.Errorf("invalid_key reported Key %q", metadataErr.Key)
	}
}
