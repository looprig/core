package v1

import (
	"encoding/json"
	"net/url"
	"strconv"
	"time"
)

// Declared member lists. Each record names its members exactly once; the
// marshaller, the decoder, and the extension capture all read the same slice.
var (
	hostLinkAttachRequestMembers       = []string{"version", "tenant_id", "session_id", "agent_id", "runtime_compatibility_id", "mode", "actor_id", "trace_id", "idempotency_key"}
	hostLinkBindRequestMembers         = []string{"version", "tenant_id", "session_id", "host_id", "host_generation", "lease_epoch", "runtime_compatibility_id", "idempotency_key"}
	hostLinkUnbindRequestMembers       = []string{"version", "tenant_id", "session_id", "host_id", "host_generation", "lease_epoch", "idempotency_key"}
	hostLinkCommandDeliveryMembers     = []string{"command_id"}
	hostLinkCapacityReportMembers      = []string{"version", "host_id", "host_generation", "agent_id", "runtime_compatibility_id", "placement", "internal_endpoint", "isolation_class", "accepting", "available_capacity", "observed_at", "expires_at"}
	hostLinkRegistryObservationMembers = []string{"version", "tenant_id", "session_id", "host_id", "host_generation", "agent_id", "runtime_compatibility_id", "placement", "internal_endpoint", "residency", "accepting", "lease_epoch", "observed_at", "expires_at"}
	hostLinkDrainRequestMembers        = []string{"version", "host_id", "host_generation", "idempotency_key", "tenant_id", "session_id"}
	hostLinkDrainObservationMembers    = []string{"host_id", "host_generation", "drain_generation", "state", "tenant_id", "session_id"}
	hostLinkErrorMembers               = []string{"code", "current_lease_epoch", "runtime_compatibility_id"}
	versionNegotiationRequestMembers   = []string{"supported_versions"}
	versionNegotiationResponseMembers  = []string{"version"}
)

// HostPlacement identifies the admission model for one Host advertisement.
// It is a bounded routing label, not a request to create or attach a session.
type HostPlacement string

const (
	HostPlacementPooled    HostPlacement = "pooled"
	HostPlacementDedicated HostPlacement = "dedicated"
)

// HostIsolationClass describes whether a Host target advertisement may accept
// sessions from different tenants. Pooled placement alone does not establish
// this boundary; Factory uses the advertised class when choosing capacity.
type HostIsolationClass string

const (
	HostIsolationClassCrossTenantIsolated HostIsolationClass = "cross_tenant_isolated"
	HostIsolationClassTenantExclusive     HostIsolationClass = "tenant_exclusive"
)

// InternalEndpoint is a non-secret WebSocket address for an authenticated
// HostLink. Credentials, signed query parameters, and fragments are prohibited;
// service authentication is carried by the HostLink adapter rather than this
// durable routing observation.
type InternalEndpoint string

// Validate reports whether endpoint is a bounded, credential-free WebSocket
// endpoint suitable for HostLink discovery. Every failure is reported as a
// *RequestValidationError naming the internal_endpoint member, so a caller can
// branch on Code instead of matching error text: an absent endpoint reports
// RequestValidationCodeMissingField and every malformed or credential-bearing
// spelling reports RequestValidationCodeInvalidField.
func (endpoint InternalEndpoint) Validate() error {
	if endpoint == "" {
		return invalidRequest(RequestValidationCodeMissingField, "internal_endpoint")
	}
	if err := validateID(string(endpoint)); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "internal_endpoint")
	}
	parsed, err := url.Parse(string(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Hostname() == "" || parsed.Opaque != "" {
		return invalidRequest(RequestValidationCodeInvalidField, "internal_endpoint")
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return invalidRequest(RequestValidationCodeInvalidField, "internal_endpoint")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return invalidRequest(RequestValidationCodeInvalidField, "internal_endpoint")
	}
	return nil
}

// HostLinkAttachMode says whether an attach creates a session or restores one
// over existing durable state. It is a closed set.
type HostLinkAttachMode string

const (
	// HostLinkAttachModeCreate launches a new session.
	HostLinkAttachModeCreate HostLinkAttachMode = "create"
	// HostLinkAttachModeRestore relaunches a session over its durable state.
	HostLinkAttachModeRestore HostLinkAttachMode = "restore"
)

// HostLinkAttachRequest asks a Host to make one session resident, sent as RPC
// method HostLinkMethodAttach. It carries no lease epoch: the attach is what
// acquires the lease and produces one.
//
// Mode rides the wire as the caller's statement of intent, not an instruction
// the Host obeys blindly: a Host refuses an attach whose mode contradicts the
// session's durable state. A restore of a session with nothing to restore from
// is refused runtime_unavailable. A create that meets existing durable state
// must still agree with that state's runtime build or is refused
// runtime_mismatch; whether a create may meet existing state at all is the
// create identity's idempotency question, not something this record decides.
//
// ActorID is the requesting SERVICE identity, not an end user. Placement may be
// driven by a sweeper or reconciler with no live user behind it, so the field
// names who asked for residency, never whose authority a later command carries.
// TraceID is optional correlation context and confers nothing.
//
// A Host answers an accepted attach with a HostLinkRegistryObservation, whose
// host_id, host_generation and lease_epoch are exactly what a following
// HostLinkBindRequest needs, so the caller can bind immediately with the
// returned epoch. A refused attach is answered with the existing HostLinkError
// vocabulary; the codes an attach may answer are:
//
//   - epoch_mismatch: the session lease is held by another owner, so the
//     registry the caller placed from was stale;
//   - runtime_mismatch: the runtime build or placement does not match what the
//     request, the target, or the durable state requires;
//   - no_capacity: the target's remaining admission capacity is too small;
//   - not_admitting: the Host is draining, or its isolation class forbids
//     admitting this tenant alongside the ones already resident;
//   - runtime_unavailable: the Host registers no target for the agent, the
//     session has no durable state to restore from, or the launched runtime is
//     unusable.
//
// A failure that is not a placement outcome (a store that failed, for example)
// carries no HostLinkError code at all.
type HostLinkAttachRequest struct {
	Version                WireVersion        `json:"version"`
	TenantID               TenantID           `json:"tenant_id"`
	SessionID              SessionID          `json:"session_id"`
	AgentID                AgentID            `json:"agent_id"`
	RuntimeCompatibilityID string             `json:"runtime_compatibility_id"`
	Mode                   HostLinkAttachMode `json:"mode"`
	ActorID                string             `json:"actor_id"`
	TraceID                string             `json:"trace_id,omitempty"`
	IdempotencyKey         string             `json:"idempotency_key"`
}

// Validate reports whether the attach request names a complete session, target
// runtime, closed mode, requesting service and retry-stable attach key.
func (r HostLinkAttachRequest) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkScope(r.TenantID, r.SessionID); err != nil {
		return err
	}
	if err := r.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if err := validateHostLinkOpaque(r.RuntimeCompatibilityID, "runtime_compatibility_id"); err != nil {
		return err
	}
	if err := validateHostLinkAttachMode(r.Mode); err != nil {
		return err
	}
	if err := validateHostLinkOpaque(r.ActorID, "actor_id"); err != nil {
		return err
	}
	if r.TraceID != "" {
		if err := validateHostLinkOpaque(r.TraceID, "trace_id"); err != nil {
			return err
		}
	}
	return validateHostLinkOpaque(r.IdempotencyKey, "idempotency_key")
}

