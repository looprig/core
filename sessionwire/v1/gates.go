package v1

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// GateAnswerability is the durable answerability projection. It is independent
// from whether a historical prompt can be rendered: an open event alone does
// not prove that an answer can still be accepted.
type GateAnswerability string

const (
	GateAnswerabilityResident    GateAnswerability = "resident"
	GateAnswerabilitySuspended   GateAnswerability = "suspended"
	GateAnswerabilitySubmitted   GateAnswerability = "submitted"
	GateAnswerabilityUnavailable GateAnswerability = "unavailable"
	GateAnswerabilityExpired     GateAnswerability = "expired"
)

func (a GateAnswerability) valid() bool {
	switch a {
	case GateAnswerabilityResident, GateAnswerabilitySuspended, GateAnswerabilitySubmitted, GateAnswerabilityUnavailable, GateAnswerabilityExpired:
		return true
	default:
		return false
	}
}

// GateFieldKind identifies the presentation-safe shape of a prompt field.
type GateFieldKind string

const (
	GateFieldKindText        GateFieldKind = "text"
	GateFieldKindSelect      GateFieldKind = "select"
	GateFieldKindMultiSelect GateFieldKind = "multi_select"
	GateFieldKindConfirm     GateFieldKind = "confirm"
)

func (k GateFieldKind) valid() bool {
	switch k {
	case GateFieldKindText, GateFieldKindSelect, GateFieldKindMultiSelect, GateFieldKindConfirm:
		return true
	default:
		return false
	}
}

// GatePromptOption is one presentation-safe selectable option.
type GatePromptOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// GatePromptField is one public structured field. Default is JSON data intended
// for rendering, not a submitted response value.
type GatePromptField struct {
	Name     string             `json:"name"`
	Label    string             `json:"label"`
	Kind     GateFieldKind      `json:"kind"`
	Required bool               `json:"required"`
	Options  []GatePromptOption `json:"options,omitempty"`
	Default  json.RawMessage    `json:"default,omitempty"`
}

// Validate reports whether a field has the public renderer vocabulary needed to
// display it. Unknown field kinds fail closed rather than being rendered as text.
func (f GatePromptField) Validate() error {
	if f.Name == "" {
		return invalidRequest(RequestValidationCodeMissingField, "prompt.schema.fields.name")
	}
	if !f.Kind.valid() {
		return invalidRequest(RequestValidationCodeInvalidField, "prompt.schema.fields.kind")
	}
	if len(f.Default) != 0 && !json.Valid(f.Default) {
		return invalidRequest(RequestValidationCodeInvalidField, "prompt.schema.fields.default")
	}
	return nil
}

// GatePromptSchema groups the public prompt fields. It deliberately contains no
// private prepared payload or response data.
type GatePromptSchema struct {
	Fields []GatePromptField `json:"fields,omitempty"`
}

