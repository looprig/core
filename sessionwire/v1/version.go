package v1

// WireVersion identifies a Looprig session wire-contract version.
type WireVersion uint8

// CurrentWireVersion is version 1 of the Looprig session wire contract. It is
// intentionally independent of any transport or broker client-protocol version.
const CurrentWireVersion WireVersion = 1