func (r HostLinkAttachRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{
		"version":                  r.Version,
		"tenant_id":                r.TenantID,
		"session_id":               r.SessionID,
		"agent_id":                 r.AgentID,
		"runtime_compatibility_id": r.RuntimeCompatibilityID,
		"mode":                     r.Mode,
		"actor_id":                 r.ActorID,
		"idempotency_key":          r.IdempotencyKey,
	}
	if r.TraceID != "" {
		fields["trace_id"] = r.TraceID
	}
	return marshalHostLinkFields(hostLinkAttachRequestMembers, fields)
}

// UnmarshalJSON fails closed because an attach is authenticated control-plane
// input that acquires a lease; an unknown member is refused, not ignored.
func (r *HostLinkAttachRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkAttachRequestMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
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
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	runtimeCompatibilityID, err := decodeHostLinkOpaque(fields, "runtime_compatibility_id")
	if err != nil {
		return err
	}
	mode, err := decodeRequiredString(fields, "mode")
	if err != nil {
		return err
	}
	if err := validateHostLinkAttachMode(HostLinkAttachMode(mode)); err != nil {
		return err
	}
	actorID, err := decodeHostLinkOpaque(fields, "actor_id")
	if err != nil {
		return err
	}
	traceID, err := decodeOptionalHostLinkOpaque(fields, "trace_id")
	if err != nil {
		return err
	}
	idempotencyKey, err := decodeHostLinkOpaque(fields, "idempotency_key")
	if err != nil {
		return err
	}
	decoded := HostLinkAttachRequest{
		Version:                version,
		TenantID:               tenantID,
		SessionID:              sessionID,
		AgentID:                agentID,
		RuntimeCompatibilityID: runtimeCompatibilityID,
		Mode:                   HostLinkAttachMode(mode),
		ActorID:                actorID,
		TraceID:                traceID,
		IdempotencyKey:         idempotencyKey,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkBindRequest establishes one Factory-local route to a Host-owned
// session. It is a routing optimization, not proof of ownership: the Host
// validates the current tuple against its durable lease before accepting it.
type HostLinkBindRequest struct {
	Version                WireVersion `json:"version"`
	TenantID               TenantID    `json:"tenant_id"`
	SessionID              SessionID   `json:"session_id"`
	HostID                 HostID      `json:"host_id"`
	HostGeneration         uint64      `json:"host_generation"`
	LeaseEpoch             uint64      `json:"lease_epoch"`
	RuntimeCompatibilityID string      `json:"runtime_compatibility_id"`
	IdempotencyKey         string      `json:"idempotency_key"`
}

// Validate reports whether the bind request carries the complete current
// ownership tuple and a retry-stable bind key.
func (r HostLinkBindRequest) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkScope(r.TenantID, r.SessionID); err != nil {
		return err
	}
	if err := validateHostLinkHost(r.HostID, r.HostGeneration); err != nil {
		return err
	}
	if r.LeaseEpoch == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "lease_epoch")
	}
	if err := validateHostLinkOpaque(r.RuntimeCompatibilityID, "runtime_compatibility_id"); err != nil {
		return err
	}
	return validateHostLinkOpaque(r.IdempotencyKey, "idempotency_key")
}

func (r HostLinkBindRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalHostLinkFields(hostLinkBindRequestMembers, map[string]any{
		"version":                  r.Version,
		"tenant_id":                r.TenantID,
		"session_id":               r.SessionID,
		"host_id":                  r.HostID,
		"host_generation":          r.HostGeneration,
		"lease_epoch":              r.LeaseEpoch,
		"runtime_compatibility_id": r.RuntimeCompatibilityID,
		"idempotency_key":          r.IdempotencyKey,
	})
}

