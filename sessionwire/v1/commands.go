package v1

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// CommandEnvelope is common to every state-changing V1 request. CommandID is
// client-generated and retry-stable; Version is the Looprig wire version, not a
// transport or broker version.
type CommandEnvelope struct {
	Version   WireVersion `json:"version"`
	CommandID CommandID   `json:"command_id"`
}

// Validate reports whether the envelope can participate in V1 durable command
// admission.
func (e CommandEnvelope) Validate() error {
	if e.Version != CurrentWireVersion {
		return invalidRequest(RequestValidationCodeUnsupportedVersion, "version")
	}
	if err := e.CommandID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "command_id")
	}
	return nil
}

// CreateRequest admits a new durable session. SessionID is supplied by the
// client so a retry after an unknown outcome can reuse the same
// (SessionID, CommandID) pair; it is intentionally not server-minted here.
type CreateRequest struct {
	CommandEnvelope
	SessionID SessionID       `json:"session_id"`
	AgentID   AgentID         `json:"agent_id"`
	Blocks    json.RawMessage `json:"blocks,omitempty"`
}

// Validate reports whether the create request has the identities needed for
// retry-safe admission. Blocks are optional for an idle create.
func (r CreateRequest) Validate() error {
	if err := r.CommandEnvelope.Validate(); err != nil {
		return err
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if err := r.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if len(r.Blocks) != 0 {
		return validateBlocks(r.Blocks, "blocks")
	}
	return nil
}

// UnmarshalJSON strictly decodes a create request. State-changing request
// records fail closed on unknown, duplicate, missing, or structurally invalid
// required members.
func (r *CreateRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, "version", "command_id", "session_id", "agent_id", "blocks")
	if err != nil {
		return err
	}
	envelope, err := decodeCommandEnvelope(fields)
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	blocks, err := decodeOptionalRawField(fields, "blocks")
	if err != nil {
		return err
	}
	decoded := CreateRequest{CommandEnvelope: envelope, SessionID: sessionID, AgentID: agentID, Blocks: blocks}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// InputRequest admits input blocks for an existing durable session. The blocks
// retain their canonical JSON form so Core does not import Harness content types.
type InputRequest struct {
	CommandEnvelope
	SessionID SessionID       `json:"session_id"`
	Blocks    json.RawMessage `json:"blocks"`
}

// Validate reports whether the input request contains a client command identity,
// a target session, and a non-empty JSON block array.
func (r InputRequest) Validate() error {
	if err := r.CommandEnvelope.Validate(); err != nil {
		return err
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	return validateBlocks(r.Blocks, "blocks")
}

// UnmarshalJSON strictly decodes an input request.
func (r *InputRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, "version", "command_id", "session_id", "blocks")
	if err != nil {
		return err
	}
	envelope, err := decodeCommandEnvelope(fields)
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	blocks, err := decodeRequiredRawField(fields, "blocks")
	if err != nil {
		return err
	}
	decoded := InputRequest{CommandEnvelope: envelope, SessionID: sessionID, Blocks: blocks}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// InterruptRequest asks the durable command plane to interrupt the target
// session. Completion is observed through CommandStatus and public events.
type InterruptRequest struct {
	CommandEnvelope
	SessionID SessionID `json:"session_id"`
}

// Validate reports whether the interrupt request is structurally admissible.
func (r InterruptRequest) Validate() error {
	if err := r.CommandEnvelope.Validate(); err != nil {
		return err
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	return nil
}

// UnmarshalJSON strictly decodes an interrupt request.
func (r *InterruptRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, "version", "command_id", "session_id")
	if err != nil {
		return err
	}
	envelope, err := decodeCommandEnvelope(fields)
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	decoded := InterruptRequest{CommandEnvelope: envelope, SessionID: sessionID}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// RestoreRequest is the explicit wire-compatibility request. New clients rely on
// normal durable command admission and placement; this record keeps the staged
// route explicit without making a read trigger restoration.
type RestoreRequest struct {
	CommandEnvelope
	SessionID SessionID `json:"session_id"`
}

// Validate reports whether the compatibility restore request is structurally
// admissible.
func (r RestoreRequest) Validate() error {
	if err := r.CommandEnvelope.Validate(); err != nil {
		return err
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	return nil
}

// UnmarshalJSON strictly decodes a compatibility restore request.
func (r *RestoreRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, "version", "command_id", "session_id")
	if err != nil {
		return err
	}
	envelope, err := decodeCommandEnvelope(fields)
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	decoded := RestoreRequest{CommandEnvelope: envelope, SessionID: sessionID}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// GateResponseRequest admits a private response to one public gate projection.
// Values are intentionally raw JSON because Harness remains the semantic gate
// validator; Core only validates the safe structural boundary.
type GateResponseRequest struct {
	CommandEnvelope
	SessionID              SessionID                  `json:"session_id"`
	GateID                 GateID                     `json:"gate_id"`
	Action                 string                     `json:"action"`
	Values                 map[string]json.RawMessage `json:"values"`
	ExpectedOpenEventID    EventID                    `json:"expected_open_event_id,omitempty"`
	ExpectedOpenJournalSeq uint64                     `json:"expected_open_journal_seq,omitempty"`

	expectedOpenJournalSeqExplicitlyZero bool
}

// Validate reports whether the gate response has exactly one optimistic-open
// version (event identity or journal sequence), preventing an answer from being
// applied to a different incarnation of the same gate ID.
func (r GateResponseRequest) Validate() error {
	if err := r.CommandEnvelope.Validate(); err != nil {
		return err
	}
	if err := r.SessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	if err := r.GateID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "gate_id")
	}
	if strings.TrimSpace(r.Action) == "" {
		return invalidRequest(RequestValidationCodeInvalidField, "action")
	}
	if r.Values == nil {
		return invalidRequest(RequestValidationCodeMissingField, "values")
	}
	if r.expectedOpenJournalSeqExplicitlyZero {
		return invalidRequest(RequestValidationCodeInvalidField, "expected_open_journal_seq")
	}
	hasEventID := r.ExpectedOpenEventID != ""
	hasSequence := r.ExpectedOpenJournalSeq != 0
	if hasEventID {
		if err := r.ExpectedOpenEventID.Validate(); err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "expected_open_event_id")
		}
	}
	if hasEventID == hasSequence {
		return invalidRequest(RequestValidationCodeInvalidField, "expected_open_version")
	}
	for name, value := range r.Values {
		if name == "" || !utf8.ValidString(name) || validateStrictJSON(value) != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "values")
		}
	}
	return nil
}

