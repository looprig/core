package v1

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Declared member lists. Each record names its members exactly once; the
// marshaller, the decoder, and the extension capture all read the same slice.
var (
	enduringPublicationMembers  = []string{sessionRecordTypeMember, "tenant_id", "session_id", "event_id", "journal_seq", "covered_through", "body"}
	ephemeralPublicationMembers = []string{sessionRecordTypeMember, "tenant_id", "session_id", "body"}
	journalTipMembers           = []string{sessionRecordTypeMember, "tenant_id", "session_id", "journal_tip"}
	sessionResetMembers         = []string{sessionRecordTypeMember, "tenant_id", "session_id", "last_contiguous", "journal_tip"}
)

// SessionRecordType is the stable wire name of a record carried on the
// tenant-scoped session channel `session:{tid}:{sid}` (specification 8.1). One
// subscriber receives every publication, repair hint, and repair control on that
// single channel, so each record names its own kind in the `type` envelope
// member rather than leaving a consumer to infer a kind from which optional
// members happen to be present.
//
// The two spec-named values are used verbatim: specification 17.3 and 21 name
// the `session.reset` repair control and the repeatable `journal_tip` hint. A
// dotted name follows the control/RPC namespace the specification already uses
// for `session.input`, `session.interrupt`, and `gate.respond`; the two
// publication records are data rather than controls and use the snake_case
// record names their schemas and fixtures are already published under.
//
// The discriminator belongs to the envelope. It never enters the opaque relayed
// event body, so the canonical byte-identical relay contract is unchanged.
type SessionRecordType string

const (
	SessionRecordTypeEnduringPublication  SessionRecordType = "enduring_publication"
	SessionRecordTypeEphemeralPublication SessionRecordType = "ephemeral_publication"
	SessionRecordTypeJournalTip           SessionRecordType = "journal_tip"
	SessionRecordTypeSessionReset         SessionRecordType = "session.reset"
)

// sessionRecordTypeMember is the envelope member every session-channel record
// carries. It is deliberately a member of the envelope and not of the relayed
// public body.
const sessionRecordTypeMember = "type"

func (t SessionRecordType) valid() bool {
	switch t {
	case SessionRecordTypeEnduringPublication, SessionRecordTypeEphemeralPublication,
		SessionRecordTypeJournalTip, SessionRecordTypeSessionReset:
		return true
	}
	return false
}

// SessionRecordTypeOf reports the record kind of one encoded session-channel
// record so a subscriber can dispatch before decoding. It fails closed on a
// missing, non-string, or unrecognized discriminator rather than guessing.
func SessionRecordTypeOf(data []byte) (SessionRecordType, error) {
	fields, err := decodeContractFields(data, sessionRecordTypeMember)
	if err != nil {
		return "", err
	}
	return decodeSessionRecordType(fields)
}

// decodeSessionRecordType reads and validates the discriminator that every
// session-channel record must carry.
func decodeSessionRecordType(fields map[string]json.RawMessage) (SessionRecordType, error) {
	raw, ok := fields[sessionRecordTypeMember]
	if !ok || isJSONNull(raw) {
		return "", invalidRequest(RequestValidationCodeMissingField, sessionRecordTypeMember)
	}
	value, err := decodeStrictJSONString(raw)
	if err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, sessionRecordTypeMember)
	}
	recordType := SessionRecordType(value)
	if !recordType.valid() {
		return "", invalidRequest(RequestValidationCodeInvalidField, sessionRecordTypeMember)
	}
	return recordType, nil
}

// requireSessionRecordType rejects a record whose discriminator is absent or
// names a different kind than the record being decoded.
func requireSessionRecordType(fields map[string]json.RawMessage, want SessionRecordType) error {
	got, err := decodeSessionRecordType(fields)
	if err != nil {
		return err
	}
	if got != want {
		return invalidRequest(RequestValidationCodeInvalidField, sessionRecordTypeMember)
	}
	return nil
}

// EnduringPublication is a committed public journal event carried over a live
// link. Body is already canonical public JSON and remains opaque to Core. The
// durable identity and sequence let a consumer join this fast path with a
// bounded journal read after reconnect or overflow.
type EnduringPublication struct {
	TenantID       TenantID        `json:"tenant_id"`
	SessionID      SessionID       `json:"session_id"`
	EventID        EventID         `json:"event_id"`
	JournalSeq     uint64          `json:"journal_seq"`
	CoveredThrough uint64          `json:"covered_through"`
	Body           json.RawMessage `json:"body"`

	extensions responseExtensions
}

// RecordType reports the stable session-channel record name this type is
// published under.
func (p EnduringPublication) RecordType() SessionRecordType {
	return SessionRecordTypeEnduringPublication
}