// UnmarshalJSON fails closed because a bind is authenticated control-plane
// input; unknown members cannot become a future attach/create workflow.
func (r *HostLinkBindRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkBindRequestMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
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
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	leaseEpoch, err := decodeRequiredNonZeroUint64(fields, "lease_epoch")
	if err != nil {
		return err
	}
	runtimeCompatibilityID, err := decodeHostLinkOpaque(fields, "runtime_compatibility_id")
	if err != nil {
		return err
	}
	idempotencyKey, err := decodeHostLinkOpaque(fields, "idempotency_key")
	if err != nil {
		return err
	}
	decoded := HostLinkBindRequest{
		Version:                version,
		TenantID:               tenantID,
		SessionID:              sessionID,
		HostID:                 hostID,
		HostGeneration:         hostGeneration,
		LeaseEpoch:             leaseEpoch,
		RuntimeCompatibilityID: runtimeCompatibilityID,
		IdempotencyKey:         idempotencyKey,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkUnbindRequest removes one Factory-local route. Repeating the same
// tuple/key is safe; it never releases the Host's lease or durable session.
type HostLinkUnbindRequest struct {
	Version        WireVersion `json:"version"`
	TenantID       TenantID    `json:"tenant_id"`
	SessionID      SessionID   `json:"session_id"`
	HostID         HostID      `json:"host_id"`
	HostGeneration uint64      `json:"host_generation"`
	LeaseEpoch     uint64      `json:"lease_epoch"`
	IdempotencyKey string      `json:"idempotency_key"`
}

// Validate reports whether the unbind request identifies a bound route.
func (r HostLinkUnbindRequest) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkScope(r.TenantID, r.SessionID); err != nil {
		return err
	}
	if err := validateHostLinkHost(r.HostID, r.HostGeneration); err != nil {
		return err
	}
	if r.LeaseEpoch == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "lease_epoch")
	}
	return validateHostLinkOpaque(r.IdempotencyKey, "idempotency_key")
}

func (r HostLinkUnbindRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalHostLinkFields(hostLinkUnbindRequestMembers, map[string]any{
		"version":         r.Version,
		"tenant_id":       r.TenantID,
		"session_id":      r.SessionID,
		"host_id":         r.HostID,
		"host_generation": r.HostGeneration,
		"lease_epoch":     r.LeaseEpoch,
		"idempotency_key": r.IdempotencyKey,
	})
}

func (r *HostLinkUnbindRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkUnbindRequestMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
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
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	leaseEpoch, err := decodeRequiredNonZeroUint64(fields, "lease_epoch")
	if err != nil {
		return err
	}
	idempotencyKey, err := decodeHostLinkOpaque(fields, "idempotency_key")
	if err != nil {
		return err
	}
	decoded := HostLinkUnbindRequest{
		Version:        version,
		TenantID:       tenantID,
		SessionID:      sessionID,
		HostID:         hostID,
		HostGeneration: hostGeneration,
		LeaseEpoch:     leaseEpoch,
		IdempotencyKey: idempotencyKey,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkCommandDelivery wakes consumption of an already accepted command on
// an existing binding. Its only body member is the public, retry-stable
// CommandID: runtime UUIDs, input blocks, gate answers, and other private
// payloads remain in the durable inbox and never cross this link.
type HostLinkCommandDelivery struct {
	CommandID CommandID `json:"command_id"`
}

// Validate reports whether the delivery contains a usable public command ID.
func (d HostLinkCommandDelivery) Validate() error {
	if err := d.CommandID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "command_id")
	}
	return nil
}

func (d HostLinkCommandDelivery) MarshalJSON() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return marshalHostLinkFields(hostLinkCommandDeliveryMembers, map[string]any{"command_id": d.CommandID})
}

func (d *HostLinkCommandDelivery) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkCommandDeliveryMembers...)
	if err != nil {
		return err
	}
	commandID, err := decodeCommandID(fields, "command_id")
	if err != nil {
		return err
	}
	decoded := HostLinkCommandDelivery{CommandID: commandID}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*d = decoded
	return nil
}

// HostLinkCapacityReport is a Host's current target-advertisement observation.
// AvailableCapacity may be zero; that is a valid observation a Factory uses to
// avoid new placement while still maintaining existing HostLinks.
type HostLinkCapacityReport struct {
	Version                WireVersion        `json:"version"`
	HostID                 HostID             `json:"host_id"`
	HostGeneration         uint64             `json:"host_generation"`
	AgentID                AgentID            `json:"agent_id"`
	RuntimeCompatibilityID string             `json:"runtime_compatibility_id"`
	Placement              HostPlacement      `json:"placement"`
	InternalEndpoint       InternalEndpoint   `json:"internal_endpoint"`
	IsolationClass         HostIsolationClass `json:"isolation_class"`
	Accepting              bool               `json:"accepting"`
	AvailableCapacity      uint64             `json:"available_capacity"`
	ObservedAt             time.Time          `json:"observed_at"`
	ExpiresAt              time.Time          `json:"expires_at"`
}

