# Contributing to looprig/core

Thanks for considering a contribution. `core` is a foundational, stdlib-only
leaf module in the looprig ecosystem: it has no dependency on any other
looprig module and is depended on directly by several others (including
`harness`). It currently provides:

- `content` — the base conversation types (`Message`, content blocks, chunks,
  streaming accumulation) shared across the ecosystem.
- `logging` — a thin, dependency-injected wrapper around `log/slog` (no
  package-level logger, no `slog.SetDefault`).
- `uuid` — a stdlib-only v4 UUID implementation.

Because so much depends on this module, changes here ripple outward. Keep
that in mind before you start.

## Before you write code

- Open an issue for anything non-trivial so direction can be agreed before
  you spend the time.
- Prefer the standard library. `core` deliberately has no non-tool runtime
  dependencies; adding one is a significant decision and should be raised
  and agreed before code is written.

## Build, test, and secure

Run these before pushing. CI runs the same.

```sh
make fmt       # gofmt the whole module in place
make test      # go test -race ./...
make check     # fmt-check + vet + test
make secure    # lint (fmt-check + vet + staticcheck + gosec) + vuln (go mod verify + govulncheck)
```

Fuzz any parser of external input: `go test -fuzz=FuzzXxx ./pkg -fuzztime=30s`
(`make fuzz` prints the usage reminder).

## Tests

- **Table-driven tests, mandatory** when several cases share setup and
  assertion shape. Each subtest calls `t.Parallel()`. Cover the happy path,
  boundary values (zero/empty/max), error cases (invalid/missing/wrong
  type), and domain edge cases.
- A test that passes without `-race` but fails with it is **not passing**.
- Never assume a test framework or script. The `Makefile` is the source of
  truth; if you change how tests run, update it.

## Pull requests

- Branch from `main`, name the branch something descriptive.
- One logical change per PR.
- Don't commit secrets, tokens, or credentials.
- This is a widely-depended-on foundational module: if your change touches
  the public API (exported types, functions, or method signatures), flag
  that clearly in the PR description and think through the downstream
  impact on every module and consumer that imports `core`.

## Code of conduct

Be excellent to each other. Discussions stay technical and respectful;
personal attacks, harassment, and discrimination are not welcome.

## License

By contributing, you agree that your contributions are licensed under the
Apache License 2.0, as described in [`LICENSE`](LICENSE).