// AdditionalFields returns copies of forward-compatible public publication
// members captured during JSON decoding.
func (p EnduringPublication) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether p can represent one committed public journal event.
// A live public event covers exactly the sequence it was committed at; later
// private records can only be covered by an authenticated journal page, not by
// this event's live publication.
func (p EnduringPublication) Validate() error {
	if err := p.TenantID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "tenant_id")
	}
	if err := p.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if err := p.EventID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "event_id")
	}
	if p.JournalSeq == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "journal_seq")
	}
	if p.CoveredThrough != p.JournalSeq {
		return invalidRequest(RequestValidationCodeInvalidField, "covered_through")
	}
	if validateTransportCanonicalPublicBody(p.Body) != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "body")
	}
	return nil
}

// MarshalJSON keeps the canonical public body opaque. Producers supply the
// canonical JSON spelling (including its standard escape form); a Host relays
// that committed body rather than decoding and synthesizing it anew.
func (p EnduringPublication) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, sessionRecordTypeMember, SessionRecordTypeEnduringPublication); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{
		"tenant_id":       p.TenantID,
		"session_id":      p.SessionID,
		"event_id":        p.EventID,
		"journal_seq":     p.JournalSeq,
		"covered_through": p.CoveredThrough,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	fields["body"] = cloneJSON(p.Body)
	return marshalResponseFields(enduringPublicationMembers, fields, p.extensions)
}

// UnmarshalJSON decodes a public enduring publication while retaining unknown
// additive envelope members and the exact canonical event body bytes.
func (p *EnduringPublication) UnmarshalJSON(data []byte) error {
	fields, err := decodeContractFields(data, enduringPublicationMembers...)
	if err != nil {
		return err
	}
	if err := requireSessionRecordType(fields, SessionRecordTypeEnduringPublication); err != nil {
		return err
	}
	tenantID, err := decodeTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	eventID, err := decodeOptionalEventID(fields, "event_id")
	if err != nil || eventID == "" {
		if err != nil {
			return err
		}
		return invalidRequest(RequestValidationCodeMissingField, "event_id")
	}
	journalSeq, err := decodeRequiredUint64(fields, "journal_seq")
	if err != nil {
		return err
	}
	coveredThrough, err := decodeRequiredUint64(fields, "covered_through")
	if err != nil {
		return err
	}
	body, err := decodeRequiredRawField(fields, "body")
	if err != nil {
		return err
	}
	decoded := EnduringPublication{
		TenantID:       tenantID,
		SessionID:      sessionID,
		EventID:        eventID,
		JournalSeq:     journalSeq,
		CoveredThrough: coveredThrough,
		Body:           body,
		extensions:     captureExtensions(fields, enduringPublicationMembers...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

// EphemeralPublication is a best-effort public live delta. It is deliberately
// not a durable event: it has no EventID, journal sequence, or coverage
// watermark and may be coalesced or dropped by a bounded delivery path.
type EphemeralPublication struct {
	TenantID  TenantID        `json:"tenant_id"`
	SessionID SessionID       `json:"session_id"`
	Body      json.RawMessage `json:"body"`

	extensions responseExtensions
}

// RecordType reports the stable session-channel record name this type is
// published under.
func (p EphemeralPublication) RecordType() SessionRecordType {
	return SessionRecordTypeEphemeralPublication
}

// AdditionalFields returns copies of forward-compatible public publication
// members captured during JSON decoding.
func (p EphemeralPublication) AdditionalFields() map[string]json.RawMessage {
	return p.extensions.copy()
}

// Validate reports whether p is a public, unsequenced live delta.
func (p EphemeralPublication) Validate() error {
	if err := p.TenantID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "tenant_id")
	}
	if err := p.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if validateTransportCanonicalPublicBody(p.Body) != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "body")
	}
	return nil
}

// validateTransportCanonicalPublicBody accepts the strict JSON spelling that is
// a fixed point of encoding/json's RawMessage transport encoding. The public
// journal is an opaque byte contract: accepting a valid but non-fixed-point
// spelling (such as literal <, >, &, U+2028, or U+2029) would silently change
// the committed bytes when an envelope is marshaled. This deliberately checks
// representation without decoding or synthesizing an event body.
func validateTransportCanonicalPublicBody(body json.RawMessage) error {
	if len(body) == 0 || isJSONNull(body) || validateStrictJSON(body) != nil {
		return errors.New("invalid public body")
	}
	encoded, err := json.Marshal(body)
	if err != nil || !bytes.Equal(encoded, body) {
		return errors.New("non-canonical public body")
	}
	return nil
}

func (p EphemeralPublication) MarshalJSON() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, sessionRecordTypeMember, SessionRecordTypeEphemeralPublication); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{
		"tenant_id":  p.TenantID,
		"session_id": p.SessionID,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	fields["body"] = cloneJSON(p.Body)
	return marshalResponseFields(ephemeralPublicationMembers, fields, p.extensions)
}

