package v1_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func validHostLinkAttachRequest() sessionwire.HostLinkAttachRequest {
	return sessionwire.HostLinkAttachRequest{
		Version:                sessionwire.CurrentWireVersion,
		TenantID:               "tenant-1",
		SessionID:              "session-1",
		HostID:                 "host-1",
		HostGeneration:         7,
		AgentID:                "agent-1",
		RuntimeCompatibilityID: "runtime-2026-08",
		Mode:                   sessionwire.HostLinkAttachModeRestore,
		ActorID:                "factory-placement",
		TraceID:                "trace-1",
		IdempotencyKey:         "attach-1",
	}
}

// validHostLinkAttachWire is validHostLinkAttachRequest as decoded JSON members,
// so a wire row can replace or drop exactly one member.
func validHostLinkAttachWire() map[string]any {
	return map[string]any{
		"version":                  1,
		"tenant_id":                "tenant-1",
		"session_id":               "session-1",
		"host_id":                  "host-1",
		"host_generation":          7,
		"agent_id":                 "agent-1",
		"runtime_compatibility_id": "runtime-2026-08",
		"mode":                     "restore",
		"actor_id":                 "factory-placement",
		"trace_id":                 "trace-1",
		"idempotency_key":          "attach-1",
	}
}

func hostLinkAttachWireWith(t *testing.T, name string, value any) []byte {
	t.Helper()
	members := validHostLinkAttachWire()
	members[name] = value
	data, err := json.Marshal(members)
	if err != nil {
		t.Fatalf("marshal wire body: %v", err)
	}
	return data
}

func hostLinkAttachWireWithout(t *testing.T, name string) []byte {
	t.Helper()
	members := validHostLinkAttachWire()
	delete(members, name)
	data, err := json.Marshal(members)
	if err != nil {
		t.Fatalf("marshal wire body: %v", err)
	}
	return data
}

// hostLinkAttachBoundedFields are the attach members whose identity bound is
// Core's MaxIDBytes.
var hostLinkAttachBoundedFields = []string{
	"tenant_id", "session_id", "host_id", "agent_id", "runtime_compatibility_id", "actor_id", "trace_id", "idempotency_key",
}

