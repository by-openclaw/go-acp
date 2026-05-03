# Contributing to dhs

## Workflow

Per **ADR-0014** (issue tracking discipline):

1. Open a GitHub issue using the appropriate template
2. Create a feature branch from `main` named `<type>/<short-slug>`
3. Make your changes — one approved unit = one commit (**ADR-0013**)
4. Run `make test`, `make vet`, and `make lint` (all must pass)
5. Open a pull request against `main` — fill in the PR template
6. Wait for CI green and `@yboujraf` codeowner approval before merge

## Protocol packages

Each protocol lives in its own subtree at `internal/<proto>/`:

```text
internal/<proto>/
├── CLAUDE.md     atomic per-protocol context (read first)
├── codec/        stdlib-only byte codec (lift-to-own-repo ready)
├── consumer/     implements protocol.Protocol
├── provider/     implements provider.Provider
├── wireshark/    dhs_<proto>.lua dissector
├── docs/         consumer / provider / README per protocol
└── assets/       spec PDFs + vendor tools
```

Protocols are independent. **Do not** import one protocol package from
another. See `internal/protocol/_template/` for a starting point.

## Testing requirements

- Unit tests live alongside the code in each package (`*_test.go`)
- Integration tests are tagged `//go:build integration` and skip unless
  the relevant `<PROTO>_TEST_HOST` env var is set
- Every codec test must include expected byte sequences from the
  protocol spec (no round-tripping your own encoder; see
  `feedback_real_peer_closes_self_test`)

## Dependencies

Per **ADR-0005**, stdlib first. New external dependencies require an
ADR-0005 manifest entry plus CVE / transitive-count / license review.
Codec packages stay stdlib-only forever (**ADR-0006**).

## Commit messages

Conventional commits:

```text
<type>(<scope>): <short description>

Optional longer explanation.

Closes #<issue>
```

Examples:

- `fix(acp1/codec): correct MLEN prefix for TCP direct mode`
- `feat(cli): add --timeout flag to connect command`
- `chore(deps): bump prometheus/client_golang to v1.23.2`

## Code style

- Go 1.23+, `gofmt` + `goimports`
- `context.Context` as first param on all I/O functions
- `log/slog` for all logging — never `fmt.Println`
- `errors.As` / `errors.Is` at call sites — never string-match

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
