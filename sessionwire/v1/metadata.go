package v1

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

// MessageMetadata is a client's app-defined string fields on create or input.
// It is not identity; the verified sender is Principal.
type MessageMetadata map[string]string

const (
	MaxMetadataFields     = 16
	MaxMetadataKeyBytes   = 64
	MaxMetadataValueBytes = 1024
	MaxMetadataBytes      = 4096
)

const reservedMetadataKeyPrefix = "looprig"

// MetadataValidationCode identifies the rule a metadata object violates.
type MetadataValidationCode string

const (
	MetadataValidationCodeTooManyFields MetadataValidationCode = "too_many_fields"
	MetadataValidationCodeInvalidKey    MetadataValidationCode = "invalid_key"
	MetadataValidationCodeReservedKey   MetadataValidationCode = "reserved_key"
	MetadataValidationCodeValueTooLarge MetadataValidationCode = "value_too_large"
	MetadataValidationCodeInvalidValue  MetadataValidationCode = "invalid_value"
	MetadataValidationCodeTooLarge      MetadataValidationCode = "too_large"
)

// MetadataValidationError reports invalid metadata. Key is present only when
// it satisfies the grammar; Error never echoes caller-controlled key or value.
// Unwrap exposes a request-level malformed-field error to consumers.
type MetadataValidationError struct {
	Code MetadataValidationCode
	Key  string
}

func (e *MetadataValidationError) Error() string {
	return "sessionwire/v1: invalid request metadata: " + string(e.Code)
}
func (e *MetadataValidationError) Unwrap() error {
	return invalidRequest(RequestValidationCodeInvalidField, "metadata")
}

// Validate enforces field, key, value and canonical object byte limits.
// Iterating sorted keys gives deterministic errors.
func (m MessageMetadata) Validate() error {
	if len(m) > MaxMetadataFields {
		return &MetadataValidationError{Code: MetadataValidationCodeTooManyFields}
	}
	for _, key := range slices.Sorted(maps.Keys(m)) {
		if !metadataKeyValid(key) {
			return &MetadataValidationError{Code: MetadataValidationCodeInvalidKey}
		}
		if strings.HasPrefix(key, reservedMetadataKeyPrefix) {
			return &MetadataValidationError{Code: MetadataValidationCodeReservedKey, Key: key}
		}
		value := m[key]
		if len(value) > MaxMetadataValueBytes {
			return &MetadataValidationError{Code: MetadataValidationCodeValueTooLarge, Key: key}
		}
		if !metadataValueValid(value) {
			return &MetadataValidationError{Code: MetadataValidationCodeInvalidValue, Key: key}
		}
	}
	encoded, err := json.Marshal(map[string]string(m))
	if err != nil {
		return err
	}
	if len(encoded) > MaxMetadataBytes {
		return &MetadataValidationError{Code: MetadataValidationCodeTooLarge}
	}
	return nil
}

func metadataKeyValid(key string) bool {
	if len(key) == 0 || len(key) > MaxMetadataKeyBytes || key[0] < 'a' || key[0] > 'z' {
		return false
	}
	for i := 1; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func metadataValueValid(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 0x20 && r != '\t' && r != '\n' {
			return false
		}
	}
	return true
}

func reportableMetadataKey(key string) string {
	if metadataKeyValid(key) {
		return key
	}
	return ""
}

// decodeOptionalMetadata rejects null, non-object and empty metadata, all of
// which would otherwise alter meaning or bytes on a Core round trip.
func decodeOptionalMetadata(fields map[string]json.RawMessage) (MessageMetadata, error) {
	raw, ok := fields["metadata"]
	if !ok {
		return nil, nil
	}
	if isJSONNull(raw) {
		return nil, invalidRequest(RequestValidationCodeInvalidField, "metadata")
	}
	members, err := decodeJSONObject(raw)
	if err != nil {
		return nil, invalidNestedJSONObject(err, "metadata")
	}
	if len(members) == 0 {
		return nil, invalidRequest(RequestValidationCodeInvalidField, "metadata")
	}
	metadata := make(MessageMetadata, len(members))
	for _, key := range slices.Sorted(maps.Keys(members)) {
		value, err := decodeStrictJSONString(members[key])
		if err != nil {
			return nil, &MetadataValidationError{Code: MetadataValidationCodeInvalidValue, Key: reportableMetadataKey(key)}
		}
		metadata[key] = value
	}
	return metadata, nil
}
