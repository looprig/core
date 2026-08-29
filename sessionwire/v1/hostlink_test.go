package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestHostLinkBindJSONGolden(t *testing.T) {
	t.Parallel()

	bind := sessionwire.HostLinkBindRequest{
		Version:                sessionwire.CurrentWireVersion,
		TenantID:               "tenant-1",
		SessionID:              "session-1",
		HostID:                 "host-1",
		HostGeneration:         7,
		LeaseEpoch:             3,
		RuntimeCompatibilityID: "runtime-2026-08",
		IdempotencyKey:         "bind-1",
	}
	data, err := json.Marshal(bind)
	if err != nil {
		t.Fatalf("Marshal(HostLinkBindRequest): %v", err)
	}
	const want = `{"host_generation":7,"host_id":"host-1","idempotency_key":"bind-1","lease_epoch":3,"runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","version":1}`
	if !bytes.Equal(data, []byte(want)) {
		t.Errorf("HostLinkBindRequest JSON = %s, want %s", data, want)
	}

	var decoded sessionwire.HostLinkBindRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(HostLinkBindRequest): %v", err)
	}
	if decoded != bind {
		t.Errorf("HostLinkBindRequest round trip = %#v, want %#v", decoded, bind)
	}
}

func TestHostLinkMessageGoldens(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "unbind",
			value: sessionwire.HostLinkUnbindRequest{
				Version:        sessionwire.CurrentWireVersion,
				TenantID:       "tenant-1",
				SessionID:      "session-1",
				HostID:         "host-1",
				HostGeneration: 7,
				LeaseEpoch:     3,
				IdempotencyKey: "unbind-1",
			},
			want: `{"host_generation":7,"host_id":"host-1","idempotency_key":"unbind-1","lease_epoch":3,"session_id":"session-1","tenant_id":"tenant-1","version":1}`,
		},
		{
			name: "accepted command delivery",
			value: sessionwire.HostLinkCommandDelivery{
				CommandID: "command-1",
			},
			want: `{"command_id":"command-1"}`,
		},
		{
			name: "capacity",
			value: sessionwire.HostLinkCapacityReport{
				Version:                sessionwire.CurrentWireVersion,
				HostID:                 "host-1",
				HostGeneration:         7,
				AgentID:                "agent-1",
				RuntimeCompatibilityID: "runtime-2026-08",
				Placement:              sessionwire.HostPlacementPooled,
				InternalEndpoint:       "wss://host-1.internal/hostlink",
				Accepting:              true,
				AvailableCapacity:      4,
				ObservedAt:             observedAt,
				ExpiresAt:              observedAt.Add(time.Minute),
			},
			want: `{"accepting":true,"agent_id":"agent-1","available_capacity":4,"expires_at":"2026-08-29T12:01:00Z","host_generation":7,"host_id":"host-1","internal_endpoint":"wss://host-1.internal/hostlink","observed_at":"2026-08-29T12:00:00Z","placement":"pooled","runtime_compatibility_id":"runtime-2026-08","version":1}`,
		},
		{
			name: "registry observation",
			value: sessionwire.HostLinkRegistryObservation{
				Version:                sessionwire.CurrentWireVersion,
				TenantID:               "tenant-1",
				SessionID:              "session-1",
				HostID:                 "host-1",
				HostGeneration:         7,
				AgentID:                "agent-1",
				RuntimeCompatibilityID: "runtime-2026-08",
				Placement:              sessionwire.HostPlacementPooled,
				InternalEndpoint:       "wss://host-1.internal/hostlink",
				Residency:              sessionwire.SessionResidencyResident,
				Accepting:              true,
				LeaseEpoch:             3,
				ObservedAt:             observedAt,
				ExpiresAt:              observedAt.Add(time.Minute),
			},
			want: `{"accepting":true,"agent_id":"agent-1","expires_at":"2026-08-29T12:01:00Z","host_generation":7,"host_id":"host-1","internal_endpoint":"wss://host-1.internal/hostlink","lease_epoch":3,"observed_at":"2026-08-29T12:00:00Z","placement":"pooled","residency":"resident","runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","version":1}`,
		},
		{
			name: "dedicated drain",
			value: sessionwire.HostLinkDrainRequest{
				Version:        sessionwire.CurrentWireVersion,
				HostID:         "host-1",
				HostGeneration: 7,
				IdempotencyKey: "drain-1",
				TenantID:       "tenant-1",
				SessionID:      "session-1",
			},
			want: `{"host_generation":7,"host_id":"host-1","idempotency_key":"drain-1","session_id":"session-1","tenant_id":"tenant-1","version":1}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal(%T): %v", tt.value, err)
			}
			if !bytes.Equal(data, []byte(tt.want)) {
				t.Errorf("JSON = %s, want %s", data, tt.want)
			}
		})
	}
}

func TestHostLinkAcceptedCommandDeliveryExcludesRuntimePayload(t *testing.T) {
	t.Parallel()

	delivery := sessionwire.HostLinkCommandDelivery{
		CommandID: "client-command:opaque",
	}
	data, err := json.Marshal(delivery)
	if err != nil {
		t.Fatalf("Marshal(HostLinkCommandDelivery): %v", err)
	}
	for _, forbidden := range []string{"runtime_command_id", "payload", "blocks", "values", "gate_answer"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Errorf("accepted command delivery leaked %q: %s", forbidden, data)
		}
	}

	const injected = `{"command_id":"client-command:opaque","runtime_command_id":"00000000-0000-0000-0000-000000000000"}`
	var decoded sessionwire.HostLinkCommandDelivery
	if err := json.Unmarshal([]byte(injected), &decoded); err == nil {
		t.Fatal("HostLinkCommandDelivery accepted a runtime UUID")
	}
}

func TestHostLinkControlsFailClosed(t *testing.T) {
	t.Parallel()

	const secretField = "raw_runtime_payload_should_never_be_accepted"
	body := `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","host_id":"host-1","host_generation":7,"lease_epoch":3,"runtime_compatibility_id":"runtime-2026-08","idempotency_key":"bind-1","` + secretField + `":{}}`
	var bind sessionwire.HostLinkBindRequest
	err := json.Unmarshal([]byte(body), &bind)
	if err == nil {
		t.Fatal("HostLinkBindRequest accepted an unknown control member")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T (%v), want RequestValidationError", err, err)
	}
	if got, want := validation.Code, sessionwire.RequestValidationCodeUnknownField; got != want {
		t.Errorf("Code = %q, want %q", got, want)
	}
	if validation.Field != "" || strings.Contains(err.Error(), secretField) {
		t.Errorf("unknown HostLink control member leaked into diagnostics: %#v / %v", validation, err)
	}
}

