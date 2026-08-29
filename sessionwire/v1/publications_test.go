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
	const want = `{"journal_tip":5,"last_contiguous":3,"session_id":"session-1","tenant_id":"tenant-1"}`
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
	const want = `{"journal_tip":5,"session_id":"session-1","tenant_id":"tenant-1"}`
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

	const wire = `{"tenant_id":"tenant-1","session_id":"session-1","event_id":"event-5","journal_seq":5,"covered_through":5,"body":{"html":"<tag>&"}}`
	var decoded sessionwire.EnduringPublication
	if err := json.Unmarshal([]byte(wire), &decoded); err == nil {
		t.Fatal("EnduringPublication.UnmarshalJSON() accepted a body encoding/json would rewrite")
	}
}

func TestEphemeralPublicationRejectsDurableSequencePromise(t *testing.T) {
	t.Parallel()

	const body = `{"tenant_id":"tenant-1","session_id":"session-1","body":{"kind":"token_delta"},"journal_seq":5}`
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
			body: `{"tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"type":"public"},"future":{"safe":true}}`,
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
			body: `{"tenant_id":"tenant-1","session_id":"session-1","journal_tip":1,"future":{"safe":true}}`,
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
			body: `{"tenant_id":"tenant-1","session_id":"session-1","last_contiguous":1,"journal_tip":2,"future":{"safe":true}}`,
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

	body := append([]byte(`{"tenant_id":"tenant-1","session_id":"session-1","event_id":"event-1","journal_seq":1,"covered_through":1,"body":{"text":"`), 0xff)
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
