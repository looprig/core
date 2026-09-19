package v1

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
)

// HostLink framing names. These are plain strings shared by the Factory and
// Host ends of a HostLink; no transport type or adapter enters Core.
//
// # Connect framing
//
// A HostLink begins with one version negotiation, carried in the transport's
// connect exchange. Its framing is frozen here because the two ends once
// disagreed on it: Factory wrapped both records as
// {"version_negotiation":{...}} while Host sent and expected them bare. That
// disagreement had two failure modes, one behind the other. Against a shipped
// Host the connect failed LOUDLY: Host's strict request decoder refused the
// wrapped Data as an unknown member and disconnected with "unsupported wire
// version". Had a Host accepted the wrapper instead, the failure would have
// been SILENT on Factory's side: its wrapped decoder reads the bare reply
// {"version":1} through a struct expecting the wrapper, finds no
// version_negotiation member, and sees version 0. Both modes are pinned as
// refusals by the helpers below.
//
// The capability signal is deliberately one-directional. Every reserved
// HostLink method is Factory→Host, so only the reply carries hostlink_methods;
// there is no Host-side gate a Factory→Host capability would inform. It also
// could not be added additively: VersionNegotiationRequest is STRICT, so a new
// request member is refused by every Host already shipped, before the Factory
// can learn that Host's version. If a request-side capability is ever wanted,
// the Host-side request decoder must first be relaxed to tolerate it in an
// earlier release, and only then may Factories begin emitting it.
//
//   - The connect request's Data is the BARE VersionNegotiationRequest:
//     {"supported_versions":[1]}. No wrapper, no envelope member.
//   - The connect reply's Data is the BARE VersionNegotiationResponse:
//     {"version":1} at minimum, plus "hostlink_methods" when the Host
//     advertises capabilities.
//
// EncodeHostLinkConnectRequest, DecodeHostLinkConnectRequest,
// EncodeHostLinkConnectReply and DecodeHostLinkConnectReply are those two
// shapes by name. They carry no transport type: each takes or returns the
// Core record and the raw bytes a transport's connect event or reply carries.
// Either end may equally call encoding/json on the record directly; the
// helpers exist so the framing is a named API a test can pin rather than a
// convention a module can drift from.
//
// A HostLink RPC names either a reserved method or a channel. The reserved
// methods below are matched exactly, and none begins with
// HostLinkChannelPrefix, so a receiver can tell a method from a channel without
// parsing either.
//
// COMMAND DELIVERY HAS NO METHOD NAME OF ITS OWN: its RPC method IS the bound
// session's channel, HostLinkChannel(tenant, session), and its body is a
// HostLinkCommandDelivery. Any method that is not reserved is resolved as a
// channel against the routes the link holds, so a delivery names its binding
// by the method alone.
const (
	// HostLinkMethodBind carries a HostLinkBindRequest.
	HostLinkMethodBind = "hostlink.bind"
	// HostLinkMethodUnbind carries a HostLinkUnbindRequest.
	HostLinkMethodUnbind = "hostlink.unbind"
	// HostLinkMethodAttach carries a HostLinkAttachRequest.
	HostLinkMethodAttach = "hostlink.attach"
	// HostLinkMethodDrain carries a HostLinkDrainRequest and initiates a drain.
	HostLinkMethodDrain = "hostlink.drain"
	// HostLinkMethodDrainStatus observes a drain without initiating one.
	HostLinkMethodDrainStatus = "hostlink.drain_status"
)

// HostLinkChannelPrefix is the leading segment of every HostLink session
// channel.
const HostLinkChannelPrefix = "hostlink.v1."

// HostLinkChannel returns the channel one session's HostLink publications and
// command deliveries travel on: HostLinkChannelPrefix, the tenant ID, a '.',
// and the session ID, each identifier base64url-encoded WITHOUT padding
// (RFC 4648 section 5 alphabet).
//
// The encoding makes the mapping injective: identifiers are opaque and may
// contain '.', so plain concatenation would let two distinct sessions share a
// channel. The result's alphabet is A-Za-z0-9-_ plus the prefix and separator.
//
// It does not validate its arguments; callers validate identifiers where they
// accept them.
func HostLinkChannel(tenantID TenantID, sessionID SessionID) string {
	encoding := base64.RawURLEncoding
	return HostLinkChannelPrefix + encoding.EncodeToString([]byte(tenantID)) + "." + encoding.EncodeToString([]byte(sessionID))
}

