package v1_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

// requestValidationError asserts err is the package's stable typed validation
// error and returns it. Every decoder in sessionwire/v1 reports failures through
// this one type so five downstream lanes can branch on Code without matching
// error text.
func requestValidationError(t *testing.T, err error) *sessionwire.RequestValidationError {
	t.Helper()
	if err == nil {
		t.Fatal("decoder accepted a record it must reject")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error %v (%T) is not a *sessionwire.RequestValidationError", err, err)
	}
	return validation
}

// TestVersionNegotiationRequestRejectsBase64EncodedVersionList pins the very
// first record exchanged on a HostLink. WireVersion is a uint8, so a naive
// []WireVersion decode routes a JSON string through encoding/json's base64
// []byte path and silently accepts "AQ==" as [1].
func TestVersionNegotiationRequestRejectsBase64EncodedVersionList(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"supported_versions":"AQ=="}`,
		`{"supported_versions":""}`,
		`{"supported_versions":"AAE="}`,
	} {
		var request sessionwire.VersionNegotiationRequest
		err := json.Unmarshal([]byte(body), &request)
		if err == nil {
			t.Fatalf("VersionNegotiationRequest accepted base64 string %s as %v", body, request.SupportedVersions)
		}
		validation := requestValidationError(t, err)
		if validation.Code != sessionwire.RequestValidationCodeInvalidField || validation.Field != "supported_versions" {
			t.Errorf("decode %s = %+v, want invalid_field supported_versions", body, validation)
		}
	}
}

// TestVersionNegotiationRequestRejectsOutOfRangeVersion keeps the widened
// numeric decode range-checked back into WireVersion instead of truncating.
func TestVersionNegotiationRequestRejectsOutOfRangeVersion(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"supported_versions":[257]}`,
		`{"supported_versions":[-1]}`,
		`{"supported_versions":[1.5]}`,
	} {
		var request sessionwire.VersionNegotiationRequest
		err := json.Unmarshal([]byte(body), &request)
		validation := requestValidationError(t, err)
		if validation.Code != sessionwire.RequestValidationCodeInvalidField || validation.Field != "supported_versions" {
			t.Errorf("decode %s = %+v, want invalid_field supported_versions", body, validation)
		}
	}
}

func TestVersionNegotiationRequestAcceptsNumericVersionList(t *testing.T) {
	t.Parallel()

	var request sessionwire.VersionNegotiationRequest
	if err := json.Unmarshal([]byte(`{"supported_versions":[1]}`), &request); err != nil {
		t.Fatalf("Unmarshal(VersionNegotiationRequest): %v", err)
	}
	if len(request.SupportedVersions) != 1 || request.SupportedVersions[0] != sessionwire.CurrentWireVersion {
		t.Errorf("SupportedVersions = %v, want [%d]", request.SupportedVersions, sessionwire.CurrentWireVersion)
	}
}

// TestObjectRecordsRejectDuplicateMembers closes the request-smuggling hole in
// the two redaction-boundary records: every other record rejects a duplicate
// object member, and two parsers disagreeing on which copy wins is exactly the
// primitive this contract exists to deny.
func TestObjectRecordsRejectDuplicateMembers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "object reference",
			body:      `{"object_id":"a","object_id":"b"}`,
			wantField: "object_id",
			decode: func(data []byte) error {
				var reference sessionwire.ObjectReference
				return json.Unmarshal(data, &reference)
			},
		},
		{
			name:      "object metadata",
			body:      `{"reference":{"object_id":"a"},"size_bytes":7,"size_bytes":9}`,
			wantField: "size_bytes",
			decode: func(data []byte) error {
				var metadata sessionwire.ObjectMetadata
				return json.Unmarshal(data, &metadata)
			},
		},
		{
			name:      "object metadata nested reference",
			body:      `{"reference":{"object_id":"a","object_id":"b"},"size_bytes":7}`,
			wantField: "object_id",
			decode: func(data []byte) error {
				var metadata sessionwire.ObjectMetadata
				return json.Unmarshal(data, &metadata)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validation := requestValidationError(t, tt.decode([]byte(tt.body)))
			if validation.Code != sessionwire.RequestValidationCodeDuplicateField {
				t.Errorf("decode %s code = %q, want duplicate_field", tt.body, validation.Code)
			}
			if validation.Field != tt.wantField {
				t.Errorf("decode %s field = %q, want %q", tt.body, validation.Field, tt.wantField)
			}
		})
	}
}

