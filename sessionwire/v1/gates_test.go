package v1_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sessionwire "github.com/looprig/core/sessionwire/v1"
)

func TestGateProjectionIsPublicAndRedactionSafe(t *testing.T) {
	t.Parallel()

	projection := sessionwire.GateProjection{
		GateID:           "gate-1",
		Kind:             "harness.ask_user",
		OpenedEventID:    "event-42",
		OpenedJournalSeq: 42,
		Deadline:         time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC),
		Answerability:    sessionwire.GateAnswerabilitySuspended,
		Prompt: sessionwire.GatePrompt{
			Title:  "Need your input",
			Body:   "Choose a path.",
			Origin: "https://example.test",
			Schema: sessionwire.GatePromptSchema{Fields: []sessionwire.GatePromptField{{
				Name:     "choice",
				Label:    "Choice",
				Kind:     sessionwire.GateFieldKindSelect,
				Required: true,
				Options:  []sessionwire.GatePromptOption{{Value: "continue", Label: "Continue"}},
			}}},
			Controls: []sessionwire.GateControl{{Action: "answer", Label: "Continue"}},
		},
	}
	if err := projection.Validate(); err != nil {
		t.Fatalf("GateProjection.Validate(): %v", err)
	}
	data, err := json.Marshal(projection)
	if err != nil {
		t.Fatalf("Marshal(GateProjection): %v", err)
	}
	for _, forbidden := range []string{"payload", "values", "raw_answer", "signed_url", "secret"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Errorf("public projection leaked %q: %s", forbidden, data)
		}
	}
}

func TestGatePagePreservesUnknownResponseFields(t *testing.T) {
	t.Parallel()

	const body = `{"journal_tip":42,"open_gate_count":1,"gates":[{"gate_id":"gate-1","kind":"harness.ask_user","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{"title":"Need input"}}],"future_gate_page":true}`
	var page sessionwire.GatePage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("Unmarshal(GatePage): %v", err)
	}
	if got := page.AdditionalFields()["future_gate_page"]; !bytes.Equal(got, []byte("true")) {
		t.Errorf("future_gate_page = %s, want true", got)
	}
	roundTrip, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("Marshal(GatePage): %v", err)
	}
	if !strings.Contains(string(roundTrip), `"future_gate_page":true`) {
		t.Errorf("round-trip JSON dropped future page field: %s", roundTrip)
	}
}

func TestGateProjectionRequiresOpenIdentityAndAnswerability(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		body      string
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{body: `{"gate_id":"gate-1","kind":"harness.ask_user","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{}}`, wantCode: sessionwire.RequestValidationCodeMissingField, wantField: "opened_event_id"},
		{body: `{"gate_id":"gate-1","kind":"harness.ask_user","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"unknown","prompt":{}}`, wantCode: sessionwire.RequestValidationCodeInvalidField, wantField: "answerability"},
	} {
		var projection sessionwire.GateProjection
		assertValidationError(t, json.Unmarshal([]byte(tt.body), &projection), tt.wantCode, tt.wantField)
	}
}

func TestGatePromptRejectsUnrenderableFieldsAndControls(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		prompt    sessionwire.GatePrompt
		wantCode  sessionwire.RequestValidationCode
		wantField string
	}{
		{
			prompt:    sessionwire.GatePrompt{Schema: sessionwire.GatePromptSchema{Fields: []sessionwire.GatePromptField{{Name: "choice", Kind: "future_unrenderable"}}}},
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "prompt.schema.fields.kind",
		},
		{
			prompt:    sessionwire.GatePrompt{Controls: []sessionwire.GateControl{{Action: "", Label: "Continue"}}},
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "prompt.controls",
		},
		{
			prompt:    sessionwire.GatePrompt{Origin: "https://example.test/authorize?state=do-not-persist"},
			wantCode:  sessionwire.RequestValidationCodeInvalidField,
			wantField: "prompt.origin",
		},
	} {
		assertValidationError(t, tt.prompt.Validate(), tt.wantCode, tt.wantField)
	}
}

// TestGateProjectionKindStaysOpaque pins the transport-neutral contract: Kind is
// a forward-compatible opaque string owned by the runtime that opened the gate.
// Core must not encode a Harness-owned enum value or a Harness-owned per-kind
// validation rule, because a rename there would silently desynchronize this
// module. Specification 6.2 requires only a trusted Origin "when applicable";
// which kinds make it applicable belongs to the layer that owns the kind.
func TestGateProjectionKindStaysOpaque(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"gate_id":"gate-1","kind":"harness.open_url","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{}}`,
		`{"gate_id":"gate-1","kind":"some.future.runtime.kind","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{}}`,
	} {
		var projection sessionwire.GateProjection
		if err := json.Unmarshal([]byte(body), &projection); err != nil {
			t.Errorf("GateProjection rejected an opaque gate kind: %s: %v", body, err)
		}
	}
}