// UnmarshalJSON rejects a durable identity or sequence on an ephemeral record
// rather than allowing an unsequenced delta to masquerade as replayable data.
func (p *EphemeralPublication) UnmarshalJSON(data []byte) error {
	fields, err := decodeContractFields(data, ephemeralPublicationMembers...)
	if err != nil {
		return err
	}
	if err := requireSessionRecordType(fields, SessionRecordTypeEphemeralPublication); err != nil {
		return err
	}
	for _, name := range []string{"event_id", "journal_seq", "covered_through"} {
		if _, ok := fields[name]; ok {
			return invalidRequest(RequestValidationCodeInvalidField, name)
		}
	}
	tenantID, err := decodeTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	body, err := decodeRequiredRawField(fields, "body")
	if err != nil {
		return err
	}
	decoded := EphemeralPublication{
		TenantID:   tenantID,
		SessionID:  sessionID,
		Body:       body,
		extensions: captureExtensions(fields, ephemeralPublicationMembers...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*p = decoded
	return nil
}

// JournalTip is a repeatable repair hint. It is not an acknowledgement or a
// browser cursor: a client compares Tip with its own covered sequence and reads
// any missing public pages through the durable query plane.
type JournalTip struct {
	TenantID  TenantID  `json:"tenant_id"`
	SessionID SessionID `json:"session_id"`
	Tip       uint64    `json:"journal_tip"`

	extensions responseExtensions
}

// RecordType reports the stable session-channel record name this type is
// published under.
func (h JournalTip) RecordType() SessionRecordType { return SessionRecordTypeJournalTip }

// AdditionalFields returns copies of forward-compatible repair-hint members.
func (h JournalTip) AdditionalFields() map[string]json.RawMessage {
	return h.extensions.copy()
}

// Validate reports whether h has the tenant/session scope required for a
// durable repair hint. A zero Tip is valid for a session with no journal yet.
func (h JournalTip) Validate() error {
	if err := h.TenantID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "tenant_id")
	}
	if err := h.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	return nil
}

func (h JournalTip) MarshalJSON() ([]byte, error) {
	if err := h.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, sessionRecordTypeMember, SessionRecordTypeJournalTip); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{
		"tenant_id":   h.TenantID,
		"session_id":  h.SessionID,
		"journal_tip": h.Tip,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(journalTipMembers, fields, h.extensions)
}

func (h *JournalTip) UnmarshalJSON(data []byte) error {
	fields, err := decodeContractFields(data, journalTipMembers...)
	if err != nil {
		return err
	}
	if err := requireSessionRecordType(fields, SessionRecordTypeJournalTip); err != nil {
		return err
	}
	tenantID, err := decodeTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	tip, err := decodeRequiredUint64(fields, "journal_tip")
	if err != nil {
		return err
	}
	decoded := JournalTip{
		TenantID:   tenantID,
		SessionID:  sessionID,
		Tip:        tip,
		extensions: captureExtensions(fields, journalTipMembers...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*h = decoded
	return nil
}

// SessionReset is a repeatable repair control sent after a bounded delivery
// path loses an enduring frame. LastContiguous is the greatest sequence the
// sender knows it forwarded in order; JournalTip is one durable tip captured
// for the receiver's subsequent bounded repair reads.
type SessionReset struct {
	TenantID       TenantID  `json:"tenant_id"`
	SessionID      SessionID `json:"session_id"`
	LastContiguous uint64    `json:"last_contiguous"`
	JournalTip     uint64    `json:"journal_tip"`

	extensions responseExtensions
}

// RecordType reports the stable session-channel record name this type is
// published under.
func (r SessionReset) RecordType() SessionRecordType { return SessionRecordTypeSessionReset }

// AdditionalFields returns copies of forward-compatible reset-control members.
func (r SessionReset) AdditionalFields() map[string]json.RawMessage {
	return r.extensions.copy()
}

// Validate reports whether reset coverage is internally coherent. A zero
// sequence is valid for a consumer that has not yet received a durable event.
func (r SessionReset) Validate() error {
	if err := r.TenantID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "tenant_id")
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if r.LastContiguous > r.JournalTip {
		return invalidRequest(RequestValidationCodeInvalidField, "journal_tip")
	}
	return nil
}

func (r SessionReset) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, sessionRecordTypeMember, SessionRecordTypeSessionReset); err != nil {
		return nil, err
	}
	for name, value := range map[string]any{
		"tenant_id":       r.TenantID,
		"session_id":      r.SessionID,
		"last_contiguous": r.LastContiguous,
		"journal_tip":     r.JournalTip,
	} {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(sessionResetMembers, fields, r.extensions)
}

func (r *SessionReset) UnmarshalJSON(data []byte) error {
	fields, err := decodeContractFields(data, sessionResetMembers...)
	if err != nil {
		return err
	}
	if err := requireSessionRecordType(fields, SessionRecordTypeSessionReset); err != nil {
		return err
	}
	tenantID, err := decodeTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	lastContiguous, err := decodeRequiredUint64(fields, "last_contiguous")
	if err != nil {
		return err
	}
	tip, err := decodeRequiredUint64(fields, "journal_tip")
	if err != nil {
		return err
	}
	decoded := SessionReset{
		TenantID:       tenantID,
		SessionID:      sessionID,
		LastContiguous: lastContiguous,
		JournalTip:     tip,
		extensions:     captureExtensions(fields, sessionResetMembers...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

func decodeTenantID(fields map[string]json.RawMessage, name string) (TenantID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := TenantID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}
