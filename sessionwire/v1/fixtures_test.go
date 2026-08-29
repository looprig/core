package v1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

type v1FixtureDecoder func([]byte) ([]byte, error)

func TestV1FixtureGoldensRoundTripThroughCoreRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		stem   string
		decode v1FixtureDecoder
	}{
		{"create_request", v1GoldenRoundTrip[sessionwire.CreateRequest]},
		{"input_request", v1GoldenRoundTrip[sessionwire.InputRequest]},
		{"interrupt_request", v1GoldenRoundTrip[sessionwire.InterruptRequest]},
		{"restore_request", v1GoldenRoundTrip[sessionwire.RestoreRequest]},
		{"gate_response_request", v1GoldenRoundTrip[sessionwire.GateResponseRequest]},
		{"command_status", v1GoldenRoundTrip[sessionwire.CommandStatus]},
		{"error_envelope", v1GoldenRoundTrip[sessionwire.ErrorEnvelope]},
		{"agent_capability_summary", v1GoldenRoundTrip[sessionwire.AgentCapabilitySummary]},
		{"department_capability_summary", v1GoldenRoundTrip[sessionwire.DepartmentCapabilitySummary]},
		{"session_summary", v1GoldenRoundTrip[sessionwire.SessionSummary]},
		{"session_status", v1GoldenRoundTrip[sessionwire.SessionStatus]},
		{"recent_session_page", v1GoldenRoundTrip[sessionwire.SessionPage]},
		{"journal_event", v1GoldenRoundTrip[sessionwire.JournalEvent]},
		{"public_journal_page", v1GoldenRoundTrip[sessionwire.JournalPage]},
		{"gate_projection", v1GoldenRoundTrip[sessionwire.GateProjection]},
		{"public_gate_page", v1GoldenRoundTrip[sessionwire.GatePage]},
		{"object_reference", v1GoldenRoundTrip[sessionwire.ObjectReference]},
		{"object_metadata", v1GoldenRoundTrip[sessionwire.ObjectMetadata]},
		{"enduring_publication", v1GoldenRoundTrip[sessionwire.EnduringPublication]},
		{"ephemeral_publication", v1GoldenRoundTrip[sessionwire.EphemeralPublication]},
		{"journal_tip", v1GoldenRoundTrip[sessionwire.JournalTip]},
		{"session_reset", v1GoldenRoundTrip[sessionwire.SessionReset]},
		{"hostlink_bind_request", v1GoldenRoundTrip[sessionwire.HostLinkBindRequest]},
		{"hostlink_unbind_request", v1GoldenRoundTrip[sessionwire.HostLinkUnbindRequest]},
		{"hostlink_command_delivery", v1GoldenRoundTrip[sessionwire.HostLinkCommandDelivery]},
		{"hostlink_capacity_report", v1GoldenRoundTrip[sessionwire.HostLinkCapacityReport]},
		{"hostlink_registry_observation", v1GoldenRoundTrip[sessionwire.HostLinkRegistryObservation]},
		{"hostlink_drain_request", v1GoldenRoundTrip[sessionwire.HostLinkDrainRequest]},
		{"hostlink_drain_observation", v1GoldenRoundTrip[sessionwire.HostLinkDrainObservation]},
		{"hostlink_error_epoch_mismatch", v1GoldenRoundTrip[sessionwire.HostLinkError]},
		{"hostlink_error_runtime_mismatch", v1GoldenRoundTrip[sessionwire.HostLinkError]},
		{"version_negotiation_request", v1GoldenRoundTrip[sessionwire.VersionNegotiationRequest]},
		{"version_negotiation_response", v1GoldenRoundTrip[sessionwire.VersionNegotiationResponse]},
	}
	decoders := make(map[string]struct{}, len(tests))
	for _, tt := range tests {
		if _, duplicate := decoders[tt.stem]; duplicate {
			t.Fatalf("duplicate fixture semantic decoder for %q", tt.stem)
		}
		decoders[tt.stem] = struct{}{}
	}
	for _, path := range v1FixtureFiles(t) {
		stem := strings.TrimSuffix(filepath.Base(path), ".json")
		if _, ok := decoders[stem]; !ok {
			t.Errorf("fixture %q has no Core semantic decoder", stem)
		}
		delete(decoders, stem)
	}
	for stem := range decoders {
		t.Errorf("Core semantic decoder %q has no fixture", stem)
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.stem, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(v1FixtureDir, tt.stem+".json")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			got, err := tt.decode(want)
			if err != nil {
				t.Fatalf("decode fixture through Core record: %v", err)
			}
			// The committed JSON documents use the ordinary POSIX final newline;
			// the Core encoder deliberately emits only the canonical JSON value.
			got = append(got, '\n')
			if !bytes.Equal(got, want) {
				t.Errorf("fixture no longer equals its canonical Core encoding\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

func TestV1GoldenFixturesKeepPublicBodiesCanonicalAndRedacted(t *testing.T) {
	t.Parallel()

	for _, stem := range []string{
		"journal_event",
		"public_journal_page",
		"enduring_publication",
		"ephemeral_publication",
	} {
		stem := stem
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join(v1FixtureDir, stem+".json"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`, `\u2028`, `\u2029`} {
				if !bytes.Contains(raw, []byte(escape)) {
					t.Errorf("fixture does not exercise canonical public JSON escape %q: %s", escape, raw)
				}
			}
			for _, forbidden := range []string{"raw_runtime_payload", "signed_url", "secret", "gate_answer"} {
				if bytes.Contains(raw, []byte(forbidden)) {
					t.Errorf("fixture leaks redacted member %q: %s", forbidden, raw)
				}
			}
		})
	}

	for _, stem := range []string{"gate_projection", "public_gate_page", "object_reference", "object_metadata", "hostlink_command_delivery"} {
		stem := stem
		t.Run(stem, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join(v1FixtureDir, stem+".json"))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			for _, forbidden := range []string{"raw_runtime_payload", "runtime_command_id", "signed_url", "secret", "gate_answer"} {
				if strings.Contains(string(raw), forbidden) {
					t.Errorf("fixture leaks redacted member %q: %s", forbidden, raw)
				}
			}
		})
	}
}

func v1GoldenRoundTrip[T any](raw []byte) ([]byte, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	// Assert on the addressable *T, not T: a Validate that later moves to a
	// pointer receiver would otherwise fail the value-type assertion and skip
	// the semantic check across every fixture with a green test. Every V1 record
	// must be validatable, so a missing method is a failure rather than a skip.
	validator, ok := any(&value).(interface{ Validate() error })
	if !ok {
		return nil, fmt.Errorf("%T has no Validate method; the fixture's semantic check would be vacuous", value)
	}
	if err := validator.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
