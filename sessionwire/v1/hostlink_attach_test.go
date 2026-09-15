package v1_test

import (
	"bytes"
	"encoding/json"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func validHostLinkAttachRequest() sessionwire.HostLinkAttachRequest {
	return sessionwire.HostLinkAttachRequest{
		Version:                sessionwire.CurrentWireVersion,
		TenantID:               "tenant-1",
		SessionID:              "session-1",
		AgentID:                "agent-1",
		RuntimeCompatibilityID: "runtime-2026-08",
		Mode:                   sessionwire.HostLinkAttachModeRestore,
		ActorID:                "factory-placement",
		TraceID:                "trace-1",
		IdempotencyKey:         "attach-1",
	}
}

func TestHostLinkAttachJSONGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value sessionwire.HostLinkAttachRequest
		want  string
	}{
		{
			name:  "restore with trace",
			value: validHostLinkAttachRequest(),
			want:  `{"actor_id":"factory-placement","agent_id":"agent-1","idempotency_key":"attach-1","mode":"restore","runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","trace_id":"trace-1","version":1}`,
		},
		{
			name: "create without trace",
			value: func() sessionwire.HostLinkAttachRequest {
				r := validHostLinkAttachRequest()
				r.Mode = sessionwire.HostLinkAttachModeCreate
				r.TraceID = ""
				return r
			}(),
			want: `{"actor_id":"factory-placement","agent_id":"agent-1","idempotency_key":"attach-1","mode":"create","runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","version":1}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Marshal(HostLinkAttachRequest): %v", err)
			}
			if !bytes.Equal(data, []byte(tt.want)) {
				t.Errorf("HostLinkAttachRequest JSON = %s, want %s", data, tt.want)
			}
			var decoded sessionwire.HostLinkAttachRequest
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal(HostLinkAttachRequest): %v", err)
			}
			if decoded != tt.value {
				t.Errorf("HostLinkAttachRequest round trip = %#v, want %#v", decoded, tt.value)
			}
		})
	}
}

func TestHostLinkAttachRequestValidateNamesEachField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*sessionwire.HostLinkAttachRequest)
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{"version", func(r *sessionwire.HostLinkAttachRequest) { r.Version = 0 }, sessionwire.RequestValidationCodeUnsupportedVersion, "version"},
		{"future version", func(r *sessionwire.HostLinkAttachRequest) { r.Version = 2 }, sessionwire.RequestValidationCodeUnsupportedVersion, "version"},
		{"tenant_id", func(r *sessionwire.HostLinkAttachRequest) { r.TenantID = "" }, sessionwire.RequestValidationCodeInvalidField, "tenant_id"},
		{"session_id", func(r *sessionwire.HostLinkAttachRequest) { r.SessionID = "" }, sessionwire.RequestValidationCodeInvalidField, "session_id"},
		{"agent_id", func(r *sessionwire.HostLinkAttachRequest) { r.AgentID = "" }, sessionwire.RequestValidationCodeInvalidField, "agent_id"},
		{"runtime_compatibility_id", func(r *sessionwire.HostLinkAttachRequest) { r.RuntimeCompatibilityID = "" }, sessionwire.RequestValidationCodeInvalidField, "runtime_compatibility_id"},
		{"mode empty", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"mode unknown", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "resume" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"mode case", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "Create" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"actor_id", func(r *sessionwire.HostLinkAttachRequest) { r.ActorID = "" }, sessionwire.RequestValidationCodeInvalidField, "actor_id"},
		{"trace_id invalid utf8", func(r *sessionwire.HostLinkAttachRequest) { r.TraceID = "\xff" }, sessionwire.RequestValidationCodeInvalidField, "trace_id"},
		{"idempotency_key", func(r *sessionwire.HostLinkAttachRequest) { r.IdempotencyKey = "" }, sessionwire.RequestValidationCodeInvalidField, "idempotency_key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := validHostLinkAttachRequest()
			tt.mutate(&request)
			assertValidationError(t, request.Validate(), tt.wantCode, tt.wantField)
			if _, err := json.Marshal(request); err == nil {
				t.Errorf("Marshal accepted a request Validate refuses")
			}
		})
	}

	valid := validHostLinkAttachRequest()
	if err := valid.Validate(); err != nil {
		t.Errorf("valid attach request refused: %v", err)
	}
	valid.TraceID = ""
	if err := valid.Validate(); err != nil {
		t.Errorf("attach request without the optional trace_id refused: %v", err)
	}
}

func TestHostLinkAttachRequestDecodeFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{
			name:      "missing mode",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","actor_id":"svc","idempotency_key":"attach-1"}`,
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "mode",
		},
		{
			name:      "unknown mode",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"attach","actor_id":"svc","idempotency_key":"attach-1"}`,
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "mode",
		},
		{
			name:      "missing actor",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"create","idempotency_key":"attach-1"}`,
			wantCode:  sessionwire.RequestValidationCodeMissingField,
			wantField: "actor_id",
		},
		{
			name:      "empty trace",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"create","actor_id":"svc","trace_id":"","idempotency_key":"attach-1"}`,
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "trace_id",
		},
		{
			name:      "null trace",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"create","actor_id":"svc","trace_id":null,"idempotency_key":"attach-1"}`,
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "trace_id",
		},
		{
			name:      "lease epoch is not an attach member",
			body:      `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"create","actor_id":"svc","idempotency_key":"attach-1","lease_epoch":3}`,
			wantCode:  sessionwire.RequestValidationCodeUnknownField,
			wantField: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var decoded sessionwire.HostLinkAttachRequest
			err := json.Unmarshal([]byte(tt.body), &decoded)
			assertValidationError(t, err, tt.wantCode, tt.wantField)
			if decoded != (sessionwire.HostLinkAttachRequest{}) {
				t.Errorf("refused decode mutated the receiver: %#v", decoded)
			}
		})
	}
}

