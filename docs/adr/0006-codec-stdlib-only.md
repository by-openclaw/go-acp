# ADR-0006 — Codec packages stdlib-only forever

Status: accepted

## Context

Codec packages (`internal/<proto>/codec/`) hold the byte-level wire
format for each protocol. They must be lift-to-own-repo ready: a third
party should be able to extract `internal/<proto>/codec/` as a Go
module with zero changes and use it standalone.

External dependencies in codec layers undermine that: they pull in
their own version skew, CVE surface, and license obligations into
every consumer of the codec — including future repos we don't yet
own.

## Decision

Codec packages MUST import only the Go standard library. Forever.

This applies to every protocol's `internal/<proto>/codec/` subtree,
no exceptions. Specifically forbidden:

- Any module from `docs/adr/0005-deps.json` (yes, even the approved
  ones).
- Any third-party Go module not in stdlib.
- Any CGo binding.
- Any `internal/amwa/*`, `internal/transport/*`, `internal/protocol`,
  `internal/provider`, or other internal-package import.

Permitted:

- `encoding/binary`, `encoding/asn1`, `encoding/json`,
  `encoding/base64`, `encoding/pem`, `encoding/hex`, `encoding/xml`
- `bytes`, `bufio`, `io`
- `errors`, `fmt`
- `math`, `math/big`
- `crypto/*` (subtree of stdlib)
- `time`, `strings`, `strconv`, `unicode/*`
- `net`, `net/url` (for path types and URL formatting only — not
  network I/O; that's in session/transport layers)

## Consequences

- Codec extraction to its own repo is one `go mod init` away.
- Wire-format bugs are reproducible without any infrastructure.
- Codec tests run on any platform with only Go installed.
- No external lib can break a codec via an upgrade.
- Lower CVE surface for the most security-sensitive layer (the bytes
  on the wire).

## Forbidden

- Adding any non-stdlib import to a codec package.
- Importing other `internal/*` packages from codec.
- Calling network I/O from codec (codec is bytes ↔ structs only).
- Using build tags to conditionally pull in non-stdlib deps in codec.

## Enforcement

- `go list -deps ./internal/*/codec/...` must list only stdlib
  packages. CI runs this as a check.
- depguard linter rule pinned in `.golangci.yml`.
- PR review checklist line: "any change to `internal/*/codec/*`
  imports → fail review".
