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
// Compatibility window: host v0.2.1 advertised a full per-tenant address. A
// Host that advertises a base, dialled verbatim by an older Factory, answers
// 404; a Factory that derives addresses refuses a v0.2.1 Host's endpoint with
// reason base_names_tenant. Factory and Host adopt the rule together.
package v1