func TestHostLinkAttachErrorCodesNeedNoVocabularyBump(t *testing.T) {
	t.Parallel()

	// An attach refusal reuses HostLinkError unchanged. Each code an attach may
	// answer must already be publishable with its existing detail rules.
	for _, refusal := range []sessionwire.HostLinkError{
		{Code: sessionwire.HostLinkErrorEpochMismatch, CurrentLeaseEpoch: 4},
		{Code: sessionwire.HostLinkErrorRuntimeMismatch, RuntimeCompatibilityID: "runtime-2026-09"},
		{Code: sessionwire.HostLinkErrorNoCapacity},
		{Code: sessionwire.HostLinkErrorNotAdmitting},
		{Code: sessionwire.HostLinkErrorRuntimeUnavailable},
	} {
		if err := refusal.Validate(); err != nil {
			t.Errorf("attach refusal %q is not publishable: %v", refusal.Code, err)
		}
	}
}

// The golden channels below were produced by running Host's own
// hostlink.ChannelFor (host internal/realtime/hostlink, pinned to core v0.7.0)
// over these inputs, not derived by hand. They cover the unpadded tails
// (1, 2 and 3 byte groups), the URL-safe '-' and '_' alphabet, a '.' inside an
// identifier, and multi-byte UTF-8.
func TestHostLinkChannelMatchesHostGoldens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tenant  sessionwire.TenantID
		session sessionwire.SessionID
		want    string
	}{
		{"tenant-1", "session-1", "hostlink.v1.dGVuYW50LTE.c2Vzc2lvbi0x"},
		{"a", "b", "hostlink.v1.YQ.Yg"},
		{"ab", "abc", "hostlink.v1.YWI.YWJj"},
		{"a.b", "c", "hostlink.v1.YS5i.Yw"},
		{"a", "b.c", "hostlink.v1.YQ.Yi5j"},
		{"~~~", "???", "hostlink.v1.fn5-.Pz8_"},
		{"tenant-é", "sess/ü+", "hostlink.v1.dGVuYW50LcOp.c2Vzcy_DvCs"},
	}
	for _, tt := range tests {
		if got := sessionwire.HostLinkChannel(tt.tenant, tt.session); got != tt.want {
			t.Errorf("HostLinkChannel(%q, %q) = %q, want %q", tt.tenant, tt.session, got, tt.want)
		}
	}
}

func TestHostLinkChannelIsInjectiveAcrossSeparators(t *testing.T) {
	t.Parallel()

	if sessionwire.HostLinkChannel("a.b", "c") == sessionwire.HostLinkChannel("a", "b.c") {
		t.Fatal("two distinct session keys minted one channel")
	}
}

func TestHostLinkFramingConstantsMatchHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"channel prefix", sessionwire.HostLinkChannelPrefix, "hostlink.v1."},
		{"bind", sessionwire.HostLinkMethodBind, "hostlink.bind"},
		{"unbind", sessionwire.HostLinkMethodUnbind, "hostlink.unbind"},
		{"attach", sessionwire.HostLinkMethodAttach, "hostlink.attach"},
		{"drain", sessionwire.HostLinkMethodDrain, "hostlink.drain"},
		{"drain status", sessionwire.HostLinkMethodDrainStatus, "hostlink.drain_status"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
	// Host tells a reserved method from a channel by exact match, so no method
	// may be spelled as a channel.
	for _, method := range []string{
		sessionwire.HostLinkMethodBind, sessionwire.HostLinkMethodUnbind, sessionwire.HostLinkMethodAttach,
		sessionwire.HostLinkMethodDrain, sessionwire.HostLinkMethodDrainStatus,
	} {
		if len(method) >= len(sessionwire.HostLinkChannelPrefix) && method[:len(sessionwire.HostLinkChannelPrefix)] == sessionwire.HostLinkChannelPrefix {
			t.Errorf("method %q begins with the channel prefix", method)
		}
	}
}
