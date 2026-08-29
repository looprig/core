package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
}

// Validate reports whether the public error has the stable code required for a
// client to branch without inspecting Message.
func (e ErrorDetail) Validate() error {
	if e.Code == "" {
		return invalidRequest(RequestValidationCodeMissingField, "error.code")
	}
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
	fields map[string]json.RawMessage
}

func (e responseExtensions) copy() map[string]json.RawMessage {
	if len(e.fields) == 0 {
		return nil
	}
	result := make(map[string]json.RawMessage, len(e.fields))
	for name, value := range e.fields {
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
	return responseExtensions{fields: extensions}
}

func marshalResponseFields(fields map[string]json.RawMessage, extensions responseExtensions) ([]byte, error) {
	for name, value := range extensions.fields {
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("sessionwire/v1: response extension collides with %q", name)
		}
		fields[name] = cloneJSON(value)
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

func cloneJSON(data json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), data...)
}