// Validate reports whether the report is a fresh, bounded-capacity observation.
func (r HostLinkCapacityReport) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkHost(r.HostID, r.HostGeneration); err != nil {
		return err
	}
	if err := r.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if err := validateHostLinkOpaque(r.RuntimeCompatibilityID, "runtime_compatibility_id"); err != nil {
		return err
	}
	if err := validateHostLinkPlacement(r.Placement); err != nil {
		return err
	}
	if err := validateHostLinkEndpoint(r.InternalEndpoint); err != nil {
		return err
	}
	if err := validateHostIsolationClass(r.IsolationClass); err != nil {
		return err
	}
	if r.Placement == HostPlacementDedicated && r.AvailableCapacity > 1 {
		return invalidRequest(RequestValidationCodeInvalidField, "available_capacity")
	}
	return validateHostLinkTimes(r.ObservedAt, r.ExpiresAt)
}

func (r HostLinkCapacityReport) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalHostLinkFields(hostLinkCapacityReportMembers, map[string]any{
		"version":                  r.Version,
		"host_id":                  r.HostID,
		"host_generation":          r.HostGeneration,
		"agent_id":                 r.AgentID,
		"runtime_compatibility_id": r.RuntimeCompatibilityID,
		"placement":                r.Placement,
		"internal_endpoint":        r.InternalEndpoint,
		"isolation_class":          r.IsolationClass,
		"accepting":                r.Accepting,
		"available_capacity":       r.AvailableCapacity,
		"observed_at":              r.ObservedAt.UTC(),
		"expires_at":               r.ExpiresAt.UTC(),
	})
}

func (r *HostLinkCapacityReport) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkCapacityReportMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
		return err
	}
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	runtimeCompatibilityID, err := decodeHostLinkOpaque(fields, "runtime_compatibility_id")
	if err != nil {
		return err
	}
	placement, err := decodeHostLinkPlacement(fields)
	if err != nil {
		return err
	}
	internalEndpoint, err := decodeHostLinkEndpoint(fields)
	if err != nil {
		return err
	}
	isolationClass, err := decodeHostIsolationClass(fields)
	if err != nil {
		return err
	}
	accepting, err := decodeRequiredBool(fields, "accepting")
	if err != nil {
		return err
	}
	availableCapacity, err := decodeRequiredUint64(fields, "available_capacity")
	if err != nil {
		return err
	}
	observedAt, err := decodeRequiredTime(fields, "observed_at")
	if err != nil {
		return err
	}
	expiresAt, err := decodeRequiredTime(fields, "expires_at")
	if err != nil {
		return err
	}
	decoded := HostLinkCapacityReport{
		Version:                version,
		HostID:                 hostID,
		HostGeneration:         hostGeneration,
		AgentID:                agentID,
		RuntimeCompatibilityID: runtimeCompatibilityID,
		Placement:              placement,
		InternalEndpoint:       internalEndpoint,
		IsolationClass:         isolationClass,
		Accepting:              accepting,
		AvailableCapacity:      availableCapacity,
		ObservedAt:             observedAt,
		ExpiresAt:              expiresAt,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkRegistryObservation is a current Host-owned session location hint.
// It is not ownership proof: SessionStore's lease and epoch fence remain
// authoritative even while the observation is fresh.
type HostLinkRegistryObservation struct {
	Version                WireVersion      `json:"version"`
	TenantID               TenantID         `json:"tenant_id"`
	SessionID              SessionID        `json:"session_id"`
	HostID                 HostID           `json:"host_id"`
	HostGeneration         uint64           `json:"host_generation"`
	AgentID                AgentID          `json:"agent_id"`
	RuntimeCompatibilityID string           `json:"runtime_compatibility_id"`
	Placement              HostPlacement    `json:"placement"`
	InternalEndpoint       InternalEndpoint `json:"internal_endpoint"`
	Residency              SessionResidency `json:"residency"`
	Accepting              bool             `json:"accepting"`
	LeaseEpoch             uint64           `json:"lease_epoch"`
	ObservedAt             time.Time        `json:"observed_at"`
	ExpiresAt              time.Time        `json:"expires_at"`
}

// Validate reports whether the per-session route observation has a current
// lease tuple and an expiry after its observation time.
func (r HostLinkRegistryObservation) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkScope(r.TenantID, r.SessionID); err != nil {
		return err
	}
	if err := validateHostLinkHost(r.HostID, r.HostGeneration); err != nil {
		return err
	}
	if err := r.AgentID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "agent_id")
	}
	if err := validateHostLinkOpaque(r.RuntimeCompatibilityID, "runtime_compatibility_id"); err != nil {
		return err
	}
	if err := validateHostLinkPlacement(r.Placement); err != nil {
		return err
	}
	if err := validateHostLinkEndpoint(r.InternalEndpoint); err != nil {
		return err
	}
	if !validHostLinkRegistryResidency(r.Residency) {
		return invalidRequest(RequestValidationCodeInvalidField, "residency")
	}
	if r.LeaseEpoch == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "lease_epoch")
	}
	return validateHostLinkTimes(r.ObservedAt, r.ExpiresAt)
}

func (r HostLinkRegistryObservation) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return marshalHostLinkFields(hostLinkRegistryObservationMembers, map[string]any{
		"version":                  r.Version,
		"tenant_id":                r.TenantID,
		"session_id":               r.SessionID,
		"host_id":                  r.HostID,
		"host_generation":          r.HostGeneration,
		"agent_id":                 r.AgentID,
		"runtime_compatibility_id": r.RuntimeCompatibilityID,
		"placement":                r.Placement,
		"internal_endpoint":        r.InternalEndpoint,
		"residency":                r.Residency,
		"accepting":                r.Accepting,
		"lease_epoch":              r.LeaseEpoch,
		"observed_at":              r.ObservedAt.UTC(),
		"expires_at":               r.ExpiresAt.UTC(),
	})
}

