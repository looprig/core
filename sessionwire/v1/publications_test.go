package v1_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestSessionResetJSONGolden(t *testing.T) {
	t.Parallel()

	reset := sessionwire.SessionReset{
		TenantID:       "tenant-1",
		SessionID:      "session-1",
		LastContiguous: 3,
		JournalTip:     5,
	}
	data, err := json.Marshal(reset)
	if err != nil {
		t.Fatalf("Marshal(SessionReset): %v", err)
	}
	const want = `{"journal_tip":5,"last_contiguous":3,"session_id":"session-1","tenant_id":"tenant-1","type":"session.reset"}`
	if !bytes.Equal(data, []byte(want)) {
		t.Errorf("SessionReset JSON = %s, want %s", data, want)
	}

	var decoded sessionwire.SessionReset
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(SessionReset): %v", err)
	}
	if decoded != reset {
		t.Errorf("SessionReset round trip = %#v, want %#v", decoded, reset)
	}
}

func TestJournalTipJSONGolden(t *testing.T) {
	t.Parallel()

	hint := sessionwire.JournalTip{
		TenantID:  "tenant-1",
		SessionID: "session-1",
		Tip:       5,
	}
	data, err := json.Marshal(hint)
	if err != nil {
		t.Fatalf("Marshal(JournalTip): %v", err)
	}
	const want = `{"journal_tip":5,"session_id":"session-1","tenant_id":"tenant-1","type":"journal_tip"}`
	if !bytes.Equal(data, []byte(want)) {
		t.Errorf("JournalTip JSON = %s, want %s", data, want)
	}

	var decoded sessionwire.JournalTip
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(JournalTip): %v", err)
	}
	if decoded != hint {
		t.Errorf("JournalTip round trip = %#v, want %#v", decoded, hint)
	}
}

func TestEnduringPublicationRequiresSequence(t *testing.T) {
	t.Parallel()

	publication := sessionwire.EnduringPublication{
		TenantID:       "tenant-1",
		SessionID:      "session-1",
		EventID:        "event-1",
		CoveredThrough: 1,
		Body:           json.RawMessage(`{"type":"public"}`),
	}
	if err := publication.Validate(); err == nil {
		t.Fatal("EnduringPublication.Validate() accepted a zero journal sequence")
	}

	publication.JournalSeq = 1
	publication.CoveredThrough = 2
	if err := publication.Validate(); err == nil {
		t.Fatal("EnduringPublication.Validate() accepted a watermark beyond its committed sequence")
	}

	publication.CoveredThrough = 1
	if err := publication.Validate(); err != nil {
		t.Fatalf("EnduringPublication.Validate() valid publication: %v", err)
	}
}

