package v1

import (
	"encoding/json"
	"time"
)

// Cursor is an opaque, server-issued page token. Clients may retain and return
// it but must not derive ordering, tenancy, or authority from its contents.
type Cursor string

// AgentCapabilitySummary advertises one Department member's stable identity and
// explicit runtime compatibility boundary without exposing launch internals.
type AgentCapabilitySummary struct {
	AgentID                AgentID  `json:"agent_id"`
	RuntimeCompatibilityID string   `json:"runtime_compatibility_id"`
	Capabilities           []string `json:"capabilities,omitempty"`
}

// Validate reports whether the capability summary identifies a launchable agent.
func (s AgentCapabilitySummary) Validate() error {
	if err := s.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if s.RuntimeCompatibilityID == "" {
		return invalidRequest(RequestValidationCodeMissingField, "runtime_compatibility_id")
	}
	return nil
}

// DepartmentCapabilitySummary is the transport-neutral capability discovery
// response for a Host Department or a Factory projection of one.
type DepartmentCapabilitySummary struct {
	Agents []AgentCapabilitySummary `json:"agents"`
}

// Validate reports whether every advertised agent is structurally valid.
func (s DepartmentCapabilitySummary) Validate() error {
	for _, agent := range s.Agents {
		if err := agent.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// SessionState is the durable execution-state projection. Values may grow in a
// future wire version; a consumer must treat an unfamiliar non-empty value as a
// state it cannot actively control rather than as a missing session.
type SessionState string

const (
	SessionStateRunning       SessionState = "running"
	SessionStateWaitingOnGate SessionState = "waiting_on_gate"
	SessionStateSuspended     SessionState = "suspended"
	SessionStateRestoring     SessionState = "restoring"
	SessionStateIdle          SessionState = "idle"
	SessionStateFailed        SessionState = "failed"
	SessionStateInterrupted   SessionState = "interrupted"
	SessionStateStopped       SessionState = "stopped"
)

// SessionResidency is independent from SessionState: an idle session can remain
// resident and a cold session remains readable.
type SessionResidency string

const (
	SessionResidencyCold      SessionResidency = "cold"
	SessionResidencyAttaching SessionResidency = "attaching"
	SessionResidencyResident  SessionResidency = "resident"
	SessionResidencyReleasing SessionResidency = "releasing"
)

// SessionSummary is the bounded, picker-facing durable projection used in a
// recent-first SessionPage. It deliberately contains no live Host route.
type SessionSummary struct {
	SessionID    SessionID    `json:"session_id"`
	AgentID      AgentID      `json:"agent_id"`
	State        SessionState `json:"state"`
	Title        string       `json:"title,omitempty"`
	CreatedAt    time.Time    `json:"created_at,omitzero"`
	LastActiveAt time.Time    `json:"last_active_at"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (s SessionSummary) AdditionalFields() map[string]json.RawMessage {
	return s.extensions.copy()
}

// Validate reports whether the summary has its required durable identity and
// recency projection. Any non-empty state is allowed for forward compatibility.
func (s SessionSummary) Validate() error {
	if err := s.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if err := s.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if s.State == "" {
		return invalidRequest(RequestValidationCodeMissingField, "state")
	}
	if s.LastActiveAt.IsZero() {
		return invalidRequest(RequestValidationCodeMissingField, "last_active_at")
	}
	return nil
}

func (s SessionSummary) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "session_id", s.SessionID); err != nil {
		return nil, err
	}
	if err := putJSONField(fields, "agent_id", s.AgentID); err != nil {
		return nil, err
	}
	if err := putJSONField(fields, "state", s.State); err != nil {
		return nil, err
	}
	if s.Title != "" {
		if err := putJSONField(fields, "title", s.Title); err != nil {
			return nil, err
		}
	}
	if !s.CreatedAt.IsZero() {
		if err := putJSONField(fields, "created_at", s.CreatedAt); err != nil {
			return nil, err
		}
	}
	if err := putJSONField(fields, "last_active_at", s.LastActiveAt); err != nil {
		return nil, err
	}
	return marshalResponseFields(fields, s.extensions)
}

func (s *SessionSummary) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	state, err := decodeRequiredString(fields, "state")
	if err != nil {
		return err
	}
	lastActiveAt, err := decodeRequiredTime(fields, "last_active_at")
	if err != nil {
		return err
	}
	var title string
	if raw, ok := fields["title"]; ok {
		if isJSONNull(raw) || json.Unmarshal(raw, &title) != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "title")
		}
	}
	var createdAt time.Time
	if raw, ok := fields["created_at"]; ok {
		if isJSONNull(raw) {
			return invalidRequest(RequestValidationCodeInvalidField, "created_at")
		}
		if err := json.Unmarshal(raw, &createdAt); err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "created_at")
		}
	}
	decoded := SessionSummary{
		SessionID:    sessionID,
		AgentID:      agentID,
		State:        SessionState(state),
		Title:        title,
		CreatedAt:    createdAt,
		LastActiveAt: lastActiveAt,
		extensions:   captureExtensions(fields, "session_id", "agent_id", "state", "title", "created_at", "last_active_at"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*s = decoded
	return nil
}

// SessionStatus is the replay-free durable state for one session. It reports
// only projections that remain available while every Host is stopped.
type SessionStatus struct {
	SessionID     SessionID        `json:"session_id"`
	AgentID       AgentID          `json:"agent_id"`
	State         SessionState     `json:"state"`
	Residency     SessionResidency `json:"residency"`
	JournalTip    uint64           `json:"journal_tip"`
	WaitingGateID GateID           `json:"waiting_gate_id,omitempty"`
	UpdatedAt     time.Time        `json:"updated_at,omitzero"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (s SessionStatus) AdditionalFields() map[string]json.RawMessage {
	return s.extensions.copy()
}

// Validate reports whether the status names a session and both independent
// execution and residency dimensions.
func (s SessionStatus) Validate() error {
	if err := s.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if err := s.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if s.State == "" {
		return invalidRequest(RequestValidationCodeMissingField, "state")
	}
	if s.Residency == "" {
		return invalidRequest(RequestValidationCodeMissingField, "residency")
	}
	if s.WaitingGateID != "" {
		if err := s.WaitingGateID.Validate(); err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "waiting_gate_id")
		}
	}
	return nil
}

func (s SessionStatus) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	for name, value := range map[string]any{
		"session_id":  s.SessionID,
		"agent_id":    s.AgentID,
		"state":       s.State,
		"residency":   s.Residency,
		"journal_tip": s.JournalTip,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	if s.WaitingGateID != "" {
		if err := putJSONField(fields, "waiting_gate_id", s.WaitingGateID); err != nil {
			return nil, err
		}
	}
	if !s.UpdatedAt.IsZero() {
		if err := putJSONField(fields, "updated_at", s.UpdatedAt); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(fields, s.extensions)
}

func (s *SessionStatus) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	state, err := decodeRequiredString(fields, "state")
	if err != nil {
		return err
	}
	residency, err := decodeRequiredString(fields, "residency")
	if err != nil {
		return err
	}
	journalTip, err := decodeRequiredUint64(fields, "journal_tip")
	if err != nil {
		return err
	}
	var waitingGateID GateID
	if _, ok := fields["waiting_gate_id"]; ok {
		value, err := decodeRequiredString(fields, "waiting_gate_id")
		if err != nil {
			return err
		}
		waitingGateID = GateID(value)
	}
	var updatedAt time.Time
	if raw, ok := fields["updated_at"]; ok {
		if isJSONNull(raw) || json.Unmarshal(raw, &updatedAt) != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "updated_at")
		}
	}
	decoded := SessionStatus{
		SessionID:     sessionID,
		AgentID:       agentID,
		State:         SessionState(state),
		Residency:     SessionResidency(residency),
		JournalTip:    journalTip,
		WaitingGateID: waitingGateID,
		UpdatedAt:     updatedAt,
		extensions:    captureExtensions(fields, "session_id", "agent_id", "state", "residency", "journal_tip", "waiting_gate_id", "updated_at"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*s = decoded
	return nil
}

// SessionPage is a bounded recent-first page. It is a durable read and never
// restores a cold session as a side effect.
type SessionPage struct {
	Sessions       []SessionSummary `json:"sessions"`
	NextCursor     Cursor           `json:"next_cursor,omitempty"`
	PreviousCursor Cursor           `json:"previous_cursor,omitempty"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (p SessionPage) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether the page is in non-increasing LastActiveAt order.
// Equal timestamps are left in the provider's stable cursor order.
func (p SessionPage) Validate() error {
	for index, summary := range p.Sessions {
		if err := summary.Validate(); err != nil {
			return err
		}
		if index > 0 && summary.LastActiveAt.After(p.Sessions[index-1].LastActiveAt) {
			return invalidRequest(RequestValidationCodeInvalidField, "sessions")
		}
	}
	return nil
}

func (p SessionPage) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "sessions", p.Sessions); err != nil {
		return nil, err
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

func (p *SessionPage) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	rawSessions, err := decodeRequiredRawField(fields, "sessions")
	if err != nil {
		return err
	}
	var sessions []SessionSummary
	if err := json.Unmarshal(rawSessions, &sessions); err != nil || sessions == nil {
		return invalidRequest(RequestValidationCodeInvalidField, "sessions")
	}
	next, err := decodeOptionalCursor(fields, "next_cursor")
	if err != nil {
		return err
	}
	previous, err := decodeOptionalCursor(fields, "previous_cursor")
	if err != nil {
		return err
	}
	decoded := SessionPage{
		Sessions:       sessions,
		NextCursor:     next,
		PreviousCursor: previous,
		extensions:     captureExtensions(fields, "sessions", "next_cursor", "previous_cursor"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

// JournalEvent is a public durable event. Body is already canonical public JSON
// and remains opaque to Core: it is neither decoded from nor synthesized from a
// Harness runtime event.
type JournalEvent struct {
	EventID    EventID         `json:"event_id"`
	JournalSeq uint64          `json:"journal_seq"`
	Body       json.RawMessage `json:"body"`
}

// Validate reports whether the event has a public identity, a durable sequence,
// and a non-null JSON body.
func (e JournalEvent) Validate() error {
	if err := e.EventID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "event_id")
	}
	if e.JournalSeq == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "journal_seq")
	}
	if len(e.Body) == 0 || isJSONNull(e.Body) || !json.Valid(e.Body) {
		return invalidRequest(RequestValidationCodeInvalidField, "body")
	}
	return nil
}

// JournalPage is a bounded public journal response captured at CapturedTip.
// CoveredThrough can advance across private ledger records, but never reveals
// their kind or bytes; clients may only use this authenticated watermark to
// close a sequence gap.
type JournalPage struct {
	Events         []JournalEvent `json:"events"`
	CapturedTip    uint64         `json:"journal_tip"`
	CoveredThrough uint64         `json:"covered_through"`
	NextCursor     Cursor         `json:"next_cursor,omitempty"`
	PreviousCursor Cursor         `json:"previous_cursor,omitempty"`

	extensions responseExtensions
}

// AdditionalFields returns copies of unknown additive response fields.
func (p JournalPage) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether page sequences are increasing and all public events
// are covered by an authenticated watermark within the captured journal tip.
func (p JournalPage) Validate() error {
	if p.CoveredThrough > p.CapturedTip {
		return invalidRequest(RequestValidationCodeInvalidField, "covered_through")
	}
	var previous uint64
	for _, event := range p.Events {
		if err := event.Validate(); err != nil {
			return err
		}
		if event.JournalSeq <= previous || event.JournalSeq > p.CoveredThrough {
			return invalidRequest(RequestValidationCodeInvalidField, "events")
		}
		previous = event.JournalSeq
	}
	return nil
}

func (p JournalPage) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "events", p.Events); err != nil {
		return nil, err
	}
	if err := putJSONField(fields, "journal_tip", p.CapturedTip); err != nil {
		return nil, err
	}
	if err := putJSONField(fields, "covered_through", p.CoveredThrough); err != nil {
		return nil, err
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

func (p *JournalPage) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidRequest(RequestValidationCodeInvalidJSON, "")
	}
	rawEvents, err := decodeRequiredRawField(fields, "events")
	if err != nil {
		return err
	}
	var events []JournalEvent
	if err := json.Unmarshal(rawEvents, &events); err != nil || events == nil {
		return invalidRequest(RequestValidationCodeInvalidField, "events")
	}
	tip, err := decodeRequiredUint64(fields, "journal_tip")
	if err != nil {
		return err
	}
	coveredThrough, err := decodeRequiredUint64(fields, "covered_through")
	if err != nil {
		return err
	}
	next, err := decodeOptionalCursor(fields, "next_cursor")
	if err != nil {
		return err
	}
	previous, err := decodeOptionalCursor(fields, "previous_cursor")
	if err != nil {
		return err
	}
	decoded := JournalPage{
		Events:         events,
		CapturedTip:    tip,
		CoveredThrough: coveredThrough,
		NextCursor:     next,
		PreviousCursor: previous,
		extensions:     captureExtensions(fields, "events", "journal_tip", "covered_through", "next_cursor", "previous_cursor"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

// ObjectReference is an opaque logical object identity. It is not a bucket key,
// signed URL, credential, or a byte payload.
type ObjectReference struct {
	ObjectID string `json:"object_id"`
}

// Validate reports whether the logical reference is bounded opaque UTF-8.
func (r ObjectReference) Validate() error {
	if err := validateID(r.ObjectID); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "object_id")
	}
	return nil
}

// ObjectMetadata describes an immutable logical object without exposing a
// provider URL, credential, backend path, or object bytes.
type ObjectMetadata struct {
	Reference ObjectReference `json:"reference"`
	SizeBytes uint64          `json:"size_bytes"`
	MediaType string          `json:"media_type,omitempty"`
	Digest    string          `json:"digest,omitempty"`
	CreatedAt time.Time       `json:"created_at,omitzero"`
}

// Validate reports whether metadata has a safe logical reference.
func (m ObjectMetadata) Validate() error { return m.Reference.Validate() }

func decodeRequiredUint64(fields map[string]json.RawMessage, name string) (uint64, error) {
	raw, ok := fields[name]
	if !ok || isJSONNull(raw) {
		return 0, invalidRequest(RequestValidationCodeMissingField, name)
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeRequiredTime(fields map[string]json.RawMessage, name string) (time.Time, error) {
	raw, ok := fields[name]
	if !ok || isJSONNull(raw) {
		return time.Time{}, invalidRequest(RequestValidationCodeMissingField, name)
	}
	var value time.Time
	if err := json.Unmarshal(raw, &value); err != nil || value.IsZero() {
		return time.Time{}, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeOptionalCursor(fields map[string]json.RawMessage, name string) (Cursor, error) {
	raw, ok := fields[name]
	if !ok {
		return "", nil
	}
	if isJSONNull(raw) {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return Cursor(value), nil
}
