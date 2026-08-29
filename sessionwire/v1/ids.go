package v1

import "unicode/utf8"

// MaxIDBytes is the largest permitted UTF-8 byte length of a sessionwire
// identity. IDs are opaque strings: this limit applies to encoded bytes, not
// runes, and no UUID grammar is implied.
const MaxIDBytes = 256

// TenantID identifies the tenant that owns a session.
type TenantID string

// SessionID identifies one session within a tenant.
type SessionID string

// AgentID identifies an agent participating in a session.
type AgentID string

// HostID identifies the host responsible for a session.
type HostID string

// CommandID identifies a client command. It is an opaque, non-empty UTF-8
// string of at most MaxIDBytes bytes; in particular, it is not a UUID.
type CommandID string

// EventID identifies a published session event.
type EventID string

// GateID identifies a gate awaiting a response.
type GateID string

// IDValidationCode is a stable machine-readable reason an identity is invalid.
type IDValidationCode string

const (
	// IDValidationCodeEmpty reports an empty identity.
	IDValidationCodeEmpty IDValidationCode = "empty"
	// IDValidationCodeTooLong reports an identity larger than MaxIDBytes bytes.
	IDValidationCodeTooLong IDValidationCode = "too_long"
	// IDValidationCodeInvalidUTF8 reports an identity that is not valid UTF-8.
	IDValidationCodeInvalidUTF8 IDValidationCode = "invalid_utf8"
)

// IDValidationError reports a failed identity validation. Code is stable for
// callers that need to distinguish validation failures without matching Error.
type IDValidationError struct {
	Code IDValidationCode
}

func (e *IDValidationError) Error() string {
	return "sessionwire/v1: invalid identity: " + string(e.Code)
}

// Validate reports whether id is a permitted opaque tenant identity.
func (id TenantID) Validate() error { return validateID(string(id)) }

// Validate reports whether id is a permitted opaque session identity.
func (id SessionID) Validate() error { return validateID(string(id)) }

// Validate reports whether id is a permitted opaque agent identity.
func (id AgentID) Validate() error { return validateID(string(id)) }

// Validate reports whether id is a permitted opaque host identity.
func (id HostID) Validate() error { return validateID(string(id)) }

// Validate reports whether id is a permitted opaque command identity.
func (id CommandID) Validate() error { return validateID(string(id)) }

// UnmarshalJSON rejects malformed JSON string encodings before they can be
// normalized into a different command identity by encoding/json.
func (id *CommandID) UnmarshalJSON(data []byte) error {
	value, err := decodeStrictJSONString(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "command_id")
	}
	*id = CommandID(value)
	return nil
}

// Validate reports whether id is a permitted opaque event identity.
func (id EventID) Validate() error { return validateID(string(id)) }

// Validate reports whether id is a permitted opaque gate identity.
func (id GateID) Validate() error { return validateID(string(id)) }

func validateID(value string) error {
	switch {
	case len(value) == 0:
		return &IDValidationError{Code: IDValidationCodeEmpty}
	case len(value) > MaxIDBytes:
		return &IDValidationError{Code: IDValidationCodeTooLong}
	case !utf8.ValidString(value):
		return &IDValidationError{Code: IDValidationCodeInvalidUTF8}
	default:
		return nil
	}
}