func TestEnduringPublicationRelaysCanonicalBodyBytes(t *testing.T) {
	t.Parallel()

	// Canonical event bytes use JSON escape forms accepted by the standard
	// encoder, so a transport marshal cannot rewrite an otherwise equivalent
	// literal <, >, or & while it encloses this body in its own JSON object.
	body := json.RawMessage(`{"type":"turn.completed","payload":{"z":3,"a":[true,false]},"html":"\u003ctag\u003e\u0026","event_note":"canonical-order"}`)
	publication := sessionwire.EnduringPublication{
		TenantID:       "tenant-1",
		SessionID:      "session-1",
		EventID:        "event-5",
		JournalSeq:     5,
		CoveredThrough: 5,
		Body:           body,
	}
	first, err := json.Marshal(publication)
	if err != nil {
		t.Fatalf("Marshal(EnduringPublication): %v", err)
	}
	if !bytes.Contains(first, body) {
		t.Fatalf("initial publication JSON changed canonical body bytes: %s", first)
	}
	var relayed sessionwire.EnduringPublication
	if err := json.Unmarshal(first, &relayed); err != nil {
		t.Fatalf("Unmarshal(EnduringPublication): %v", err)
	}
	if !bytes.Equal(relayed.Body, body) {
		t.Errorf("decoded body = %s, want byte-identical %s", relayed.Body, body)
	}
	second, err := json.Marshal(relayed)
	if err != nil {
		t.Fatalf("Marshal(relayed EnduringPublication): %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(second, &fields); err != nil {
		t.Fatalf("Unmarshal relayed publication JSON: %v", err)
	}
	if !bytes.Equal(fields["body"], body) {
		t.Errorf("relayed body = %s, want byte-identical %s", fields["body"], body)
	}
}

func TestEnduringPublicationRejectsNonTransportCanonicalBody(t *testing.T) {
	t.Parallel()

	publication := sessionwire.EnduringPublication{
		TenantID:       "tenant-1",
		SessionID:      "session-1",
		EventID:        "event-5",
		JournalSeq:     5,
		CoveredThrough: 5,
		Body:           json.RawMessage(`{"html":"<tag>&"}`),
	}
	if err := publication.Validate(); err == nil {
		t.Fatal("EnduringPublication.Validate() accepted a body encoding/json would rewrite")
	}

	const wire = `{"type":"enduring_publication","tenant_id":"tenant-1","session_id":"session-1","event_id":"event-5","journal_seq":5,"covered_through":5,"body":{"html":"<tag>&"}}`
	var decoded sessionwire.EnduringPublication
	if err := json.Unmarshal([]byte(wire), &decoded); err == nil {
		t.Fatal("EnduringPublication.UnmarshalJSON() accepted a body encoding/json would rewrite")
	}
}

func TestEphemeralPublicationRejectsDurableSequencePromise(t *testing.T) {
	t.Parallel()

	const body = `{"type":"ephemeral_publication","tenant_id":"tenant-1","session_id":"session-1","body":{"kind":"token_delta"},"journal_seq":5}`
	var publication sessionwire.EphemeralPublication
	if err := json.Unmarshal([]byte(body), &publication); err == nil {
		t.Fatal("EphemeralPublication accepted a durable journal sequence")
	}
}

func TestPublicationAndRepairRecordsPreserveSafeResponseExtensions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		roundTrip func([]byte) ([]byte, error)
	}{
		{
			name: "enduring publication",
			body: `{"type":"enduring_publication","tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"type":"public"},"future":{"safe":true}}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var value sessionwire.EnduringPublication
				if err := json.Unmarshal(data, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
		{
			name: "journal tip",
			body: `{"type":"journal_tip","tenant_id":"tenant-1","session_id":"session-1","journal_tip":1,"future":{"safe":true}}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var value sessionwire.JournalTip
				if err := json.Unmarshal(data, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
		{
			name: "session reset",
			body: `{"type":"session.reset","tenant_id":"tenant-1","session_id":"session-1","last_contiguous":1,"journal_tip":2,"future":{"safe":true}}`,
			roundTrip: func(data []byte) ([]byte, error) {
				var value sessionwire.SessionReset
				if err := json.Unmarshal(data, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := tt.roundTrip([]byte(tt.body))
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			if !bytes.Contains(data, []byte(`"future":{"safe":true}`)) {
				t.Errorf("round trip dropped additive response field: %s", data)
			}
		})
	}
}

func TestSessionResetRejectsTipBeforeLastContiguous(t *testing.T) {
	t.Parallel()

	reset := sessionwire.SessionReset{
		TenantID:       "tenant-1",
		SessionID:      "session-1",
		LastContiguous: 4,
		JournalTip:     3,
	}
	if err := reset.Validate(); err == nil {
		t.Fatal("SessionReset.Validate() accepted a tip before last contiguous sequence")
	}
}

func TestEnduringPublicationRejectsMalformedPublicBody(t *testing.T) {
	t.Parallel()

	body := append([]byte(`{"type":"enduring_publication","tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"text":"`), 0xff)
	body = append(body, []byte(`"}}`)...)
	var publication sessionwire.EnduringPublication
	err := json.Unmarshal(body, &publication)
	if err == nil {
		t.Fatal("EnduringPublication accepted invalid UTF-8 in public body")
	}
	var validation *sessionwire.RequestValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("error = %T (%v), want RequestValidationError", err, err)
	}
}

// TestSessionChannelRecordsCarryStableTypeDiscriminator pins the wire
// discriminator. Specification 8.1 puts every live session record on one
// channel, `session:{tid}:{sid}`, and 17.3/21 name the `session.reset` repair
// control and the repeatable `journal_tip` hint. Without an explicit member a
// subscriber could only guess a record's kind from which optional members
// happen to be present.
func TestSessionChannelRecordsCarryStableTypeDiscriminator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		record any
		want   sessionwire.SessionRecordType
	}{
		{
			name: "enduring publication",
			record: sessionwire.EnduringPublication{
				TenantID: "tenant-1", SessionID: "session-1", EventID: "event-1",
				JournalSeq: 1, CoveredThrough: 1, Body: json.RawMessage(`{"type":"public"}`),
			},
			want: sessionwire.SessionRecordTypeEnduringPublication,
		},
		{
			name: "ephemeral publication",
			record: sessionwire.EphemeralPublication{
				TenantID: "tenant-1", SessionID: "session-1", Body: json.RawMessage(`{"type":"token_delta"}`),
			},
			want: sessionwire.SessionRecordTypeEphemeralPublication,
		},
		{
			name:   "journal tip",
			record: sessionwire.JournalTip{TenantID: "tenant-1", SessionID: "session-1", Tip: 5},
			want:   sessionwire.SessionRecordTypeJournalTip,
		},
		{
			name:   "session reset",
			record: sessionwire.SessionReset{TenantID: "tenant-1", SessionID: "session-1", LastContiguous: 3, JournalTip: 5},
			want:   sessionwire.SessionRecordTypeSessionReset,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.record)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got, err := sessionwire.SessionRecordTypeOf(data)
			if err != nil {
				t.Fatalf("SessionRecordTypeOf: %v", err)
			}
			if got != tt.want {
				t.Errorf("SessionRecordTypeOf = %q, want %q", got, tt.want)
			}
			if declared, ok := tt.record.(interface {
				RecordType() sessionwire.SessionRecordType
			}); !ok {
				t.Fatalf("%T does not declare its session record type", tt.record)
			} else if declared.RecordType() != tt.want {
				t.Errorf("RecordType() = %q, want %q", declared.RecordType(), tt.want)
			}
		})
	}
	if got, want := len(tests), 4; got != want {
		t.Errorf("covered %d session-channel record kinds, want %d", got, want)
	}
}