func (r *HostLinkRegistryObservation) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkRegistryObservationMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
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
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	agentID, err := decodeAgentID(fields, "agent_id")
	if err != nil {
		return err
	}
	runtimeCompatibilityID, err := decodeHostLinkOpaque(fields, "runtime_compatibility_id")
	if err != nil {
		return err
	}
	placement, err := decodeHostLinkPlacement(fields)
	if err != nil {
		return err
	}
	internalEndpoint, err := decodeHostLinkEndpoint(fields)
	if err != nil {
		return err
	}
	residency, err := decodeRequiredString(fields, "residency")
	if err != nil {
		return err
	}
	accepting, err := decodeRequiredBool(fields, "accepting")
	if err != nil {
		return err
	}
	leaseEpoch, err := decodeRequiredNonZeroUint64(fields, "lease_epoch")
	if err != nil {
		return err
	}
	observedAt, err := decodeRequiredTime(fields, "observed_at")
	if err != nil {
		return err
	}
	expiresAt, err := decodeRequiredTime(fields, "expires_at")
	if err != nil {
		return err
	}
	decoded := HostLinkRegistryObservation{
		Version:                version,
		TenantID:               tenantID,
		SessionID:              sessionID,
		HostID:                 hostID,
		HostGeneration:         hostGeneration,
		AgentID:                agentID,
		RuntimeCompatibilityID: runtimeCompatibilityID,
		Placement:              placement,
		InternalEndpoint:       internalEndpoint,
		Residency:              SessionResidency(residency),
		Accepting:              accepting,
		LeaseEpoch:             leaseEpoch,
		ObservedAt:             observedAt,
		ExpiresAt:              expiresAt,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkDrainRequest starts idempotent Host draining. A missing tenant/session
// scope means whole-Host drain; both present scopes identify the fixed session of
// a dedicated Host. This request does not contain user authority or a deadline.
type HostLinkDrainRequest struct {
	Version        WireVersion `json:"version"`
	HostID         HostID      `json:"host_id"`
	HostGeneration uint64      `json:"host_generation"`
	IdempotencyKey string      `json:"idempotency_key"`
	TenantID       TenantID    `json:"tenant_id,omitempty"`
	SessionID      SessionID   `json:"session_id,omitempty"`
}

// Validate reports whether the request has an exact whole-host or dedicated
// session scope and a retry-stable initiation key.
func (r HostLinkDrainRequest) Validate() error {
	if err := validateHostLinkVersion(r.Version); err != nil {
		return err
	}
	if err := validateHostLinkHost(r.HostID, r.HostGeneration); err != nil {
		return err
	}
	if err := validateOptionalHostLinkScope(r.TenantID, r.SessionID); err != nil {
		return err
	}
	return validateHostLinkOpaque(r.IdempotencyKey, "idempotency_key")
}

func (r HostLinkDrainRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{
		"version":         r.Version,
		"host_id":         r.HostID,
		"host_generation": r.HostGeneration,
		"idempotency_key": r.IdempotencyKey,
	}
	if r.TenantID != "" {
		fields["tenant_id"] = r.TenantID
		fields["session_id"] = r.SessionID
	}
	return marshalHostLinkFields(hostLinkDrainRequestMembers, fields)
}

func (r *HostLinkDrainRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkDrainRequestMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
		return err
	}
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	idempotencyKey, err := decodeHostLinkOpaque(fields, "idempotency_key")
	if err != nil {
		return err
	}
	tenantID, err := decodeOptionalTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeOptionalSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	decoded := HostLinkDrainRequest{
		Version:        version,
		HostID:         hostID,
		HostGeneration: hostGeneration,
		IdempotencyKey: idempotencyKey,
		TenantID:       tenantID,
		SessionID:      sessionID,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// HostLinkDrainState is the bounded public result of a drain observation.
type HostLinkDrainState string

const (
	// HostLinkDrainStateDraining means admission is stopped and durable release
	// work is in progress.
	HostLinkDrainStateDraining HostLinkDrainState = "draining"
	// HostLinkDrainStateDrained means the observed scope has finished release.
	HostLinkDrainStateDrained HostLinkDrainState = "drained"
)

func (s HostLinkDrainState) valid() bool {
	switch s {
	case HostLinkDrainStateDraining, HostLinkDrainStateDrained:
		return true
	default:
		return false
	}
}

// HostLinkDrainObservation acknowledges a drain initiation and exposes its
// stable generation/state for a Factory to observe without inferring release
// completion from a transport close.
type HostLinkDrainObservation struct {
	HostID          HostID             `json:"host_id"`
	HostGeneration  uint64             `json:"host_generation"`
	DrainGeneration uint64             `json:"drain_generation"`
	State           HostLinkDrainState `json:"state"`
	TenantID        TenantID           `json:"tenant_id,omitempty"`
	SessionID       SessionID          `json:"session_id,omitempty"`
}

// Validate reports whether a drain status describes one Host/scope generation.
func (o HostLinkDrainObservation) Validate() error {
	if err := validateHostLinkHost(o.HostID, o.HostGeneration); err != nil {
		return err
	}
	if o.DrainGeneration == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "drain_generation")
	}
	if !o.State.valid() {
		return invalidRequest(RequestValidationCodeInvalidField, "state")
	}
	return validateOptionalHostLinkScope(o.TenantID, o.SessionID)
}

