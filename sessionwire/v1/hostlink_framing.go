package v1

import (
	"encoding/base64"
	"encoding/json"
)

// HostLink framing names. These are plain strings shared by the Factory and
// Host ends of a HostLink; no transport type or adapter enters Core.
//
// # Connect framing
//
// A HostLink begins with one version negotiation, carried in the transport's
// connect exchange. Its framing is frozen here because the two ends once
// disagreed on it — one wrapped the records as {"version_negotiation":{...}}
// while the other sent and expected them bare — and a strict request decoder
// on one side plus a tolerant response decoder on the other turned that
// disagreement into a connect that failed silently with "version 0".
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