func TestHostLinkRejectsZeroEpochAndInvalidRegistryTimes(t *testing.T) {
	t.Parallel()

	bind := sessionwire.HostLinkBindRequest{
		Version:                sessionwire.CurrentWireVersion,
		TenantID:               "tenant-1",
		SessionID:              "session-1",
		HostID:                 "host-1",
		HostGeneration:         1,
		RuntimeCompatibilityID: "runtime-1",
		IdempotencyKey:         "bind-1",
	}
	if err := bind.Validate(); err == nil {
		t.Fatal("HostLinkBindRequest.Validate() accepted zero lease epoch")
	}

	observation := sessionwire.HostLinkRegistryObservation{
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
		ObservedAt:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		ExpiresAt:              time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
	}
	if err := observation.Validate(); err == nil {
		t.Fatal("HostLinkRegistryObservation.Validate() accepted a non-future expiry")
	}
}

func TestHostLinkRegistryObservationRejectsCold(t *testing.T) {
	t.Parallel()

	observation := sessionwire.HostLinkRegistryObservation{
		Version:                sessionwire.CurrentWireVersion,
		TenantID:               "tenant-1",
		SessionID:              "session-1",
		HostID:                 "host-1",
		HostGeneration:         1,
		AgentID:                "agent-1",
		RuntimeCompatibilityID: "runtime-1",
		Placement:              sessionwire.HostPlacementPooled,
		InternalEndpoint:       "wss://host-1.internal/hostlink",
		Residency:              sessionwire.SessionResidencyCold,
		Accepting:              false,
		LeaseEpoch:             1,
		ObservedAt:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		ExpiresAt:              time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC),
	}
	if err := observation.Validate(); err == nil {
		t.Fatal("HostLinkRegistryObservation.Validate() accepted cold; cold is represented by no fresh observation")
	}

	const coldWire = `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","host_id":"host-1","host_generation":1,"agent_id":"agent-1","runtime_compatibility_id":"runtime-1","placement":"pooled","internal_endpoint":"wss://host-1.internal/hostlink","residency":"cold","accepting":false,"lease_epoch":1,"observed_at":"2026-08-29T12:00:00Z","expires_at":"2026-08-29T12:01:00Z"}`
	var decoded sessionwire.HostLinkRegistryObservation
	if err := json.Unmarshal([]byte(coldWire), &decoded); err == nil {
		t.Fatal("HostLinkRegistryObservation.UnmarshalJSON() accepted cold")
	}
}