func setHostLinkAttachField(r *sessionwire.HostLinkAttachRequest, name, value string) {
	switch name {
	case "tenant_id":
		r.TenantID = sessionwire.TenantID(value)
	case "session_id":
		r.SessionID = sessionwire.SessionID(value)
	case "host_id":
		r.HostID = sessionwire.HostID(value)
	case "agent_id":
		r.AgentID = sessionwire.AgentID(value)
	case "runtime_compatibility_id":
		r.RuntimeCompatibilityID = value
	case "actor_id":
		r.ActorID = value
	case "trace_id":
		r.TraceID = value
	case "idempotency_key":
		r.IdempotencyKey = value
	default:
		panic("unknown bounded attach field " + name)
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
			want:  `{"actor_id":"factory-placement","agent_id":"agent-1","host_generation":7,"host_id":"host-1","idempotency_key":"attach-1","mode":"restore","runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","trace_id":"trace-1","version":1}`,
		},
		{
			name: "create without trace",
			value: func() sessionwire.HostLinkAttachRequest {
				r := validHostLinkAttachRequest()
				r.Mode = sessionwire.HostLinkAttachModeCreate
				r.TraceID = ""
				return r
			}(),
			want: `{"actor_id":"factory-placement","agent_id":"agent-1","host_generation":7,"host_id":"host-1","idempotency_key":"attach-1","mode":"create","runtime_compatibility_id":"runtime-2026-08","session_id":"session-1","tenant_id":"tenant-1","version":1}`,
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

// Every row mutates exactly one member of an otherwise valid request, so it
// passes every guard before the one it names and fails at that guard.
func TestHostLinkAttachRequestValidateNamesEachField(t *testing.T) {
	t.Parallel()

	type row struct {
		name      string
		mutate    func(*sessionwire.HostLinkAttachRequest)
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}
	tests := []row{
		{"version", func(r *sessionwire.HostLinkAttachRequest) { r.Version = 0 }, sessionwire.RequestValidationCodeUnsupportedVersion, "version"},
		{"future version", func(r *sessionwire.HostLinkAttachRequest) { r.Version = 2 }, sessionwire.RequestValidationCodeUnsupportedVersion, "version"},
		{"tenant_id", func(r *sessionwire.HostLinkAttachRequest) { r.TenantID = "" }, sessionwire.RequestValidationCodeInvalidField, "tenant_id"},
		{"session_id", func(r *sessionwire.HostLinkAttachRequest) { r.SessionID = "" }, sessionwire.RequestValidationCodeInvalidField, "session_id"},
		{"host_id", func(r *sessionwire.HostLinkAttachRequest) { r.HostID = "" }, sessionwire.RequestValidationCodeInvalidField, "host_id"},
		{"host_generation", func(r *sessionwire.HostLinkAttachRequest) { r.HostGeneration = 0 }, sessionwire.RequestValidationCodeInvalidField, "host_generation"},
		{"agent_id", func(r *sessionwire.HostLinkAttachRequest) { r.AgentID = "" }, sessionwire.RequestValidationCodeInvalidField, "agent_id"},
		{"runtime_compatibility_id", func(r *sessionwire.HostLinkAttachRequest) { r.RuntimeCompatibilityID = "" }, sessionwire.RequestValidationCodeInvalidField, "runtime_compatibility_id"},
		{"mode empty", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"mode unknown", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "resume" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"mode case", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "Create" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"mode upper case", func(r *sessionwire.HostLinkAttachRequest) { r.Mode = "RESTORE" }, sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"actor_id", func(r *sessionwire.HostLinkAttachRequest) { r.ActorID = "" }, sessionwire.RequestValidationCodeInvalidField, "actor_id"},
		{"trace_id invalid utf8", func(r *sessionwire.HostLinkAttachRequest) { r.TraceID = "\xff" }, sessionwire.RequestValidationCodeInvalidField, "trace_id"},
		{"idempotency_key", func(r *sessionwire.HostLinkAttachRequest) { r.IdempotencyKey = "" }, sessionwire.RequestValidationCodeInvalidField, "idempotency_key"},
	}
	oversize := strings.Repeat("x", sessionwire.MaxIDBytes+1)
	for _, field := range hostLinkAttachBoundedFields {
		field := field
		tests = append(tests, row{
			name:      field + " over MaxIDBytes",
			mutate:    func(r *sessionwire.HostLinkAttachRequest) { setHostLinkAttachField(r, field, oversize) },
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: field,
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			request := validHostLinkAttachRequest()
			tt.mutate(&request)
			assertValidationError(t, request.Validate(), tt.wantCode, tt.wantField)
			_, err := json.Marshal(request)
			assertValidationError(t, err, tt.wantCode, tt.wantField)
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

func TestHostLinkAttachRequestAcceptsMaxLengthIdentities(t *testing.T) {
	t.Parallel()

	request := validHostLinkAttachRequest()
	for _, field := range hostLinkAttachBoundedFields {
		setHostLinkAttachField(&request, field, strings.Repeat(field[:1], sessionwire.MaxIDBytes))
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("Validate refused identities at exactly MaxIDBytes: %v", err)
	}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("Marshal refused identities at exactly MaxIDBytes: %v", err)
	}
	var decoded sessionwire.HostLinkAttachRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal refused identities at exactly MaxIDBytes: %v", err)
	}
	if decoded != request {
		t.Errorf("max-length round trip = %#v, want %#v", decoded, request)
	}
}

func TestHostLinkAttachRequestDecodeFailsClosed(t *testing.T) {
	t.Parallel()

	type row struct {
		name      string
		body      []byte
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}
	tests := []row{
		{"missing mode", hostLinkAttachWireWithout(t, "mode"), sessionwire.RequestValidationCodeMissingField, "mode"},
		{"unknown mode", hostLinkAttachWireWith(t, "mode", "attach"), sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"empty mode", hostLinkAttachWireWith(t, "mode", ""), sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"capitalised mode", hostLinkAttachWireWith(t, "mode", "Create"), sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"upper-case mode", hostLinkAttachWireWith(t, "mode", "RESTORE"), sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"numeric mode", hostLinkAttachWireWith(t, "mode", 1), sessionwire.RequestValidationCodeInvalidField, "mode"},
		{"null mode", hostLinkAttachWireWith(t, "mode", nil), sessionwire.RequestValidationCodeMissingField, "mode"},
		{"missing host_id", hostLinkAttachWireWithout(t, "host_id"), sessionwire.RequestValidationCodeMissingField, "host_id"},
		{"missing host_generation", hostLinkAttachWireWithout(t, "host_generation"), sessionwire.RequestValidationCodeMissingField, "host_generation"},
		{"zero host_generation", hostLinkAttachWireWith(t, "host_generation", 0), sessionwire.RequestValidationCodeInvalidField, "host_generation"},
		{"missing actor", hostLinkAttachWireWithout(t, "actor_id"), sessionwire.RequestValidationCodeMissingField, "actor_id"},
		{"empty trace", hostLinkAttachWireWith(t, "trace_id", ""), sessionwire.RequestValidationCodeInvalidField, "trace_id"},
		{"null trace", hostLinkAttachWireWith(t, "trace_id", nil), sessionwire.RequestValidationCodeInvalidField, "trace_id"},
		{"lease epoch is not an attach member", hostLinkAttachWireWith(t, "lease_epoch", 3), sessionwire.RequestValidationCodeUnknownField, ""},
		{
			name:      "duplicate mode",
			body:      []byte(`{"version":1,"tenant_id":"tenant-1","session_id":"session-1","host_id":"host-1","host_generation":7,"agent_id":"agent-1","runtime_compatibility_id":"runtime-1","mode":"create","mode":"restore","actor_id":"svc","idempotency_key":"attach-1"}`),
			wantCode:  sessionwire.RequestValidationCodeDuplicateField,
			wantField: "mode",
		},
	}
	oversize := strings.Repeat("x", sessionwire.MaxIDBytes+1)
	for _, field := range hostLinkAttachBoundedFields {
		tests = append(tests, row{
			name:      field + " over MaxIDBytes",
			body:      hostLinkAttachWireWith(t, field, oversize),
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: field,
		})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var decoded sessionwire.HostLinkAttachRequest
			err := json.Unmarshal(tt.body, &decoded)
			assertValidationError(t, err, tt.wantCode, tt.wantField)
			if decoded != (sessionwire.HostLinkAttachRequest{}) {
				t.Errorf("refused decode mutated the receiver: %#v", decoded)
			}
		})
	}
}

// The bind sibling had no duplicate-member coverage either; two parsers that
// disagree on which copy of a control member wins is the smuggling primitive
// the strict decoders exist to deny.
func TestHostLinkBindRequestRejectsDuplicateMembers(t *testing.T) {
	t.Parallel()

	const body = `{"version":1,"tenant_id":"tenant-1","session_id":"session-1","host_id":"host-1","host_generation":7,"lease_epoch":3,"lease_epoch":4,"runtime_compatibility_id":"runtime-1","idempotency_key":"bind-1"}`
	var decoded sessionwire.HostLinkBindRequest
	assertValidationError(t, json.Unmarshal([]byte(body), &decoded), sessionwire.RequestValidationCodeDuplicateField, "lease_epoch")
}

// TestHostLinkAttachSchemaAgreesWithDecoder holds the published schema's member
// set, required set and mode enum to what Core's decoder actually enforces, so
// a schema edit that a generated consumer would trust cannot drift silently.
func TestHostLinkAttachSchemaAgreesWithDecoder(t *testing.T) {
	t.Parallel()

	const stem = "hostlink_attach_request"
	rawSchema, err := os.ReadFile(filepath.Join(v1SchemaDir, stem+".schema.json"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	fixture := readV1JSONObject(t, filepath.Join(v1FixtureDir, stem+".json"))

	var declared, present []string
	for name := range schema.Properties {
		declared = append(declared, name)
	}
	for name := range fixture {
		present = append(present, name)
	}
	slices.Sort(declared)
	slices.Sort(present)
	if !slices.Equal(declared, present) {
		t.Fatalf("schema properties %v != canonical fixture members %v; the check below needs every member present", declared, present)
	}

	for _, name := range declared {
		members := make(map[string]json.RawMessage, len(fixture))
		for k, v := range fixture {
			if k != name {
				members[k] = v
			}
		}
		body, err := json.Marshal(members)
		if err != nil {
			t.Fatalf("marshal body without %s: %v", name, err)
		}
		var decoded sessionwire.HostLinkAttachRequest
		decodeErr := json.Unmarshal(body, &decoded)
		if slices.Contains(schema.Required, name) {
			if decodeErr == nil {
				t.Errorf("schema requires %q but the decoder accepts a body without it", name)
				continue
			}
			assertValidationError(t, decodeErr, sessionwire.RequestValidationCodeMissingField, name)
		} else if decodeErr != nil {
			t.Errorf("schema makes %q optional but the decoder refuses a body without it: %v", name, decodeErr)
		}
	}

	var mode struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(schema.Properties["mode"], &mode); err != nil {
		t.Fatalf("decode mode schema: %v", err)
	}
	enum := slices.Clone(mode.Enum)
	slices.Sort(enum)
	want := []string{string(sessionwire.HostLinkAttachModeCreate), string(sessionwire.HostLinkAttachModeRestore)}
	if !slices.Equal(enum, want) {
		t.Errorf("schema mode enum = %v, want exactly Core's modes %v", enum, want)
	}
	for _, value := range mode.Enum {
		members := make(map[string]json.RawMessage, len(fixture))
		for k, v := range fixture {
			members[k] = v
		}
		members["mode"], _ = json.Marshal(value)
		body, _ := json.Marshal(members)
		var decoded sessionwire.HostLinkAttachRequest
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("schema admits mode %q but the decoder refuses it: %v", value, err)
		}
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
		if strings.HasPrefix(method, sessionwire.HostLinkChannelPrefix) {
			t.Errorf("method %q begins with the channel prefix", method)
		}
	}
}
