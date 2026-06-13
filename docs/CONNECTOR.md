# dhs Connector contract

This document is the binding contract every connector implements. It
collates the relevant Architecture Decision Records (ADRs); per
ADR-0015 (single source of truth) it never restates an ADR's content
— it points to it. When a rule changes, the ADR is updated; this
document is regenerated.

## Three roles

Every connector implements one or more of:

| Role | Direction | Examples |
|---|---|---|
| **consumer** | dhs talks **to** an external device or service | `dhs-emberplus consumer walk --host 10.6.239.113` |
| **producer** | dhs **serves** state to external clients | `dhs-emberplus producer serve --port 9000` |
| **registry** | dhs is the dual-face middleware (consumer of registrations + provider of catalogue) | `dhs-nmos registry serve` |

See **ADR-0001** (per-connector binary + own repo) and
**ADR-0002** (canonical CLI verbs + flags).

## CLI

Same canonical verb set + flag set per role across every connector.
Per-protocol verbs are additions, never replacements.

See **ADR-0002**.

For declarative orchestration (Ansible / Puppet / Terraform), every
connector exposes `ensure --state present|absent --check`.

Per-verb reference with a worked example for each verb:
**`docs/protocols/verbs.md`**.

## Definition of done + test tiers

A connector is **DONE** only when all six **ADR-0025** deliverables exist
together (consumer, producer, integration test, DM + manifest generator,
Wireshark dissector, replay fixtures). Validation is three-tier with the
**oracle-per-tier** rule — Unit (spec bytes), Smoke (built binary), Integration
(**vendor emulator + a real device — never our own provider**). Every Ansible
play (deploy / test / verify / converge) is idempotent (run-twice = 0 changes);
there are no PowerShell `.ps1` drivers. CI gates: `-race`, per-package coverage
floors (no-regression), and `-tags integration`.

See **ADR-0025** (definition of done), **ADR-0007** (`ensure`),
`docs/protocols/verb-tests.md` (test taxonomy + oracle rule), and
`docs/protocols/verbs.md` (per-verb reference + examples).

See **ADR-0007**.

## Discovery

Three modes, shared layer (`internal/transport/discover/`):

- `mdns` — multicast DNS-SD (RFC 6762/6763)
- `unicast` — unicast DNS-SD
- `peer-list` — static `peers.csv` / `peers.yaml`

See **ADR-0012**.

Multi-OS backend selection (Avahi DBus on Linux, Bonjour on
macOS/Windows pending wrapper conflict, stdlib floor universal):
see **ADR-0016**.

## License

JWT-EdDSA signed by Vault Transit. Offline mandatory + online
refresh. Expired = no-start.

See **ADR-0003**. Trial fingerprint binding: **ADR-0004**.

## Plugin / hot un-register

`hashicorp/go-plugin` supervisor. Each connector binary runs as a
standalone process; optional `dhs-core` supervisor orchestrates
multiple connector children with hot register / unregister / drain /
rollback.

See **ADR-0009**.

## Dependencies

Stdlib first. New external deps require ADR documentation in
`0005-deps.json` + CI verification (CVE history, transitive count,
license).

See **ADR-0005**. Codec layer is stdlib-only forever: **ADR-0006**.

## Multi-OS

Every connector builds + tests green on Windows + Linux + macOS.
Stdlib floor is the universal fallback.

See **ADR-0016**.

## Observability

| Surface | Mechanism |
|---|---|
| Build identity | `dhs-<proto> info --output json` (see **ADR-0018**) |
| Logs | structured `slog` JSON (`--log-format json`) for Loki/Promtail |
| Metrics | Prometheus scrape endpoint per binary (`--metrics-addr`) |
| License audit events | `license install` / `license expire` / `license refresh` events emitted to logs |

## Compliance

Each connector ships `internal/<proto>/COMPLIANCE.md` with a fixed
section structure (ports, protocols, firewall rules, encryption,
GDPR, NIS2, ISO 27001, CRA, audit log, recovery, per-OS dep matrix).

See **ADR-0008**.

## Workflow

| Step | Reference |
|---|---|
| Issue → branch → tests → PR → CI green → `@yboujraf` approval → merge | **ADR-0014** |
| One approved unit = one commit | **ADR-0013** |
| No restatement of architectural rules across docs | **ADR-0015** |
| Plugin supervisor hot un/register UX (DHS v1 parity) | **ADR-0009** |
| Customer + license + asset records | Odoo, **ADR-0011** |
| License signing key in Vault Transit, internal-only | **ADR-0010** |

## File layout per connector module

```
dhs-<proto>/                          single Go module per connector
├── cmd/dhs-<proto>/main.go           CLI entry point
├── internal/buildinfo/               -ldflags injected build identity (ADR-0018)
├── internal/<proto>/codec/           stdlib-only wire codec (ADR-0006)
├── internal/<proto>/consumer/        consumer side (ADR-0001/0002)
├── internal/<proto>/provider/        provider side (ADR-0001/0002)
├── internal/<proto>/registry/        registry side if applicable (ADR-0001/0002)
├── internal/<proto>/wireshark/       Lua dissector (per top-level CLAUDE.md)
├── internal/<proto>/CLAUDE.md        per-protocol wire facts only (ADR-0015)
├── internal/<proto>/COMPLIANCE.md    audit pack (ADR-0008)
├── internal/<proto>/runbook-multi-os.md   per-OS install (ADR-0016)
├── internal/<proto>/docs/            consumer / provider / README per protocol
└── tests/                            unit / integration / smoke / fixtures
```

## ADR index

[`docs/adr/README.md`](adr/README.md) — full list with status.

## Status: ADR-0017 parked

ADR-0017 (file-header template) is parked pending the project
owner's pick of license model + corporate identity values. Template
structure preserved in agent memory until activated.