func TestHostLinkAdvertisementsRequireSafeRoute(t *testing.T) {
	t.Parallel()

	report := sessionwire.HostLinkCapacityReport{
		Version:                sessionwire.CurrentWireVersion,
		HostID:                 "host-1",
		HostGeneration:         1,
		AgentID:                "agent-1",
		RuntimeCompatibilityID: "runtime-1",
		Placement:              sessionwire.HostPlacementPooled,
		InternalEndpoint:       "wss://host-1.internal/hostlink",
		Accepting:              true,
		AvailableCapacity:      1,
		ObservedAt:             time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC),
		ExpiresAt:              time.Date(2026, 8, 29, 12, 1, 0, 0, time.UTC),
	}
	if err := report.Validate(); err != nil {
		t.Fatalf("HostLinkCapacityReport.Validate() valid route: %v", err)
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal(HostLinkCapacityReport): %v", err)
	}
	var decoded sessionwire.HostLinkCapacityReport
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(HostLinkCapacityReport): %v", err)
	}
	if decoded != report {
		t.Errorf("HostLinkCapacityReport round trip = %#v, want %#v", decoded, report)
	}

	for _, invalid := range []struct {
		name      string
		placement sessionwire.HostPlacement
		endpoint  sessionwire.InternalEndpoint
	}{
		{name: "unknown placement", placement: "elastic", endpoint: report.InternalEndpoint},
		{name: "missing hostname", placement: report.Placement, endpoint: "wss://:443/hostlink"},
		{name: "signed query", placement: report.Placement, endpoint: "wss://host-1.internal/hostlink?sig=top-secret"},
		{name: "userinfo", placement: report.Placement, endpoint: "wss://token:top-secret@host-1.internal/hostlink"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			t.Parallel()
			candidate := report
			candidate.Placement = invalid.placement
			candidate.InternalEndpoint = invalid.endpoint
			err := candidate.Validate()
			if err == nil {
				t.Fatalf("HostLinkCapacityReport.Validate() accepted unsafe route: %#v", candidate)
			}
			if strings.Contains(err.Error(), "top-secret") {
				t.Errorf("unsafe endpoint leaked into diagnostics: %v", err)
			}
		})
	}
}

