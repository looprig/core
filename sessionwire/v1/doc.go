// Package v1 defines version 1 of Looprig's transport-neutral session wire
// contract.
//
// It owns the shared identity and protocol-version vocabulary without coupling
// the contract to a particular host, transport, broker, or user interface.
//
// # HostLink addressing
//
// A Host's advertised internal_endpoint (HostLinkCapacityReport,
// HostLinkRegistryObservation) is a BASE: a ws or wss scheme and an authority.
// A Host serves one HostLink per tenant, and the per-tenant HostLink address is
// derived with HostLinkEndpoint(base, tenant); a Factory holds one link per
// (Host, tenant) pair. The records carry no marker saying whether an endpoint
// is a base, and must not grow one, because their decoders are strict.
//
// Compatibility window: host v0.2.1 advertises its configured endpoint
// verbatim, and working deployments configured a per-tenant address because an
// older Factory dials the advertised endpoint verbatim. A Host that advertises
// a base, dialled that way, answers 404; a deriving Factory refuses a
// per-tenant endpoint with code base_names_tenant (base_not_bare behind an
// ingress path prefix). A host v0.2.1 reconfigured to advertise a bare base
// already works with a deriving Factory, with no Host upgrade. The advertised
// value and the Factory move together.
package v1
