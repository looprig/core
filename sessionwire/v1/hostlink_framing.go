package v1

import "encoding/base64"

// HostLink framing names. These are plain strings shared by the Factory and
// Host ends of a HostLink; no transport type or adapter enters Core.
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