func TestHostLinkTypedEpochAndRuntimeErrors(t *testing.T) {
	t.Parallel()

	epoch := sessionwire.HostLinkError{
		Code:              sessionwire.HostLinkErrorEpochMismatch,
		CurrentLeaseEpoch: 4,
	}
	if err := epoch.Validate(); err != nil {
		t.Fatalf("epoch mismatch error validation: %v", err)
	}
	runtime := sessionwire.HostLinkError{
		Code:                   sessionwire.HostLinkErrorRuntimeMismatch,
		RuntimeCompatibilityID: "runtime-2026-08",
	}
	if err := runtime.Validate(); err != nil {
		t.Fatalf("runtime mismatch error validation: %v", err)
	}
	for _, value := range []sessionwire.HostLinkError{epoch, runtime} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("Marshal(HostLinkError): %v", err)
		}
		var decoded sessionwire.HostLinkError
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal(HostLinkError): %v", err)
		}
		if decoded != value {
			t.Errorf("HostLinkError round trip = %#v, want %#v", decoded, value)
		}
	}
}

func TestHostLinkErrorRejectsInconsistentDetails(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"code":"epoch_mismatch","current_lease_epoch":0}`,
		`{"code":"not_admitting","current_lease_epoch":0}`,
	} {
		var decoded sessionwire.HostLinkError
		if err := json.Unmarshal([]byte(body), &decoded); err == nil {
			t.Fatalf("HostLinkError.UnmarshalJSON() accepted an explicit zero epoch: %s", body)
		}
	}
	if err := (sessionwire.HostLinkError{
		Code:                   sessionwire.HostLinkErrorNotAdmitting,
		RuntimeCompatibilityID: "runtime-2026-08",
	}).Validate(); err == nil {
		t.Fatal("HostLinkError.Validate() accepted runtime details for a non-runtime mismatch")
	}
}

func TestVersionNegotiationJSONGolden(t *testing.T) {
	t.Parallel()

	offer := sessionwire.VersionNegotiationRequest{SupportedVersions: []sessionwire.WireVersion{sessionwire.CurrentWireVersion}}
	data, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("Marshal(VersionNegotiationRequest): %v", err)
	}
	const wantOffer = `{"supported_versions":[1]}`
	if !bytes.Equal(data, []byte(wantOffer)) {
		t.Errorf("VersionNegotiationRequest JSON = %s, want %s", data, wantOffer)
	}
	result, err := sessionwire.NegotiateVersion(offer)
	if err != nil {
		t.Fatalf("NegotiateVersion(): %v", err)
	}
	resultData, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal(VersionNegotiationResponse): %v", err)
	}
	const wantResult = `{"version":1}`
	if !bytes.Equal(resultData, []byte(wantResult)) {
		t.Errorf("VersionNegotiationResponse JSON = %s, want %s", resultData, wantResult)
	}

	_, err = sessionwire.NegotiateVersion(sessionwire.VersionNegotiationRequest{SupportedVersions: []sessionwire.WireVersion{2}})
	if err == nil {
		t.Fatal("NegotiateVersion() accepted an unsupported version")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("unsupported negotiation error = %T (%v), want RequestValidationError", err, err)
	}
	if got, want := validation.Code, sessionwire.RequestValidationCodeUnsupportedVersion; got != want {
		t.Errorf("unsupported negotiation code = %q, want %q", got, want)
	}
}

func TestHostLinkDrainObservationRoundTrip(t *testing.T) {
	t.Parallel()

	observation := sessionwire.HostLinkDrainObservation{
		HostID:          "host-1",
		HostGeneration:  7,
		DrainGeneration: 9,
		State:           sessionwire.HostLinkDrainStateDraining,
		TenantID:        "tenant-1",
		SessionID:       "session-1",
	}
	data, err := json.Marshal(observation)
	if err != nil {
		t.Fatalf("Marshal(HostLinkDrainObservation): %v", err)
	}
	var decoded sessionwire.HostLinkDrainObservation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(HostLinkDrainObservation): %v", err)
	}
	if decoded.HostID != observation.HostID || decoded.DrainGeneration != observation.DrainGeneration || decoded.State != observation.State {
		t.Errorf("HostLinkDrainObservation round trip = %#v, want %#v", decoded, observation)
	}
}