// Validate reports whether every public field is renderable.
func (s GatePromptSchema) Validate() error {
	for _, field := range s.Fields {
		if err := field.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GateControl is an exact action and display label supplied by the gate owner.
type GateControl struct {
	Action string `json:"action"`
	Label  string `json:"label"`
}

// GatePrompt contains only presentation-safe data that a Factory can render.
// In particular, Origin is the trusted display origin when applicable—not an
// action URL—and no private payload or signed URL is present.
type GatePrompt struct {
	Title    string           `json:"title,omitempty"`
	Body     string           `json:"body,omitempty"`
	Origin   string           `json:"origin,omitempty"`
	Schema   GatePromptSchema `json:"schema,omitempty"`
	Controls []GateControl    `json:"controls,omitempty"`
}

// Validate reports whether the nested public schema and controls are renderable.
func (p GatePrompt) Validate() error {
	if err := validateGateOrigin(p.Origin); err != nil {
		return err
	}
	if err := p.Schema.Validate(); err != nil {
		return err
	}
	for _, control := range p.Controls {
		if strings.TrimSpace(control.Action) == "" || strings.TrimSpace(control.Label) == "" {
			return invalidRequest(RequestValidationCodeInvalidField, "prompt.controls")
		}
	}
	return nil
}

// validateGateOrigin accepts an absent origin for ordinary prompts and otherwise
// permits only the bare http(s) origin a renderer may safely show. A path, query,
// fragment, userinfo, or opaque URL could smuggle a signed action URL into a
// durable public projection and is therefore rejected.
func validateGateOrigin(origin string) error {
	if origin == "" {
		return nil
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "prompt.origin")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return invalidRequest(RequestValidationCodeInvalidField, "prompt.origin")
	}
	return nil
}

// GateProjection is the complete public durable view of an open gate. It joins
// a presentation-safe prompt with its opening event, effective deadline, and
// current answerability; it contains no raw answer or private runtime data.
type GateProjection struct {
	GateID           GateID            `json:"gate_id"`
	Kind             string            `json:"kind"`
	Prompt           GatePrompt        `json:"prompt"`
	OpenedEventID    EventID           `json:"opened_event_id"`
	OpenedJournalSeq uint64            `json:"opened_journal_seq"`
	Deadline         time.Time         `json:"deadline"`
	Answerability    GateAnswerability `json:"answerability"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (p GateProjection) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether the public projection has a durable opening identity
// and one of the explicit answerability states.
func (p GateProjection) Validate() error {
	if err := p.GateID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "gate_id")
	}
	if p.Kind == "" {
		return invalidRequest(RequestValidationCodeMissingField, "kind")
	}
	if p.Kind == "harness.open_url" && p.Prompt.Origin == "" {
		return invalidRequest(RequestValidationCodeMissingField, "prompt.origin")
	}
	if err := p.OpenedEventID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "opened_event_id")
	}
	if p.OpenedJournalSeq == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "opened_journal_seq")
	}
	if p.Deadline.IsZero() {
		return invalidRequest(RequestValidationCodeMissingField, "deadline")
	}
	if !p.Answerability.valid() {
		return invalidRequest(RequestValidationCodeInvalidField, "answerability")
	}
	return p.Prompt.Validate()
}

func (p GateProjection) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	for name, value := range map[string]any{
		"gate_id":            p.GateID,
		"kind":               p.Kind,
		"prompt":             p.Prompt,
		"opened_event_id":    p.OpenedEventID,
		"opened_journal_seq": p.OpenedJournalSeq,
		"deadline":           p.Deadline,
		"answerability":      p.Answerability,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(fields, p.extensions)
}

func (p *GateProjection) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	gateID, err := decodeGateID(fields, "gate_id")
	if err != nil {
		return err
	}
	kind, err := decodeRequiredString(fields, "kind")
	if err != nil {
		return err
	}
	rawPrompt, err := decodeRequiredRawField(fields, "prompt")
	if err != nil {
		return err
	}
	var prompt GatePrompt
	if err := json.Unmarshal(rawPrompt, &prompt); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "prompt")
	}
	openedEventID, err := decodeOptionalEventID(fields, "opened_event_id")
	if err != nil || openedEventID == "" {
		if err != nil {
			return err
		}
		return invalidRequest(RequestValidationCodeMissingField, "opened_event_id")
	}
	openedJournalSeq, err := decodeRequiredUint64(fields, "opened_journal_seq")
	if err != nil {
		return err
	}
	deadline, err := decodeRequiredTime(fields, "deadline")
	if err != nil {
		return err
	}
	answerability, err := decodeRequiredString(fields, "answerability")
	if err != nil {
		return err
	}
	decoded := GateProjection{
		GateID:           gateID,
		Kind:             kind,
		Prompt:           prompt,
		OpenedEventID:    openedEventID,
		OpenedJournalSeq: openedJournalSeq,
		Deadline:         deadline,
		Answerability:    GateAnswerability(answerability),
		extensions:       captureExtensions(fields, "gate_id", "kind", "prompt", "opened_event_id", "opened_journal_seq", "deadline", "answerability"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

// GatePage is the bounded durable read of open public gates. JournalTip is a
// captured tip for the projection, and OpenGateCount can exceed len(Gates) when
// a later API pages a larger open-gate set.
type GatePage struct {
	JournalTip     uint64           `json:"journal_tip"`
	OpenGateCount  uint64           `json:"open_gate_count"`
	Gates          []GateProjection `json:"gates"`
	NextCursor     Cursor           `json:"next_cursor,omitempty"`
	PreviousCursor Cursor           `json:"previous_cursor,omitempty"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (p GatePage) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether every page entry is public and deterministically
// ordered by its opening sequence, with the captured tip at or after every gate.
func (p GatePage) Validate() error {
	if uint64(len(p.Gates)) > p.OpenGateCount {
		return invalidRequest(RequestValidationCodeInvalidField, "open_gate_count")
	}
	var previous uint64
	for _, gate := range p.Gates {
		if err := gate.Validate(); err != nil {
			return err
		}
		if gate.OpenedJournalSeq <= previous || gate.OpenedJournalSeq > p.JournalTip {
			return invalidRequest(RequestValidationCodeInvalidField, "gates")
		}
		previous = gate.OpenedJournalSeq
	}
	return nil
}

func (p GatePage) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	for name, value := range map[string]any{
		"journal_tip":     p.JournalTip,
		"open_gate_count": p.OpenGateCount,
		"gates":           p.Gates,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	if p.NextCursor != "" {
		if err := putJSONField(fields, "next_cursor", p.NextCursor); err != nil {
			return nil, err
		}
	}
	if p.PreviousCursor != "" {
		if err := putJSONField(fields, "previous_cursor", p.PreviousCursor); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(fields, p.extensions)
}

func (p *GatePage) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	journalTip, err := decodeRequiredUint64(fields, "journal_tip")
	if err != nil {
		return err
	}
	openGateCount, err := decodeRequiredUint64(fields, "open_gate_count")
	if err != nil {
		return err
	}
	rawGates, err := decodeRequiredRawField(fields, "gates")
	if err != nil {
		return err
	}
	var gates []GateProjection
	if err := json.Unmarshal(rawGates, &gates); err != nil || gates == nil {
		return invalidRequest(RequestValidationCodeInvalidField, "gates")
	}
	next, err := decodeOptionalCursor(fields, "next_cursor")
	if err != nil {
		return err
	}
	previous, err := decodeOptionalCursor(fields, "previous_cursor")
	if err != nil {
		return err
	}
	decoded := GatePage{
		JournalTip:     journalTip,
		OpenGateCount:  openGateCount,
		Gates:          gates,
		NextCursor:     next,
		PreviousCursor: previous,
		extensions:     captureExtensions(fields, "journal_tip", "open_gate_count", "gates", "next_cursor", "previous_cursor"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}
