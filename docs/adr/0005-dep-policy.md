# ADR-0005 — External dependency policy + accepted-library manifest

Status: accepted

## Context

Every external Go dependency we accept becomes attack surface (CVE),
maintenance burden (transitive deps, version skew), and licensing
exposure (commercial sale obligations). The default must be stdlib;
exceptions must be deliberate, documented, and re-audited regularly.

## Decision

### Default

Stdlib only. New external Go deps are forbidden by default.

### Exception process

A new external dep is admissible only after:

1. CVE history reviewed (`govulncheck` + `osv.dev`).
2. Transitive dep count audited (`go mod graph`); 0 is best, ≤3
   acceptable, >5 needs strong justification.
3. License confirmed commercial-clean.
4. Confirmed pure Go (no CGo wrappers, per ADR-0016 multi-OS).
5. ADR file updated to add the dep to the manifest below.
6. Issue / branch / PR / CI green / `@yboujraf` codeowner approval
   per ADR-0014.

### Forbidden in codec layer

`internal/<proto>/codec/` packages MUST NOT import any external dep,
ever. Codec is stdlib-only forever (ADR-0006).

### Authoritative manifest

The accepted-deps list is in [`0005-deps.json`](0005-deps.json). The
table below is regenerated from that file by
`tools/render-dep-table.sh` and must not be hand-edited.

#### Generated dependency table

| ID | Module | Owner | License | Pure Go | CGo | Scope | Reason |
|---|---|---|---|---|---|---|---|
| coder-websocket | `github.com/coder/websocket` | Coder Technologies, Inc. | ISC | yes | no | all | no stdlib WebSocket; ws+wss native via stdlib TLS |
| godbus-dbus | `github.com/godbus/dbus/v5` | godbus org (community) | BSD-2-Clause | yes | no | linux | Linux Avahi via DBus; no stdlib DBus |
| hashicorp-vault-api | `github.com/hashicorp/vault/api` | HashiCorp, Inc. | MPL-2.0 | yes | no | all | secrets manager client SDK (ADR-0010) |
| hashicorp-go-plugin | `github.com/hashicorp/go-plugin` | HashiCorp, Inc. | MPL-2.0 | yes | no | all | plugin supervisor (ADR-0009) |
| golang-jwt-v5 | `github.com/golang-jwt/jwt/v5` | golang-jwt org (community) | MIT | yes | no | all | license format JWT-EdDSA (ADR-0003) |

### Rules attached to this manifest

| # | Rule |
|---|---|
| 1 | This manifest is the **only** authoritative list. `go.mod` direct deps must match exactly. |
| 2 | CI gate: `tools/check-deps.sh` parses `go.mod`, fails if any direct dep absent from `0005-deps.json`. |
| 3 | Adding a new dep = update this ADR (issue + branch + PR + CI green + codeowner approval per ADR-0014). |
| 4 | Versions pinned in `go.mod` — no floating versions. |
| 5 | Quarterly re-run of `govulncheck` + `osv-scanner` + `go-licenses`; refresh `cve_last_check` field in `0005-deps.json`. |
| 6 | Codec packages never import any of these deps (ADR-0006). |
| 7 | CGo wrappers are forbidden; stdlib floor must always work where the OS-native daemon is unavailable (ADR-0016). |

## Consequences

- Lean dependency tree, small CVE surface.
- Every external dep has a commercial-clean license, audited.
- New deps require justification + review before merge.
- CI rejects PRs that introduce unlisted deps.

## Forbidden

- Importing any external Go package not present in `0005-deps.json`.
- Codec layer importing any external Go package, listed or not.
- CGo wrappers around closed-source native libraries.
- Floating versions in `go.mod` (no `v0.0.0-<date>-<commit>` unless
  pinned with explicit reason in this ADR).
