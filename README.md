# looprig/core

`github.com/looprig/core` is the foundation module of the Looprig ecosystem.
It holds the vocabulary every other module shares: conversation content
(messages, content blocks, streaming chunks, token usage), a v4 UUID, a
dependency-injected `log/slog` constructor, and version 1 of the
transport-neutral session wire contract that Factory, Host, Harness and web
clients speak to each other.

It is a tier-0 leaf: it depends on no other Looprig module and has no
third-party runtime dependencies (the `tool` directives in `go.mod` are
development tools only).

## Install

```sh
go get github.com/looprig/core@latest
```

## Packages

| Package | Purpose |
|---|---|
| `content` | The sealed `Block` union (`TextBlock`, `ThinkingBlock`, `ToolUseBlock`, `ToolResultBlock`, `ImageBlock`, `DocumentBlock`, `AudioBlock`, ...), streaming `Chunk` types, messages (`UserMessage`, `AIMessage`, `AgenticMessages`), usage accounting, JSON codecs (`MarshalBlock`/`UnmarshalBlock`) and faithful copies (`CloneBlock`/`CloneBlocks`). |
| `content/streamaccumulator` | Pure converters that fold streamed chunks into complete blocks (text, thinking, tool uses, refusals, images). |
| `content/blocktest` | Test helper that builds fully populated block fixtures by reflection, so copy/encode/decode paths are checked field by field. |
| `logging` | `New(Config)` builds a `*slog.Logger` for a composition root to inject; there is no package-level logger and `slog.SetDefault` is never called. |
| `uuid` | Stdlib-only v4 UUID: `New`, strict `Parse`/`MustParse`, `String`, `IsZero`, text marshalling. |
| `sessionwire/v1` | Version 1 of the session wire contract: typed IDs, public session records (create/input/interrupt/restore/gate-response requests, command status, journal and gate pages, session status and summaries, publications, error envelopes), version negotiation, and the HostLink records and framing between Factory and Host. |

`content.Block` is sealed: only the `content` package can add variants. Provider
opaque reasoning state travels in `ProviderState`/`ProviderStateFormat` on the
blocks that carry it; code that copies blocks should use `content.CloneBlock`
rather than a hand-written struct literal, and can pin that with `blocktest`.

## Usage

Adapted from `examples/content/example_test.go`:

```go
request := &content.UserMessage{Message: content.Message{
	Role: content.RoleUser,
	Blocks: []content.Block{
		&content.TextBlock{Text: "Summarize the report."},
		&content.DocumentBlock{
			MediaType: content.MediaTypeDocumentText,
			Name:      "report.txt",
			Text:      "Revenue increased.",
		},
	},
}}

wire, err := content.MarshalBlock(request.Blocks[1])
if err != nil {
	return err
}
decoded, err := content.UnmarshalBlock(wire)
if err != nil {
	return err
}
document := decoded.(*content.DocumentBlock)
```

Further runnable examples live under `examples/` (`content`, `streaming`,
`usage`, `logging`, `uuid`) and run with `go test ./examples/...`.

## Session wire contract (`sessionwire/v1`)

Every record has a checked-in JSON Schema (draft 2020-12) under
`sessionwire/v1/schema/` and a golden fixture under
`sessionwire/v1/testdata/fixtures/`; tests hold the Go types, schemas and
fixtures together, and `sessionwire/v1/testdata/exported_api.txt` pins the
exported API. Decoders are strict and refuse unknown fields, except the
version-negotiation response, which is tolerant so that new optional members
stay additive.

HostLink specifics a Factory or Host implementer needs:

- **Connect framing.** The connect request and reply are the bare
  `VersionNegotiationRequest`/`VersionNegotiationResponse`; use
  `EncodeHostLinkConnectRequest`/`DecodeHostLinkConnectRequest` and
  `EncodeHostLinkConnectReply`/`DecodeHostLinkConnectReply`, which refuse a
  wrapped shape.
- **Method names and channels.** The `HostLinkMethod*` constants name the RPC
  methods; command delivery is sent on `HostLinkChannel(tenant, session)`
  (prefix `HostLinkChannelPrefix`).
- **Capabilities.** A Host lists the methods and capability tokens it supports
  in the negotiation reply's `hostlink_methods`; check them with
  `VersionNegotiationResponse.Supports`. Capability tokens are not methods:
  `HostLinkCapabilityGateResponse` (`hostlink.command.gate_response`) gates
  gate responses, and `HostLinkCapabilityAttributionPrincipal`
  (`hostlink.attribution.principal`) gates any command carrying `principal` or
  `metadata`, and `HostLinkCapabilityPayloadReference`
  (`hostlink.payload.reference`) gates placing a session whose pending command
  body is stored by reference (above SessionStore's 64 KiB inline bound). A
  Factory must not send a command a Host's reply does not
  support, and must not send `hostlink.attach` to a Host that does not
  advertise it.
- **Addressing.** A Host advertises a bare base endpoint (`ws`/`wss` scheme and
  authority, no path). Each tenant's HostLink address is derived with
  `HostLinkEndpoint(base, tenant)` (`base` + `HostLinkPathPrefix` + escaped
  tenant); refusals are a typed `HostLinkEndpointError`. A Host behind a
  path-prefixed ingress must use host-based routing, a port per Host, or
  in-cluster Service DNS instead.
- **Command principal and metadata.** Every command request may carry an
  optional `principal` (`Principal{Tenant, Subject, Kind}`, kind `actor` or
  `service`): the verified sender, stamped only by a Factory, never by a
  client. `create` and `input` may also carry optional `metadata`
  (`MessageMetadata`, string values only): at most 16 fields; keys
  `^[a-z][a-z0-9_]{0,63}$`, never beginning `looprig`; values at most
  1024 bytes; 4096 bytes encoded. Failures are a typed
  `MetadataValidationError` that also unwraps to `RequestValidationError`
  (`invalid_field`, `metadata`). When both are absent a request is
  byte-identical to earlier releases.
- **Command IDs.** `DeterministicCommandID(domain, fields...)` derives a
  retry-stable command ID from framed fields.

The wire version is `1`; changes within v1 are additive and existing fixtures
stay byte-identical.

## Where it sits

Tier 0 of the Looprig release graph, with no Looprig dependencies. Nearly every
other module depends on it directly (`inference`, `harness`, `flow`,
`sessionstore`, `factory`, `host`, `tools`, `tui`, `wui` and more), so a core
release is published before any dependent names it.

## Development

The module targets Go 1.26.9.

```sh
GOWORK=off go test ./...   # standalone, against the pinned go.mod
make test                  # go test -race ./...
make fmt                   # gofmt in place
make lint                  # fmt-check + vet + staticcheck + gosec
make secure                # lint + vuln (go mod verify + govulncheck)
make check                 # fmt-check, vet, staticcheck, gosec, govulncheck, tests, build
make fuzz                  # prints how to run the fuzz targets
```

Dependencies are pinned by `go.mod`/`go.sum`; the module is not vendored. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for contribution guidelines.

## License

Apache License 2.0. See [`LICENSE`](LICENSE).