// TestSessionRecordTypeNamesFollowTheSpecification keeps the two spec-named
// values byte-stable; renaming either one silently breaks Factory and WUI.
func TestSessionRecordTypeNamesFollowTheSpecification(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		got  sessionwire.SessionRecordType
		want string
	}{
		{sessionwire.SessionRecordTypeSessionReset, "session.reset"},
		{sessionwire.SessionRecordTypeJournalTip, "journal_tip"},
		{sessionwire.SessionRecordTypeEnduringPublication, "enduring_publication"},
		{sessionwire.SessionRecordTypeEphemeralPublication, "ephemeral_publication"},
	} {
		if string(tt.got) != tt.want {
			t.Errorf("session record type = %q, want %q", tt.got, tt.want)
		}
	}
}

// TestSessionChannelRecordsRejectAbsentOrMismatchedType proves the
// discriminator is load-bearing rather than decorative: a record whose type is
// missing, or whose type names a different kind, must not decode.
func TestSessionChannelRecordsRejectAbsentOrMismatchedType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		absent  string
		wrong   string
		decode  func([]byte) error
		wantAll bool
	}{
		{
			name:   "enduring publication",
			absent: `{"tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"type":"public"}}`,
			wrong:  `{"type":"ephemeral_publication","tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"type":"public"}}`,
			decode: func(data []byte) error {
				var value sessionwire.EnduringPublication
				return json.Unmarshal(data, &value)
			},
		},
		{
			name:   "ephemeral publication",
			absent: `{"tenant_id":"tenant-1","session_id":"session-1","body":{"type":"token_delta"}}`,
			wrong:  `{"type":"enduring_publication","tenant_id":"tenant-1","session_id":"session-1","body":{"type":"token_delta"}}`,
			decode: func(data []byte) error {
				var value sessionwire.EphemeralPublication
				return json.Unmarshal(data, &value)
			},
		},
		{
			name:   "journal tip",
			absent: `{"tenant_id":"tenant-1","session_id":"session-1","journal_tip":5}`,
			wrong:  `{"type":"session.reset","tenant_id":"tenant-1","session_id":"session-1","journal_tip":5}`,
			decode: func(data []byte) error {
				var value sessionwire.JournalTip
				return json.Unmarshal(data, &value)
			},
		},
		{
			name:   "session reset",
			absent: `{"tenant_id":"tenant-1","session_id":"session-1","last_contiguous":3,"journal_tip":5}`,
			wrong:  `{"type":"journal_tip","tenant_id":"tenant-1","session_id":"session-1","last_contiguous":3,"journal_tip":5}`,
			decode: func(data []byte) error {
				var value sessionwire.SessionReset
				return json.Unmarshal(data, &value)
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, probe := range []struct {
				label string
				body  string
				code  sessionwire.RequestValidationCode
			}{
				{"absent", tt.absent, sessionwire.RequestValidationCodeMissingField},
				{"mismatched", tt.wrong, sessionwire.RequestValidationCodeInvalidField},
			} {
				err := tt.decode([]byte(probe.body))
				if err == nil {
					t.Fatalf("%s discriminator accepted: %s", probe.label, probe.body)
				}
				var validation *sessionwire.RequestValidationError
				if !errors.As(err, &validation) {
					t.Fatalf("%s: error = %T (%v), want RequestValidationError", probe.label, err, err)
				}
				if validation.Code != probe.code || validation.Field != "type" {
					t.Errorf("%s: code/field = %q/%q, want %q/%q", probe.label, validation.Code, validation.Field, probe.code, "type")
				}
			}
		})
	}
}

// TestSessionRecordTypeOfRejectsUnknownAndMalformedRecords keeps a subscriber
// fail-closed rather than guessing a kind for an unrecognized record.
func TestSessionRecordTypeOfRejectsUnknownAndMalformedRecords(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"tenant_id":"tenant-1"}`,
		`{"type":"not_a_session_record","tenant_id":"tenant-1"}`,
		`{"type":5}`,
		`[]`,
		`{`,
	} {
		if got, err := sessionwire.SessionRecordTypeOf([]byte(body)); err == nil {
			t.Errorf("SessionRecordTypeOf(%s) = %q, want error", body, got)
		}
	}
}