// UnmarshalJSON strictly decodes a gate response request.
func (r *GateResponseRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, "version", "command_id", "session_id", "gate_id", "action", "values", "expected_open_event_id", "expected_open_journal_seq")
	if err != nil {
		return err
	}
	envelope, err := decodeCommandEnvelope(fields)
	if err != nil {
		return err
	}
	sessionID, err := decodeSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	gateID, err := decodeGateID(fields, "gate_id")
	if err != nil {
		return err
	}
	action, err := decodeRequiredString(fields, "action")
	if err != nil {
		return err
	}
	values, err := decodeRawValues(fields)
	if err != nil {
		return err
	}
	eventID, err := decodeOptionalEventID(fields, "expected_open_event_id")
	if err != nil {
		return err
	}
	sequence, err := decodeOptionalUint64(fields, "expected_open_journal_seq")
	if err != nil {
		return err
	}
	_, sequencePresent := fields["expected_open_journal_seq"]
	decoded := GateResponseRequest{
		CommandEnvelope:                      envelope,
		SessionID:                            sessionID,
		GateID:                               gateID,
		Action:                               action,
		Values:                               values,
		ExpectedOpenEventID:                  eventID,
		ExpectedOpenJournalSeq:               sequence,
		expectedOpenJournalSeqExplicitlyZero: sequencePresent && sequence == 0,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// CommandState records the durable command-admission lifecycle visible to a
// caller. Accepted means the inbox commit succeeded; it does not promise that a
// Host has already applied the command.
type CommandState string

const (
	CommandStateAccepted CommandState = "accepted"
	CommandStatePending  CommandState = "pending"
	CommandStateApplied  CommandState = "applied"
	CommandStateRejected CommandState = "rejected"
)

// CommandStatus is the forward-compatible public representation of an accepted
// command. Rejected commands carry a stable ErrorDetail rather than a provider or
// runtime cause.
type CommandStatus struct {
	CommandID     CommandID    `json:"command_id"`
	State         CommandState `json:"status"`
	AcceptedOrder uint64       `json:"accepted_order,omitempty"`
	Error         *ErrorDetail `json:"error,omitempty"`

	extensions responseExtensions
}

// AdditionalFields returns copies of forward-compatible response members.
func (s CommandStatus) AdditionalFields() map[string]json.RawMessage {
	return s.extensions.copy()
}

// Validate reports whether the public command state is structurally coherent.
func (s CommandStatus) Validate() error {
	if err := s.CommandID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "command_id")
	}
	switch s.State {
	case CommandStateAccepted, CommandStatePending, CommandStateApplied:
		if s.Error != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "error")
		}
	case CommandStateRejected:
		if s.Error == nil {
			return invalidRequest(RequestValidationCodeMissingField, "error")
		}
		if err := s.Error.Validate(); err != nil {
			return err
		}
	default:
		return invalidRequest(RequestValidationCodeInvalidField, "status")
	}
	return nil
}

func (s CommandStatus) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "command_id", s.CommandID); err != nil {
		return nil, err
	}
	if err := putJSONField(fields, "status", s.State); err != nil {
		return nil, err
	}
	if s.AcceptedOrder != 0 {
		if err := putJSONField(fields, "accepted_order", s.AcceptedOrder); err != nil {
			return nil, err
		}
	}
	if s.Error != nil {
		if err := putJSONField(fields, "error", s.Error); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(fields, s.extensions)
}