func (o HostLinkDrainObservation) MarshalJSON() ([]byte, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{
		"host_id":          o.HostID,
		"host_generation":  o.HostGeneration,
		"drain_generation": o.DrainGeneration,
		"state":            o.State,
	}
	if o.TenantID != "" {
		fields["tenant_id"] = o.TenantID
		fields["session_id"] = o.SessionID
	}
	return marshalHostLinkFields(hostLinkDrainObservationMembers, fields)
}

// UnmarshalJSON stays strict because this internal observation is a control
// boundary, not a public projection that may proxy additive fields.
func (o *HostLinkDrainObservation) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkDrainObservationMembers...)
	if err != nil {
		return err
	}
	hostID, err := decodeHostID(fields, "host_id")
	if err != nil {
		return err
	}
	hostGeneration, err := decodeRequiredNonZeroUint64(fields, "host_generation")
	if err != nil {
		return err
	}
	drainGeneration, err := decodeRequiredNonZeroUint64(fields, "drain_generation")
	if err != nil {
		return err
	}
	state, err := decodeRequiredString(fields, "state")
	if err != nil {
		return err
	}
	tenantID, err := decodeOptionalTenantID(fields, "tenant_id")
	if err != nil {
		return err
	}
	sessionID, err := decodeOptionalSessionID(fields, "session_id")
	if err != nil {
		return err
	}
	decoded := HostLinkDrainObservation{
		HostID:          hostID,
		HostGeneration:  hostGeneration,
		DrainGeneration: drainGeneration,
		State:           HostLinkDrainState(state),
		TenantID:        tenantID,
		SessionID:       sessionID,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*o = decoded
	return nil
}

// HostLinkErrorCode is a stable HostLink control-plane refusal class. It has no
// provider error string or runtime payload, so callers can act without parsing
// opaque internals.
type HostLinkErrorCode string

const (
	HostLinkErrorNotAdmitting       HostLinkErrorCode = "not_admitting"
	HostLinkErrorReleasing          HostLinkErrorCode = "releasing"
	HostLinkErrorNoCapacity         HostLinkErrorCode = "no_capacity"
	HostLinkErrorEpochMismatch      HostLinkErrorCode = "epoch_mismatch"
	HostLinkErrorRuntimeUnavailable HostLinkErrorCode = "runtime_unavailable"
	HostLinkErrorRuntimeMismatch    HostLinkErrorCode = "runtime_mismatch"
)

func (c HostLinkErrorCode) valid() bool {
	switch c {
	case HostLinkErrorNotAdmitting, HostLinkErrorReleasing, HostLinkErrorNoCapacity,
		HostLinkErrorEpochMismatch, HostLinkErrorRuntimeUnavailable, HostLinkErrorRuntimeMismatch:
		return true
	default:
		return false
	}
}

// HostLinkError is a safe typed HostLink refusal. CurrentLeaseEpoch is present
// only for an epoch mismatch; RuntimeCompatibilityID identifies the compatible
// boundary in a runtime mismatch, never credentials or a raw runtime payload.
type HostLinkError struct {
	Code                   HostLinkErrorCode `json:"code"`
	CurrentLeaseEpoch      uint64            `json:"current_lease_epoch,omitempty"`
	RuntimeCompatibilityID string            `json:"runtime_compatibility_id,omitempty"`
}

// Validate reports whether error-specific safe details match its stable code.
func (e HostLinkError) Validate() error {
	if !e.Code.valid() {
		return invalidRequest(RequestValidationCodeInvalidField, "code")
	}
	if e.Code == HostLinkErrorEpochMismatch {
		if e.CurrentLeaseEpoch == 0 {
			return invalidRequest(RequestValidationCodeMissingField, "current_lease_epoch")
		}
	} else if e.CurrentLeaseEpoch != 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "current_lease_epoch")
	}
	if e.Code == HostLinkErrorRuntimeMismatch {
		if e.RuntimeCompatibilityID == "" {
			return invalidRequest(RequestValidationCodeMissingField, "runtime_compatibility_id")
		}
		if err := validateHostLinkOpaque(e.RuntimeCompatibilityID, "runtime_compatibility_id"); err != nil {
			return err
		}
	} else if e.RuntimeCompatibilityID != "" {
		return invalidRequest(RequestValidationCodeInvalidField, "runtime_compatibility_id")
	}
	return nil
}

func (e HostLinkError) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]any{"code": e.Code}
	if e.CurrentLeaseEpoch != 0 {
		fields["current_lease_epoch"] = e.CurrentLeaseEpoch
	}
	if e.RuntimeCompatibilityID != "" {
		fields["runtime_compatibility_id"] = e.RuntimeCompatibilityID
	}
	return marshalHostLinkFields(hostLinkErrorMembers, fields)
}

func (e *HostLinkError) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, hostLinkErrorMembers...)
	if err != nil {
		return err
	}
	code, err := decodeRequiredString(fields, "code")
	if err != nil {
		return err
	}
	_, currentLeaseEpochPresent := fields["current_lease_epoch"]
	currentLeaseEpoch, err := decodeOptionalUint64(fields, "current_lease_epoch")
	if err != nil {
		return err
	}
	if currentLeaseEpochPresent && currentLeaseEpoch == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "current_lease_epoch")
	}
	runtimeCompatibilityID, err := decodeOptionalHostLinkOpaque(fields, "runtime_compatibility_id")
	if err != nil {
		return err
	}
	decoded := HostLinkError{
		Code:                   HostLinkErrorCode(code),
		CurrentLeaseEpoch:      currentLeaseEpoch,
		RuntimeCompatibilityID: runtimeCompatibilityID,
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*e = decoded
	return nil
}

