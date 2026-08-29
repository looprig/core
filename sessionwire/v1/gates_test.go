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

	for _, body := range []string{
		`{"gate_id":"gate-1","kind":"harness.ask_user","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{}}`,
		`{"gate_id":"gate-1","kind":"harness.ask_user","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"unknown","prompt":{}}`,
		`{"gate_id":"gate-1","kind":"harness.open_url","opened_event_id":"event-42","opened_journal_seq":42,"deadline":"2026-08-29T20:00:00Z","answerability":"resident","prompt":{}}`,
	} {
		var projection sessionwire.GateProjection
		if err := json.Unmarshal([]byte(body), &projection); err == nil {
			t.Fatalf("GateProjection decoded invalid public state: %s", body)
		}
	}
}

func TestGatePromptRejectsUnrenderableFieldsAndControls(t *testing.T) {
	t.Parallel()

	for _, prompt := range []sessionwire.GatePrompt{
		{Schema: sessionwire.GatePromptSchema{Fields: []sessionwire.GatePromptField{{Name: "choice", Kind: "future_unrenderable"}}}},
		{Controls: []sessionwire.GateControl{{Action: "", Label: "Continue"}}},
		{Origin: "https://example.test/authorize?state=do-not-persist"},
	} {
		if err := prompt.Validate(); err == nil {
			t.Fatalf("GatePrompt.Validate() accepted an unrenderable public prompt: %#v", prompt)
		}
	}
}
