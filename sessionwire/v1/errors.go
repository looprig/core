package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

// RequestValidationCode is a stable, transport-neutral reason a request record
// could not be decoded or validated. It intentionally names fields, never values,
// so request payloads such as gate answers do not leak through an error string.
type RequestValidationCode string

const (
	RequestValidationCodeInvalidJSON        RequestValidationCode = "invalid_json"
	RequestValidationCodeDuplicateField     RequestValidationCode = "duplicate_field"
	RequestValidationCodeUnknownField       RequestValidationCode = "unknown_field"
	RequestValidationCodeMissingField       RequestValidationCode = "missing_required_field"
	RequestValidationCodeInvalidField       RequestValidationCode = "invalid_field"
	RequestValidationCodeUnsupportedVersion RequestValidationCode = "unsupported_version"
)

// RequestValidationError reports a structural V1 request failure. Field is a
// stable field name rather than a caller-provided value.
type RequestValidationError struct {
	Code  RequestValidationCode
	Field string
}

func (e *RequestValidationError) Error() string {
	if e.Field == "" {
		return "sessionwire/v1: invalid request: " + string(e.Code)
	}
	return "sessionwire/v1: invalid request " + e.Field + ": " + string(e.Code)
}

func invalidRequest(code RequestValidationCode, field string) error {
	return &RequestValidationError{Code: code, Field: field}
}

// ErrorCode is a stable public error discriminator. Factory maps transport and
// domain failures into this vocabulary without exposing implementation causes.
type ErrorCode string

const (
	ErrorCodeInvalidRequest      ErrorCode = "invalid_request"
	ErrorCodeUnsupportedVersion  ErrorCode = "unsupported_version"
	ErrorCodeSessionNotFound     ErrorCode = "session_not_found"
	ErrorCodeCommandRejected     ErrorCode = "command_rejected"
	ErrorCodeGateResolved        ErrorCode = "gate_resolved"
	ErrorCodeGateNotResumable    ErrorCode = "gate_not_resumable"
	ErrorCodeGateExpired         ErrorCode = "gate_expired"
	ErrorCodeGateResponseInvalid ErrorCode = "gate_response_invalid"
	ErrorCodeRuntimeUnavailable  ErrorCode = "runtime_unavailable"
)

// ErrorDetail is the client-safe body of a stable error envelope.
type ErrorDetail struct {
	Code      ErrorCode `json:"code"`
	Message   string    `json:"message,omitempty"`
	Retryable bool      `json:"retryable"`

	extensions responseExtensions
}

// AdditionalFields returns copies of forward-compatible error-detail members.
func (e ErrorDetail) AdditionalFields() map[string]json.RawMessage {
	return e.extensions.copy()
}

// Validate reports whether the public error has the stable code required for a
// client to branch without inspecting Message.
func (e ErrorDetail) Validate() error {
	if e.Code == "" {
		return invalidRequest(RequestValidationCodeMissingField, "error.code")
	}
	return nil
}

func (e ErrorDetail) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "code", e.Code); err != nil {
		return nil, err
	}
	if e.Message != "" {
		if err := putJSONField(fields, "message", e.Message); err != nil {
			return nil, err
		}
	}
	if err := putJSONField(fields, "retryable", e.Retryable); err != nil {
		return nil, err
	}
	return marshalResponseFields(fields, e.extensions)
}

func (e *ErrorDetail) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	code, err := decodeRequiredString(fields, "code")
	if err != nil {
		return err
	}
	var message string
	if raw, ok := fields["message"]; ok {
		if isJSONNull(raw) {
			return invalidRequest(RequestValidationCodeInvalidField, "message")
		}
		message, err = decodeStrictJSONString(raw)
		if err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "message")
		}
	}
	var retryable bool
	if raw, ok := fields["retryable"]; ok {
		if isJSONNull(raw) || json.Unmarshal(raw, &retryable) != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "retryable")
		}
	}
	decoded := ErrorDetail{
		Code:       ErrorCode(code),
		Message:    message,
		Retryable:  retryable,
		extensions: captureExtensions(fields, "code", "message", "retryable"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*e = decoded
	return nil
}

// ErrorEnvelope is a forward-compatible response envelope. Unknown additive
// top-level fields are retained so a Core consumer can proxy a newer response
// without silently dropping data it does not yet understand.
type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields captured
// while unmarshalling. Mutating the returned map or values cannot alter a later
// re-marshal of this envelope.
func (e ErrorEnvelope) AdditionalFields() map[string]json.RawMessage {
	return e.extensions.copy()
}

// Validate reports whether the envelope carries a valid public error detail.
func (e ErrorEnvelope) Validate() error { return e.Error.Validate() }

func (e ErrorEnvelope) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "error", e.Error); err != nil {
		return nil, err
	}
	return marshalResponseFields(fields, e.extensions)
}

func (e *ErrorEnvelope) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	raw, ok := fields["error"]
	if !ok || isJSONNull(raw) {
		return invalidRequest(RequestValidationCodeMissingField, "error")
	}
	var detail ErrorDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "error")
	}
	if err := detail.Validate(); err != nil {
		return err
	}
	*e = ErrorEnvelope{Error: detail, extensions: captureExtensions(fields, "error")}
	return nil
}

// responseExtensions stores response members not known to this version. It is
// deliberately unexported: callers can inspect preserved data through
// AdditionalFields but cannot overwrite a known contract member.
type responseExtensions struct {
	fields *map[string]json.RawMessage
}