func (s *CommandStatus) UnmarshalJSON(data []byte) error {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return invalidJSONObject(err)
	}
	commandID, err := decodeCommandID(fields, "command_id")
	if err != nil {
		return err
	}
	state, err := decodeRequiredString(fields, "status")
	if err != nil {
		return err
	}
	acceptedOrder, err := decodeOptionalUint64(fields, "accepted_order")
	if err != nil {
		return err
	}
	var detail *ErrorDetail
	if raw, ok := fields["error"]; ok {
		if isJSONNull(raw) {
			return invalidRequest(RequestValidationCodeInvalidField, "error")
		}
		var decoded ErrorDetail
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "error")
		}
		detail = &decoded
	}
	decoded := CommandStatus{
		CommandID:     commandID,
		State:         CommandState(state),
		AcceptedOrder: acceptedOrder,
		Error:         detail,
		extensions:    captureExtensions(fields, "command_id", "status", "accepted_order", "error"),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*s = decoded
	return nil
}

func decodeRequestFields(data []byte, known ...string) (map[string]json.RawMessage, error) {
	fields, err := decodeJSONObject(data)
	if err != nil {
		return nil, invalidJSONObject(err)
	}
	allowed := make(map[string]struct{}, len(known))
	for _, name := range known {
		allowed[name] = struct{}{}
	}
	for name := range fields {
		if _, ok := allowed[name]; !ok {
			return nil, invalidRequest(RequestValidationCodeUnknownField, "")
		}
	}
	return fields, nil
}

func decodeCommandEnvelope(fields map[string]json.RawMessage) (CommandEnvelope, error) {
	versionRaw, ok := fields["version"]
	if !ok || isJSONNull(versionRaw) {
		return CommandEnvelope{}, invalidRequest(RequestValidationCodeMissingField, "version")
	}
	var version WireVersion
	if err := json.Unmarshal(versionRaw, &version); err != nil {
		return CommandEnvelope{}, invalidRequest(RequestValidationCodeInvalidField, "version")
	}
	commandID, err := decodeCommandID(fields, "command_id")
	if err != nil {
		return CommandEnvelope{}, err
	}
	envelope := CommandEnvelope{Version: version, CommandID: commandID}
	if err := envelope.Validate(); err != nil {
		return CommandEnvelope{}, err
	}
	return envelope, nil
}

func decodeCommandID(fields map[string]json.RawMessage, name string) (CommandID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := CommandID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeSessionID(fields map[string]json.RawMessage, name string) (SessionID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := SessionID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeAgentID(fields map[string]json.RawMessage, name string) (AgentID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := AgentID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeGateID(fields map[string]json.RawMessage, name string) (GateID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := GateID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeOptionalEventID(fields map[string]json.RawMessage, name string) (EventID, error) {
	raw, ok := fields[name]
	if !ok {
		return "", nil
	}
	if isJSONNull(raw) {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	value, err := decodeStrictJSONString(raw)
	if err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	id := EventID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeRequiredString(fields map[string]json.RawMessage, name string) (string, error) {
	raw, ok := fields[name]
	if !ok || isJSONNull(raw) {
		return "", invalidRequest(RequestValidationCodeMissingField, name)
	}
	value, err := decodeStrictJSONString(raw)
	if err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeRequiredRawField(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok || isJSONNull(raw) {
		return nil, invalidRequest(RequestValidationCodeMissingField, name)
	}
	if validateStrictJSON(raw) != nil {
		return nil, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return cloneJSON(raw), nil
}

func decodeOptionalRawField(fields map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := fields[name]
	if !ok {
		return nil, nil
	}
	if isJSONNull(raw) || validateStrictJSON(raw) != nil {
		return nil, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return cloneJSON(raw), nil
}

func decodeOptionalUint64(fields map[string]json.RawMessage, name string) (uint64, error) {
	raw, ok := fields[name]
	if !ok {
		return 0, nil
	}
	if isJSONNull(raw) {
		return 0, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeRawValues(fields map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	raw, ok := fields["values"]
	if !ok || isJSONNull(raw) {
		return nil, invalidRequest(RequestValidationCodeMissingField, "values")
	}
	values, err := decodeJSONObject(raw)
	if err != nil {
		return nil, invalidNestedJSONObject(err, "values")
	}
	for name, value := range values {
		if name == "" || !utf8.ValidString(name) || validateStrictJSON(value) != nil {
			return nil, invalidRequest(RequestValidationCodeInvalidField, "values")
		}
		values[name] = cloneJSON(value)
	}
	return values, nil
}

func validateBlocks(raw json.RawMessage, field string) error {
	if len(raw) == 0 || validateStrictJSON(raw) != nil || isJSONNull(raw) {
		return invalidRequest(RequestValidationCodeInvalidField, field)
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil || len(blocks) == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, field)
	}
	return nil
}