// HostLinkPathPrefix is the path under which a Host serves each tenant's
// HostLink: a tenant's link is HostLinkPathPrefix followed by exactly one path
// segment naming that tenant. It is the value host v0.2.1 serves at.
const HostLinkPathPrefix = "/hostlink/"

// HostLinkEndpointReason is a stable, machine-readable reason HostLinkEndpoint
// could not derive a tenant's HostLink address. A caller branches on it rather
// than on error text; the set grows only additively.
type HostLinkEndpointReason string

const (
	// HostLinkEndpointReasonInvalidBase: the base fails
	// InternalEndpoint.Validate. The error wraps that *RequestValidationError.
	HostLinkEndpointReasonInvalidBase HostLinkEndpointReason = "invalid_base"
	// HostLinkEndpointReasonBaseNamesTenant: the base's path is already
	// HostLinkPathPrefix, with or without a tenant segment. This is how a
	// host v0.2.1 endpoint, which advertised one tenant's link, is spelled; it
	// is not a base, and appending a second tenant to it could never route.
	HostLinkEndpointReasonBaseNamesTenant HostLinkEndpointReason = "base_names_tenant"
	// HostLinkEndpointReasonBaseNotBare: the base carries something besides a
	// scheme, an authority and at most one trailing '/': any other path, or an
	// empty fragment marker ('#', which InternalEndpoint.Validate tolerates
	// because net/url drops an empty fragment, but which would swallow an
	// appended path).
	HostLinkEndpointReasonBaseNotBare HostLinkEndpointReason = "base_not_bare"
	// HostLinkEndpointReasonInvalidTenant: the tenant fails TenantID.Validate.
	// The error wraps that *IDValidationError.
	HostLinkEndpointReasonInvalidTenant HostLinkEndpointReason = "invalid_tenant"
	// HostLinkEndpointReasonUnroutableTenant: the tenant is a legal Core
	// identity that a Host's HostLink router cannot be relied on to resolve:
	// "." and "..", or any tenant containing '/'. See HostLinkEndpoint.
	HostLinkEndpointReasonUnroutableTenant HostLinkEndpointReason = "unroutable_tenant"
	// HostLinkEndpointReasonTooLong: the derived address is longer than
	// MaxIDBytes, so it would fail InternalEndpoint.Validate. The limit counts
	// the ESCAPED tenant, so a tenant well under MaxIDBytes can exceed it.
	HostLinkEndpointReasonTooLong HostLinkEndpointReason = "too_long"
)

// HostLinkEndpointError reports why HostLinkEndpoint refused. Its text names
// the reason only, never the base or tenant, so it is safe to log. Err is the
// lower-level validation error for the invalid_base and invalid_tenant reasons
// and nil otherwise.
type HostLinkEndpointError struct {
	Reason HostLinkEndpointReason
	Err    error
}

func (e *HostLinkEndpointError) Error() string {
	return "sessionwire/v1: cannot derive HostLink endpoint: " + string(e.Reason)
}

// Unwrap returns the lower-level validation error, if any.
func (e *HostLinkEndpointError) Unwrap() error { return e.Err }

