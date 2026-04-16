# Contributing to acp

## Workflow

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Run `make test` and `make lint` (both must pass)
5. Open a pull request against `main`
6. Code review required before merge

## Protocol packages

Each protocol lives in its own package: `internal/protocol/{name}/`.
Protocols are independent and self-contained. Do not import one protocol
package from another.

See `internal/protocol/_template/` for a starting point.

## Testing requirements

- Unit tests: `tests/unit/{name}/` -- table-driven, byte-exact against spec
- Integration tests: `tests/integration/{name}/` -- tagged `//go:build integration`
- All codec tests must include expected byte sequences from the protocol spec

## Dependencies

No external Go dependencies without explicit approval from the maintainer.
This project ships as a single static binary. Every dependency must justify
its existence.

## Commit messages

```
<component>: <short description>

Optional longer explanation.
```

Examples:
- `acp1/codec: fix MLEN prefix for TCP direct mode`
- `cli: add --timeout flag to connect command`
- `export: support YAML import round-trip`

## Code style

- Go 1.22+, `gofmt` + `goimports`
- `context.Context` as first param on all I/O functions
- `log/slog` for all logging, never `fmt.Println`
- `errors.As` / `errors.Is` at call sites, never string-match

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