// VersionNegotiationRequest advertises the sessionwire versions a peer can
// speak before any versioned HostLink control records are accepted.
type VersionNegotiationRequest struct {
	SupportedVersions []WireVersion `json:"supported_versions"`
}

// Validate reports whether the peer offered a non-empty, duplicate-free set.
func (r VersionNegotiationRequest) Validate() error {
	if len(r.SupportedVersions) == 0 {
		return invalidRequest(RequestValidationCodeMissingField, "supported_versions")
	}
	seen := make(map[WireVersion]struct{}, len(r.SupportedVersions))
	for _, version := range r.SupportedVersions {
		if version == 0 {
			return invalidRequest(RequestValidationCodeInvalidField, "supported_versions")
		}
		if _, duplicate := seen[version]; duplicate {
			return invalidRequest(RequestValidationCodeInvalidField, "supported_versions")
		}
		seen[version] = struct{}{}
	}
	return nil
}

func (r VersionNegotiationRequest) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	// WireVersion is an uint8. encoding/json treats a []uint8 as binary data
	// and emits base64, so widen the advertised list to JSON numbers.
	versions := make([]uint16, len(r.SupportedVersions))
	for index, version := range r.SupportedVersions {
		versions[index] = uint16(version)
	}
	return marshalHostLinkFields(versionNegotiationRequestMembers, map[string]any{"supported_versions": versions})
}

func (r *VersionNegotiationRequest) UnmarshalJSON(data []byte) error {
	fields, err := decodeRequestFields(data, versionNegotiationRequestMembers...)
	if err != nil {
		return err
	}
	rawVersions, err := decodeRequiredRawField(fields, "supported_versions")
	if err != nil {
		return err
	}
	// WireVersion is a uint8, so a []WireVersion is a []byte to encoding/json:
	// decoding a JSON *string* into one takes the base64 path and would accept
	// "AQ==" as [1] on the very first record a HostLink exchanges. Decode the
	// advertised list as JSON numbers and range-check each one back down.
	var rawNumbers []json.Number
	if err := json.Unmarshal(rawVersions, &rawNumbers); err != nil || rawNumbers == nil {
		return invalidRequest(RequestValidationCodeInvalidField, "supported_versions")
	}
	versions := make([]WireVersion, 0, len(rawNumbers))
	for _, number := range rawNumbers {
		value, err := strconv.ParseUint(number.String(), 10, 8)
		if err != nil {
			return invalidRequest(RequestValidationCodeInvalidField, "supported_versions")
		}
		versions = append(versions, WireVersion(value))
	}
	decoded := VersionNegotiationRequest{SupportedVersions: versions}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// VersionNegotiationResponse selects the one current sessionwire version both
// ends use. Unknown additive response fields are retained for public ClientLink
// negotiation proxies; HostLink control requests remain strict.
type VersionNegotiationResponse struct {
	Version WireVersion `json:"version"`

	extensions responseExtensions
}

// AdditionalFields returns copies of forward-compatible response members.
func (r VersionNegotiationResponse) AdditionalFields() map[string]json.RawMessage {
	return r.extensions.copy()
}

// Validate reports whether r selects this Core implementation's supported V1.
func (r VersionNegotiationResponse) Validate() error {
	return validateHostLinkVersion(r.Version)
}

func (r VersionNegotiationResponse) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := putJSONField(fields, "version", r.Version); err != nil {
		return nil, err
	}
	return marshalResponseFields(versionNegotiationResponseMembers, fields, r.extensions)
}

func (r *VersionNegotiationResponse) UnmarshalJSON(data []byte) error {
	fields, err := decodeContractFields(data, versionNegotiationResponseMembers...)
	if err != nil {
		return err
	}
	version, err := decodeHostLinkVersion(fields)
	if err != nil {
		return err
	}
	decoded := VersionNegotiationResponse{
		Version:    version,
		extensions: captureExtensions(fields, versionNegotiationResponseMembers...),
	}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*r = decoded
	return nil
}

// NegotiateVersion selects CurrentWireVersion when the peer advertises it.
// Callers can serialize the returned response or return the stable unsupported
// version validation error without depending on a transport library.
func NegotiateVersion(request VersionNegotiationRequest) (VersionNegotiationResponse, error) {
	if err := request.Validate(); err != nil {
		return VersionNegotiationResponse{}, err
	}
	for _, version := range request.SupportedVersions {
		if version == CurrentWireVersion {
			return VersionNegotiationResponse{Version: CurrentWireVersion}, nil
		}
	}
	return VersionNegotiationResponse{}, invalidRequest(RequestValidationCodeUnsupportedVersion, "supported_versions")
}

func validateHostLinkVersion(version WireVersion) error {
	if version != CurrentWireVersion {
		return invalidRequest(RequestValidationCodeUnsupportedVersion, "version")
	}
	return nil
}

func validateHostLinkScope(tenantID TenantID, sessionID SessionID) error {
	if err := tenantID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "tenant_id")
	}
	if err := sessionID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "session_id")
	}
	return nil
}

func validateOptionalHostLinkScope(tenantID TenantID, sessionID SessionID) error {
	if (tenantID == "") != (sessionID == "") {
		return invalidRequest(RequestValidationCodeInvalidField, "session_scope")
	}
	if tenantID == "" {
		return nil
	}
	return validateHostLinkScope(tenantID, sessionID)
}

func validateHostLinkHost(hostID HostID, hostGeneration uint64) error {
	if err := hostID.Validate(); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, "host_id")
	}
	if hostGeneration == 0 {
		return invalidRequest(RequestValidationCodeInvalidField, "host_generation")
	}
	return nil
}