// HostLinkEndpoint derives one tenant's HostLink address from a Host's BASE
// endpoint.
//
// # A Host's advertised internal_endpoint is a BASE
//
// A Host serves a separate HostLink per tenant, at HostLinkPathPrefix plus that
// tenant, and authenticates each connection against the tenant its PATH names
// before any transport is chosen. The internal_endpoint a Host advertises (in
// HostLinkCapacityReport and HostLinkRegistryObservation) is therefore a base
// — a ws or wss scheme and an authority, nothing else — and a Factory reaches
// tenant T on that Host by dialling HostLinkEndpoint(base, T). One Factory link
// carries one tenant; a pooled Host serving several tenants has one link per
// tenant, all derived from the same base.
//
// The result is the base with at most one trailing '/' removed, then
// HostLinkPathPrefix, then the tenant escaped with url.PathEscape. That is
// byte-for-byte the path host v0.2.1's router resolves back to the same tenant
// (pinned by goldens produced by running that router). The escaping is not
// optional: a '%' in a tenant would otherwise be decoded, so a tenant spelled
// "%2e" would reach tenant "."; and '?', '#', space and control characters
// would otherwise end or break the path.
//
// # What it refuses
//
// It refuses, with a *HostLinkEndpointError, every input that cannot produce a
// routable, valid address, checking the base before the tenant:
//
//   - a base that fails InternalEndpoint.Validate (invalid_base);
//   - a base whose path is already HostLinkPathPrefix, as a host v0.2.1
//     per-tenant endpoint is (base_names_tenant);
//   - a base with any other path, or an empty fragment marker
//     (base_not_bare). A path prefix is refused rather than preserved because
//     a Host serves HostLinkPathPrefix at its root; admitting one later is an
//     additive relaxation, while refusing one later would not be;
//   - a tenant that fails TenantID.Validate (invalid_tenant);
//   - "." and "..", and any tenant containing '/' (unroutable_tenant). A Go
//     http.ServeMux path-cleans "/hostlink/." and "/hostlink/.." and redirects
//     them, and a Host takes exactly one path segment, which '/' — escaped or
//     not, since the server decodes it — can never be. "." and ".." do reach
//     host v0.2.1 when spelled "%2E" and "%2E%2E", but only because Go 1.22's
//     mux cleans the escaped path: under GODEBUG=httpmuxgo121=1 every
//     spelling is redirected, and RFC 3986 lets any intermediary decode a
//     percent-encoded unreserved character and then remove the dot segment.
//     An address that routes only by that accident is refused;
//   - a derived address longer than MaxIDBytes (too_long).
//
// It returns an endpoint that passes InternalEndpoint.Validate whenever it
// returns no error. It is a pure function over its arguments.
//
// # Compatibility window
//
// A host v0.2.1 advertises its full per-tenant address (base plus
// "/hostlink/<tenant>"), and a Factory predating this rule dials the advertised
// endpoint verbatim. The two halves of the change must therefore move together:
// a Host that advertises a BASE, dialled verbatim by an older Factory, answers
// 404, because the base's path names no tenant segment; and a Factory deriving
// addresses from a v0.2.1 Host's advertised endpoint gets base_names_tenant.
// Upgrade Factory with Host.
func HostLinkEndpoint(base InternalEndpoint, tenant TenantID) (InternalEndpoint, error) {
	if err := base.Validate(); err != nil {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonInvalidBase, Err: err}
	}
	// Validate has just parsed base successfully, so this parse cannot fail.
	parsed, _ := url.Parse(string(base))
	if parsed.Path == strings.TrimSuffix(HostLinkPathPrefix, "/") || strings.HasPrefix(parsed.Path, HostLinkPathPrefix) {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonBaseNamesTenant}
	}
	if (parsed.Path != "" && parsed.Path != "/") || strings.Contains(string(base), "#") {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonBaseNotBare}
	}
	if err := tenant.Validate(); err != nil {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonInvalidTenant, Err: err}
	}
	if tenant == "." || tenant == ".." || strings.Contains(string(tenant), "/") {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonUnroutableTenant}
	}
	endpoint := strings.TrimSuffix(string(base), "/") + HostLinkPathPrefix + url.PathEscape(string(tenant))
	if len(endpoint) > MaxIDBytes {
		return "", &HostLinkEndpointError{Reason: HostLinkEndpointReasonTooLong}
	}
	return InternalEndpoint(endpoint), nil
}

// EncodeHostLinkConnectRequest returns the bytes a HostLink connect carries as
// its Data: the bare VersionNegotiationRequest. It refuses an offer that
// VersionNegotiationRequest.Validate refuses.
func EncodeHostLinkConnectRequest(request VersionNegotiationRequest) ([]byte, error) {
	return json.Marshal(request)
}

// DecodeHostLinkConnectRequest reads a HostLink connect's Data as the bare
// VersionNegotiationRequest. It is strict: a wrapped or otherwise unknown
// member is refused with RequestValidationCodeUnknownField.
func DecodeHostLinkConnectRequest(data []byte) (VersionNegotiationRequest, error) {
	var request VersionNegotiationRequest
	if err := json.Unmarshal(data, &request); err != nil {
		return VersionNegotiationRequest{}, err
	}
	return request, nil
}

// EncodeHostLinkConnectReply returns the bytes a HostLink connect reply
// carries as its Data: the bare VersionNegotiationResponse, with
// hostlink_methods present only when the Host advertises capabilities.
func EncodeHostLinkConnectReply(response VersionNegotiationResponse) ([]byte, error) {
	return json.Marshal(response)
}

// DecodeHostLinkConnectReply reads a HostLink connect reply's Data as the bare
// VersionNegotiationResponse. A reply without the version member — including
// one wrapped as {"version_negotiation":{...}} — is refused with
// RequestValidationCodeMissingField rather than read as version 0.
func DecodeHostLinkConnectReply(data []byte) (VersionNegotiationResponse, error) {
	var response VersionNegotiationResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return VersionNegotiationResponse{}, err
	}
	return response, nil
}
