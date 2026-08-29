package v1_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestSessionPageRejectsNonRecentOrder(t *testing.T) {
	t.Parallel()

	newer := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	page := sessionwire.SessionPage{Sessions: []sessionwire.SessionSummary{
		{SessionID: "session-older", AgentID: "agent-1", State: sessionwire.SessionStateIdle, LastActiveAt: newer.Add(-time.Minute)},
		{SessionID: "session-newer", AgentID: "agent-1", State: sessionwire.SessionStateIdle, LastActiveAt: newer},
	}}
	if err := page.Validate(); err == nil {
		t.Fatal("SessionPage.Validate() accepted an older session before a newer session")
	}
}

func TestSessionPagePreservesUnknownResponseFields(t *testing.T) {
	t.Parallel()

	const body = `{"sessions":[{"session_id":"session-1","agent_id":"agent-1","state":"idle","last_active_at":"2026-08-29T12:00:00Z"}],"next_cursor":"opaque-next","future_page_hint":{"source":"ranked"}}`
	var page sessionwire.SessionPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("Unmarshal(SessionPage): %v", err)
	}
	if got := page.AdditionalFields()["future_page_hint"]; !bytes.Equal(got, []byte(`{"source":"ranked"}`)) {
		t.Errorf("future_page_hint = %s, want preserved value", got)
	}
	roundTrip, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("Marshal(SessionPage): %v", err)
	}
	if !bytes.Contains(roundTrip, []byte(`"future_page_hint":{"source":"ranked"}`)) {
		t.Errorf("round-trip JSON dropped future response field: %s", roundTrip)
	}
}

func TestPublicJournalPageFixtureCoversPrivateGapsWithoutLeakingThem(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/public_journal_page.json")
	if err != nil {
		t.Fatalf("ReadFile fixture: %v", err)
	}
	if strings.Contains(string(data), "private") || strings.Contains(string(data), "secret") {
		t.Fatalf("public journal fixture contains private material: %s", data)
	}

	var page sessionwire.JournalPage
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("Unmarshal(JournalPage): %v", err)
	}
	if got, want := page.CapturedTip, uint64(5); got != want {
		t.Errorf("CapturedTip = %d, want %d", got, want)
	}
	if got, want := page.CoveredThrough, uint64(5); got != want {
		t.Errorf("CoveredThrough = %d, want %d", got, want)
	}
	if got, want := len(page.Events), 2; got != want {
		t.Fatalf("len(Events) = %d, want %d", got, want)
	}
	if got, want := page.Events[0].JournalSeq, uint64(2); got != want {
		t.Errorf("first JournalSeq = %d, want %d", got, want)
	}
	if got, want := page.Events[1].JournalSeq, uint64(5); got != want {
		t.Errorf("last JournalSeq = %d, want %d", got, want)
	}
	for _, event := range page.Events {
		if bytes.Contains(event.Body, []byte("private")) || bytes.Contains(event.Body, []byte("secret")) {
			t.Errorf("public event %d leaked private body: %s", event.JournalSeq, event.Body)
		}
	}
	if err := page.Validate(); err != nil {
		t.Fatalf("JournalPage.Validate(): %v", err)
	}
}

func TestJournalPageRejectsCoverageBehindVisibleEvent(t *testing.T) {
	t.Parallel()

	page := sessionwire.JournalPage{
		CapturedTip:    5,
		CoveredThrough: 4,
		Events: []sessionwire.JournalEvent{{
			EventID:    "event-5",
			JournalSeq: 5,
			Body:       json.RawMessage(`{"type":"public"}`),
		}},
	}
	if err := page.Validate(); err == nil {
		t.Fatal("JournalPage.Validate() accepted a visible event past covered_through")
	}
}

func TestPublicJournalAndLivePublicationUseTransportStableCanonicalBodies(t *testing.T) {
	t.Parallel()

	canonical := json.RawMessage(`{"type":"public","html":"\u003ctag\u003e\u0026","line":"\u2028\u2029","number":1e+00}`)
	journal := sessionwire.JournalPage{
		CapturedTip:    5,
		CoveredThrough: 5,
		Events: []sessionwire.JournalEvent{{
			EventID:    "event-5",
			JournalSeq: 5,
			Body:       canonical,
		}},
	}
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatalf("Marshal(JournalPage): %v", err)
	}
	if !bytes.Contains(data, canonical) {
		t.Fatalf("JournalPage changed canonical event body bytes: %s", data)
	}
	var decoded sessionwire.JournalPage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal(JournalPage): %v", err)
	}
	if got := decoded.Events[0].Body; !bytes.Equal(got, canonical) {
		t.Errorf("JournalPage body = %s, want byte-identical %s", got, canonical)
	}

	for _, body := range []json.RawMessage{
		json.RawMessage(`{"html":"<tag>&"}`),
		json.RawMessage("{\"line\":\"" + string(rune(0x2028)) + string(rune(0x2029)) + "\"}"),
	} {
		event := sessionwire.JournalEvent{EventID: "event-5", JournalSeq: 5, Body: body}
		if err := event.Validate(); err == nil {
			t.Fatalf("JournalEvent.Validate() accepted a body encoding/json would rewrite: %s", body)
		}
		publication := sessionwire.EnduringPublication{
			TenantID:       "tenant-1",
			SessionID:      "session-1",
			EventID:        "event-5",
			JournalSeq:     5,
			CoveredThrough: 5,
			Body:           body,
		}
		if err := publication.Validate(); err == nil {
			t.Fatalf("EnduringPublication.Validate() accepted a body encoding/json would rewrite: %s", body)
		}
	}
}

func TestCapabilityAndObjectRecordsRemainTransportNeutral(t *testing.T) {
	t.Parallel()

	department := sessionwire.DepartmentCapabilitySummary{Agents: []sessionwire.AgentCapabilitySummary{{
		AgentID:                "agent-1",
		RuntimeCompatibilityID: "runtime-2026-08",
		Capabilities:           []string{"input", "gates"},
	}}}
	if err := department.Validate(); err != nil {
		t.Fatalf("DepartmentCapabilitySummary.Validate(): %v", err)
	}

	metadata := sessionwire.ObjectMetadata{
		Reference: sessionwire.ObjectReference{ObjectID: "object-1"},
		SizeBytes: 42,
		MediaType: "application/json",
		Digest:    "sha256:abc",
	}
	if err := metadata.Validate(); err != nil {
		t.Fatalf("ObjectMetadata.Validate(): %v", err)
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("Marshal(ObjectMetadata): %v", err)
	}
	for _, forbidden := range []string{"signed_url", "secret", "payload", "contents"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Errorf("object metadata leaked %q: %s", forbidden, data)
		}
	}
}