// TestObjectRecordsReportTypedRedactedFieldErrors keeps the documented promise
// that a validation error names fields and never values. A raw encoding/json
// *UnmarshalTypeError embeds the offending literal and the unexported shadow
// type name, and is invisible to errors.As on RequestValidationError.
func TestObjectRecordsReportTypedRedactedFieldErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantField string
		secret    string
		decode    func([]byte) error
	}{
		{
			name:      "metadata size type",
			body:      `{"reference":{"object_id":"a"},"size_bytes":"AKIAIOSFODNN7EXAMPLE"}`,
			wantField: "size_bytes",
			secret:    "AKIAIOSFODNN7EXAMPLE",
			decode: func(data []byte) error {
				var metadata sessionwire.ObjectMetadata
				return json.Unmarshal(data, &metadata)
			},
		},
		{
			name:      "metadata media type",
			body:      `{"reference":{"object_id":"a"},"size_bytes":7,"media_type":12345}`,
			wantField: "media_type",
			secret:    "12345",
			decode: func(data []byte) error {
				var metadata sessionwire.ObjectMetadata
				return json.Unmarshal(data, &metadata)
			},
		},
		{
			name:      "reference identity type",
			body:      `{"object_id":98765}`,
			wantField: "object_id",
			secret:    "98765",
			decode: func(data []byte) error {
				var reference sessionwire.ObjectReference
				return json.Unmarshal(data, &reference)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.decode([]byte(tt.body))
			validation := requestValidationError(t, err)
			if validation.Code != sessionwire.RequestValidationCodeInvalidField {
				t.Errorf("decode %s code = %q, want invalid_field", tt.body, validation.Code)
			}
			if validation.Field != tt.wantField {
				t.Errorf("decode %s field = %q, want %q", tt.body, validation.Field, tt.wantField)
			}
			if strings.Contains(err.Error(), tt.secret) {
				t.Errorf("error %q echoes the offending wire value %q", err, tt.secret)
			}
			if strings.Contains(err.Error(), "objectMetadata") || strings.Contains(err.Error(), "objectReference") {
				t.Errorf("error %q leaks an unexported shadow type name", err)
			}
		})
	}
}

// TestObjectRecordsStillDropUnknownMembers keeps the deliberate redaction
// behaviour: a provider URL or credential added by a newer producer is dropped
// rather than retained and proxied.
func TestObjectRecordsStillDropUnknownMembers(t *testing.T) {
	t.Parallel()

	var metadata sessionwire.ObjectMetadata
	body := `{"reference":{"object_id":"object-1","signed_url":"https://example.invalid/x?sig=abc"},"size_bytes":7,"signed_url":"https://example.invalid/y"}`
	if err := json.Unmarshal([]byte(body), &metadata); err != nil {
		t.Fatalf("Unmarshal(ObjectMetadata): %v", err)
	}
	if metadata.Reference.ObjectID != "object-1" || metadata.SizeBytes != 7 {
		t.Fatalf("ObjectMetadata = %+v, want object-1/7", metadata)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal(ObjectMetadata): %v", err)
	}
	if strings.Contains(string(encoded), "signed_url") {
		t.Errorf("re-encoded metadata proxies a redacted member: %s", encoded)
	}
}

// TestDuplicateMemberErrorDoesNotEchoAttackerControlledName is the top-level
// counterpart of the already-correct nested case: an unknown member reports
// Field "", so a duplicated unknown member must not report the name verbatim.
func TestDuplicateMemberErrorDoesNotEchoAttackerControlledName(t *testing.T) {
	t.Parallel()

	const secretMember = "AKIAIOSFODNN7EXAMPLE_wpJalrXUtn"
	tests := []struct {
		name   string
		body   string
		decode func([]byte) error
	}{
		{
			name: "request record",
			body: `{"version":1,"command_id":"cmd-1","session_id":"session-1","agent_id":"agent-1","` +
				secretMember + `":1,"` + secretMember + `":2}`,
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name: "response record",
			body: `{"code":"invalid_request","retryable":false,"` +
				secretMember + `":1,"` + secretMember + `":2}`,
			decode: func(data []byte) error {
				var envelope sessionwire.ErrorDetail
				return json.Unmarshal(data, &envelope)
			},
		},
		{
			name: "hostlink control record",
			body: `{"supported_versions":[1],"` + secretMember + `":1,"` + secretMember + `":2}`,
			decode: func(data []byte) error {
				var request sessionwire.VersionNegotiationRequest
				return json.Unmarshal(data, &request)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.decode([]byte(tt.body))
			validation := requestValidationError(t, err)
			if validation.Code != sessionwire.RequestValidationCodeDuplicateField {
				t.Errorf("code = %q, want duplicate_field", validation.Code)
			}
			if validation.Field != "" {
				t.Errorf("field = %q, want \"\" for a caller-chosen member name", validation.Field)
			}
			if strings.Contains(err.Error(), secretMember) {
				t.Errorf("error %q echoes the attacker-chosen member name", err)
			}
		})
	}
}