func (e responseExtensions) copy() map[string]json.RawMessage {
	if e.fields == nil || len(*e.fields) == 0 {
		return nil
	}
	result := make(map[string]json.RawMessage, len(*e.fields))
	for name, value := range *e.fields {
		result[name] = cloneJSON(value)
	}
	return result
}

func captureExtensions(fields map[string]json.RawMessage, known ...string) responseExtensions {
	knownFields := make(map[string]struct{}, len(known))
	for _, name := range known {
		knownFields[name] = struct{}{}
	}
	var extensions map[string]json.RawMessage
	for name, value := range fields {
		if _, ok := knownFields[name]; ok {
			continue
		}
		if extensions == nil {
			extensions = make(map[string]json.RawMessage)
		}
		extensions[name] = cloneJSON(value)
	}
	if len(extensions) == 0 {
		return responseExtensions{}
	}
	return responseExtensions{fields: &extensions}
}

func marshalResponseFields(fields map[string]json.RawMessage, extensions responseExtensions) ([]byte, error) {
	if extensions.fields != nil {
		for name, value := range *extensions.fields {
			if _, exists := fields[name]; exists {
				return nil, fmt.Errorf("sessionwire/v1: response extension collides with %q", name)
			}
			fields[name] = cloneJSON(value)
		}
	}
	return json.Marshal(fields)
}

func putJSONField(fields map[string]json.RawMessage, name string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	fields[name] = data
	return nil
}

func decodeJSONObject(data []byte) (map[string]json.RawMessage, error) {
	if err := validateStrictJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	start, ok := token.(json.Delim)
	if !ok || start != '{' {
		return nil, fmt.Errorf("expected JSON object")
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected object member name")
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("duplicate object member %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = cloneJSON(value)
	}
	token, err = decoder.Token()
	if err != nil {
		return nil, err
	}
	end, ok := token.(json.Delim)
	if !ok || end != '}' {
		return nil, fmt.Errorf("expected end of JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing JSON value")
		}
		return nil, err
	}
	return fields, nil
}

func isJSONNull(data []byte) bool {
	return bytes.Equal(bytes.TrimSpace(data), []byte("null"))
}

// validateStrictJSON rejects JSON strings that encoding/json otherwise accepts
// by replacing malformed UTF-8 or unpaired UTF-16 surrogate escapes with U+FFFD.
// Callers use it before decoding strings or retaining RawMessage bytes so a wire
// value cannot change identity while it crosses the Core boundary.
func validateStrictJSON(data []byte) error {
	if !json.Valid(data) {
		return fmt.Errorf("invalid JSON")
	}
	for offset := 0; offset < len(data); {
		if data[offset] != '"' {
			offset++
			continue
		}
		next, err := scanStrictJSONString(data, offset)
		if err != nil {
			return err
		}
		offset = next
	}
	return nil
}

func scanStrictJSONString(data []byte, offset int) (int, error) {
	for offset++; offset < len(data); {
		switch data[offset] {
		case '"':
			return offset + 1, nil
		case '\\':
			if offset+1 >= len(data) {
				return 0, fmt.Errorf("invalid JSON string escape")
			}
			switch data[offset+1] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				offset += 2
			case 'u':
				unit, ok := decodeJSONUTF16Unit(data, offset+2)
				if !ok {
					return 0, fmt.Errorf("invalid JSON unicode escape")
				}
				offset += 6
				switch {
				case unit >= 0xd800 && unit <= 0xdbff:
					if offset+1 >= len(data) || data[offset] != '\\' || data[offset+1] != 'u' {
						return 0, fmt.Errorf("unpaired JSON high surrogate")
					}
					low, ok := decodeJSONUTF16Unit(data, offset+2)
					if !ok || low < 0xdc00 || low > 0xdfff {
						return 0, fmt.Errorf("unpaired JSON high surrogate")
					}
					offset += 6
				case unit >= 0xdc00 && unit <= 0xdfff:
					return 0, fmt.Errorf("unpaired JSON low surrogate")
				}
			default:
				return 0, fmt.Errorf("invalid JSON string escape")
			}
		default:
			if data[offset] < 0x20 {
				return 0, fmt.Errorf("invalid JSON control character")
			}
			if data[offset] < utf8.RuneSelf {
				offset++
				continue
			}
			_, size := utf8.DecodeRune(data[offset:])
			if size == 1 {
				return 0, fmt.Errorf("invalid JSON UTF-8")
			}
			offset += size
		}
	}
	return 0, fmt.Errorf("unterminated JSON string")
}

func decodeJSONUTF16Unit(data []byte, offset int) (uint16, bool) {
	if offset+4 > len(data) {
		return 0, false
	}
	var unit uint16
	for _, value := range data[offset : offset+4] {
		unit <<= 4
		switch {
		case value >= '0' && value <= '9':
			unit |= uint16(value - '0')
		case value >= 'a' && value <= 'f':
			unit |= uint16(value-'a') + 10
		case value >= 'A' && value <= 'F':
			unit |= uint16(value-'A') + 10
		default:
			return 0, false
		}
	}
	return unit, true
}

func decodeStrictJSONString(data []byte) (string, error) {
	if err := validateStrictJSON(data); err != nil {
		return "", err
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	return value, nil
}

func cloneJSON(data json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), data...)
}

func nonNilSlice[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