func validateHostLinkOpaque(value, field string) error {
	if err := validateID(value); err != nil {
		return invalidRequest(RequestValidationCodeInvalidField, field)
	}
	return nil
}

func validateHostLinkPlacement(placement HostPlacement) error {
	switch placement {
	case HostPlacementPooled, HostPlacementDedicated:
		return nil
	default:
		return invalidRequest(RequestValidationCodeInvalidField, "placement")
	}
}

func validateHostLinkAttachMode(mode HostLinkAttachMode) error {
	switch mode {
	case HostLinkAttachModeCreate, HostLinkAttachModeRestore:
		return nil
	default:
		return invalidRequest(RequestValidationCodeInvalidField, "mode")
	}
}

func validateHostIsolationClass(class HostIsolationClass) error {
	switch class {
	case HostIsolationClassCrossTenantIsolated, HostIsolationClassTenantExclusive:
		return nil
	default:
		return invalidRequest(RequestValidationCodeInvalidField, "isolation_class")
	}
}

// validateHostLinkEndpoint forwards to InternalEndpoint.Validate, which already
// reports the stable internal_endpoint code pair this record needs.
func validateHostLinkEndpoint(endpoint InternalEndpoint) error {
	return endpoint.Validate()
}

func validateHostLinkTimes(observedAt, expiresAt time.Time) error {
	if observedAt.IsZero() {
		return invalidRequest(RequestValidationCodeMissingField, "observed_at")
	}
	if expiresAt.IsZero() || !expiresAt.After(observedAt) {
		return invalidRequest(RequestValidationCodeInvalidField, "expires_at")
	}
	return nil
}

func validHostLinkRegistryResidency(residency SessionResidency) bool {
	switch residency {
	case SessionResidencyAttaching, SessionResidencyResident, SessionResidencyReleasing:
		return true
	default:
		return false
	}
}

// marshalHostLinkFields emits one strict HostLink control record. HostLink
// records carry no forward-compatible extensions, so the only thing left to
// check is that every emitted member is one the record declares.
func marshalHostLinkFields(members []string, values map[string]any) ([]byte, error) {
	fields := make(map[string]json.RawMessage, len(values))
	for name, value := range values {
		if err := putJSONField(fields, name, value); err != nil {
			return nil, err
		}
	}
	return marshalResponseFields(members, fields, responseExtensions{})
}

func decodeHostLinkVersion(fields map[string]json.RawMessage) (WireVersion, error) {
	raw, ok := fields["version"]
	if !ok || isJSONNull(raw) {
		return 0, invalidRequest(RequestValidationCodeMissingField, "version")
	}
	var version WireVersion
	if err := json.Unmarshal(raw, &version); err != nil {
		return 0, invalidRequest(RequestValidationCodeInvalidField, "version")
	}
	if err := validateHostLinkVersion(version); err != nil {
		return 0, err
	}
	return version, nil
}

func decodeHostID(fields map[string]json.RawMessage, name string) (HostID, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	id := HostID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeRequiredNonZeroUint64(fields map[string]json.RawMessage, name string) (uint64, error) {
	value, err := decodeRequiredUint64(fields, name)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeRequiredBool(fields map[string]json.RawMessage, name string) (bool, error) {
	raw, ok := fields[name]
	if !ok || isJSONNull(raw) {
		return false, invalidRequest(RequestValidationCodeMissingField, name)
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return value, nil
}

func decodeHostLinkOpaque(fields map[string]json.RawMessage, name string) (string, error) {
	value, err := decodeRequiredString(fields, name)
	if err != nil {
		return "", err
	}
	if err := validateHostLinkOpaque(value, name); err != nil {
		return "", err
	}
	return value, nil
}

func decodeHostLinkPlacement(fields map[string]json.RawMessage) (HostPlacement, error) {
	value, err := decodeRequiredString(fields, "placement")
	if err != nil {
		return "", err
	}
	placement := HostPlacement(value)
	if err := validateHostLinkPlacement(placement); err != nil {
		return "", err
	}
	return placement, nil
}

func decodeHostIsolationClass(fields map[string]json.RawMessage) (HostIsolationClass, error) {
	value, err := decodeRequiredString(fields, "isolation_class")
	if err != nil {
		return "", err
	}
	class := HostIsolationClass(value)
	if err := validateHostIsolationClass(class); err != nil {
		return "", err
	}
	return class, nil
}

func decodeHostLinkEndpoint(fields map[string]json.RawMessage) (InternalEndpoint, error) {
	value, err := decodeRequiredString(fields, "internal_endpoint")
	if err != nil {
		return "", err
	}
	endpoint := InternalEndpoint(value)
	if err := validateHostLinkEndpoint(endpoint); err != nil {
		return "", err
	}
	return endpoint, nil
}

func decodeOptionalHostLinkOpaque(fields map[string]json.RawMessage, name string) (string, error) {
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
	if err := validateHostLinkOpaque(value, name); err != nil {
		return "", err
	}
	return value, nil
}

func decodeOptionalTenantID(fields map[string]json.RawMessage, name string) (TenantID, error) {
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
	id := TenantID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}

func decodeOptionalSessionID(fields map[string]json.RawMessage, name string) (SessionID, error) {
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
	id := SessionID(value)
	if err := id.Validate(); err != nil {
		return "", invalidRequest(RequestValidationCodeInvalidField, name)
	}
	return id, nil
}