// TestDuplicateContractMemberStillNamesTheField keeps the diagnostic value of
// the redaction: a duplicate of a member this version actually defines is still
// named, because that name is contract text rather than caller bytes.
func TestDuplicateContractMemberStillNamesTheField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "request record",
			body:      `{"version":1,"command_id":"cmd-1","command_id":"cmd-2","session_id":"session-1","agent_id":"agent-1"}`,
			wantField: "command_id",
			decode: func(data []byte) error {
				var request sessionwire.CreateRequest
				return json.Unmarshal(data, &request)
			},
		},
		{
			name:      "response record",
			body:      `{"code":"invalid_request","code":"session_not_found","retryable":false}`,
			wantField: "code",
			decode: func(data []byte) error {
				var detail sessionwire.ErrorDetail
				return json.Unmarshal(data, &detail)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validation := requestValidationError(t, tt.decode([]byte(tt.body)))
			if validation.Code != sessionwire.RequestValidationCodeDuplicateField {
				t.Errorf("code = %q, want duplicate_field", validation.Code)
			}
			if validation.Field != tt.wantField {
				t.Errorf("field = %q, want %q", validation.Field, tt.wantField)
			}
		})
	}
}

// TestWireTimestampsNormalizeToUTC keeps two Factories that hold the same
// instant in different locations byte-identical on the wire. This package
// hand-rolls canonical hash framing and rejects non-fixed-point bodies; a
// producer-dependent timestamp offset would undo both.
func TestWireTimestampsNormalizeToUTC(t *testing.T) {
	t.Parallel()

	berlin := time.FixedZone("CET", 1*60*60)
	observed := time.Date(2026, 8, 29, 13, 0, 0, 0, berlin)
	expires := time.Date(2026, 8, 29, 14, 0, 0, 0, berlin)

	tests := []struct {
		name  string
		value any
		want  []string
	}{
		{
			name: "gate projection deadline",
			value: sessionwire.GateProjection{
				GateID:           "gate-1",
				Kind:             "harness.ask_user",
				OpenedEventID:    "event-1",
				OpenedJournalSeq: 1,
				Deadline:         expires,
				Answerability:    sessionwire.GateAnswerabilityResident,
			},
			want: []string{`"deadline":"2026-08-29T13:00:00Z"`},
		},
		{
			name: "session summary timestamps",
			value: sessionwire.SessionSummary{
				SessionID:    "session-1",
				AgentID:      "agent-1",
				State:        "idle",
				CreatedAt:    observed,
				LastActiveAt: expires,
			},
			want: []string{`"created_at":"2026-08-29T12:00:00Z"`, `"last_active_at":"2026-08-29T13:00:00Z"`},
		},
		{
			name: "session status updated at",
			value: sessionwire.SessionStatus{
				SessionID: "session-1",
				AgentID:   "agent-1",
				State:     "idle",
				Residency: "resident",
				UpdatedAt: observed,
			},
			want: []string{`"updated_at":"2026-08-29T12:00:00Z"`},
		},
		{
			name: "hostlink capacity report",
			value: sessionwire.HostLinkCapacityReport{
				Version:                sessionwire.CurrentWireVersion,
				HostID:                 "host-1",
				HostGeneration:         1,
				AgentID:                "agent-1",
				RuntimeCompatibilityID: "runtime-1",
				Placement:              sessionwire.HostPlacementPooled,
				InternalEndpoint:       "wss://host-1.internal/hostlink",
				IsolationClass:         sessionwire.HostIsolationClassTenantExclusive,
				Accepting:              true,
				AvailableCapacity:      1,
				ObservedAt:             observed,
				ExpiresAt:              expires,
			},
			want: []string{`"observed_at":"2026-08-29T12:00:00Z"`, `"expires_at":"2026-08-29T13:00:00Z"`},
		},
		{
			name: "hostlink registry observation",
			value: sessionwire.HostLinkRegistryObservation{
				Version:                sessionwire.CurrentWireVersion,
				TenantID:               "tenant-1",
				SessionID:              "session-1",
				HostID:                 "host-1",
				HostGeneration:         1,
				AgentID:                "agent-1",
				RuntimeCompatibilityID: "runtime-1",
				Placement:              sessionwire.HostPlacementPooled,
				InternalEndpoint:       "wss://host-1.internal/hostlink",
				Residency:              sessionwire.SessionResidencyResident,
				Accepting:              true,
				LeaseEpoch:             1,
				ObservedAt:             observed,
				ExpiresAt:              expires,
			},
			want: []string{`"observed_at":"2026-08-29T12:00:00Z"`, `"expires_at":"2026-08-29T13:00:00Z"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal(%T): %v", tt.value, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(string(encoded), want) {
					t.Errorf("encoding %s does not contain %s", encoded, want)
				}
			}
			if strings.Contains(string(encoded), "+01:00") {
				t.Errorf("encoding %s carries the producer's zone offset", encoded)
			}
		})
	}
}

// TestOptionalTimestampsRejectTheZeroInstant keeps a decode/encode hop
// shape-preserving. An accepted zero timestamp is dropped by the omitzero
// marshal path, so the record silently changes shape in transit.
func TestOptionalTimestampsRejectTheZeroInstant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantField string
		decode    func([]byte) error
	}{
		{
			name:      "session status updated_at",
			body:      `{"session_id":"session-1","agent_id":"agent-1","state":"idle","residency":"resident","journal_tip":1,"updated_at":"0001-01-01T00:00:00Z"}`,
			wantField: "updated_at",
			decode: func(data []byte) error {
				var status sessionwire.SessionStatus
				return json.Unmarshal(data, &status)
			},
		},
		{
			name:      "session summary created_at",
			body:      `{"session_id":"session-1","agent_id":"agent-1","state":"idle","created_at":"0001-01-01T00:00:00Z","last_active_at":"2026-08-29T12:00:00Z"}`,
			wantField: "created_at",
			decode: func(data []byte) error {
				var summary sessionwire.SessionSummary
				return json.Unmarshal(data, &summary)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validation := requestValidationError(t, tt.decode([]byte(tt.body)))
			if validation.Code != sessionwire.RequestValidationCodeInvalidField {
				t.Errorf("code = %q, want invalid_field", validation.Code)
			}
			if validation.Field != tt.wantField {
				t.Errorf("field = %q, want %q", validation.Field, tt.wantField)
			}
		})
	}
}

// TestInternalEndpointValidateReportsOneCodedErrorType keeps the only exported
// Validate method in the package that returned an uncoded bare-string error on
// the same stable vocabulary as everything else.
func TestInternalEndpointValidateReportsOneCodedErrorType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		endpoint sessionwire.InternalEndpoint
		wantCode sessionwire.RequestValidationCode
	}{
		{name: "empty", endpoint: "", wantCode: sessionwire.RequestValidationCodeMissingField},
		{name: "not a URL", endpoint: "://", wantCode: sessionwire.RequestValidationCodeInvalidField},
		{name: "wrong scheme", endpoint: "https://host-1.internal/hostlink", wantCode: sessionwire.RequestValidationCodeInvalidField},
		{name: "credentials", endpoint: "wss://user:pass@host-1.internal/hostlink", wantCode: sessionwire.RequestValidationCodeInvalidField},
		{name: "signed query", endpoint: "wss://host-1.internal/hostlink?sig=abc", wantCode: sessionwire.RequestValidationCodeInvalidField},
		{name: "fragment", endpoint: "wss://host-1.internal/hostlink#f", wantCode: sessionwire.RequestValidationCodeInvalidField},
		{name: "too long", endpoint: sessionwire.InternalEndpoint("wss://host-1.internal/" + strings.Repeat("a", 300)), wantCode: sessionwire.RequestValidationCodeInvalidField},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validation := requestValidationError(t, tt.endpoint.Validate())
			if validation.Code != tt.wantCode {
				t.Errorf("Validate(%q) code = %q, want %q", tt.endpoint, validation.Code, tt.wantCode)
			}
			if validation.Field != "internal_endpoint" {
				t.Errorf("Validate(%q) field = %q, want internal_endpoint", tt.endpoint, validation.Field)
			}
		})
	}
	if err := sessionwire.InternalEndpoint("wss://host-1.internal/hostlink").Validate(); err != nil {
		t.Errorf("Validate(valid endpoint) = %v, want nil", err)
	}
}

// assertValidationError pins both halves of the package's product: the stable
// Code a consumer branches on and the stable Field it reports. Four test files
// previously asserted only err != nil, which a refactor collapsing invalid_field
// into invalid_json would have passed unchanged.
func assertValidationError(t *testing.T, err error, wantCode sessionwire.RequestValidationCode, wantField string) {
	t.Helper()
	validation := requestValidationError(t, err)
	if validation.Code != wantCode || validation.Field != wantField {
		t.Errorf("error = {Code:%q Field:%q}, want {Code:%q Field:%q}",
			validation.Code, validation.Field, wantCode, wantField)
	}
}
