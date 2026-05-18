# Ember+ — consumer ↔ provider parity audit, 2026-05-18

Live SHA at audit time: `c875e34` (`feat/emberplus-stream-idle-ttl-472`, off `main` `6c28325`).

Methodology per R19 #484: phase 1 inventory complete (each UC mapped to its consumer + provider + dissector + tests); phase 2 wire-verify deferred to the ADR-0025 #3 integration-test batch (live producer + consumer + tshark on the LXC test rig); phase 3 compliance-verify covered for `OneToOneSourceStealAccepted` and `StreamIdleTTLExpired` via the corresponding unit tests; phase 4 reconcile reflected in `docs/protocols/use-cases/emberplus.md`.

## Audit summary

| UC | Verb | Consumer | Provider | Caveat |
| --- | --- | --- | --- | --- |
| UC-1 | `info` | ✅ | ✅ | DTD version surfaced post R6 #470 |
| UC-2 | `walk` | ✅ | ✅ | Multi-frame reassembly via S101 FlagFirst/FlagLast |
| UC-3 | `get` | ✅ | ✅ | Numeric OID + dotted-label both accepted (R21 #486) |
| UC-4 | `set` | ✅ | ✅ | Client-side range/step/enum constraints (R16 #483) |
| UC-5 | `watch` | ✅ | ✅ | Tally + stream + glow merged feed |
| UC-6 | `matrix` connect / disconnect / absolute | ✅ | ✅ | oneToOne source-steal accepted with compliance event |
| UC-7 | `invoke` | ✅ | ✅ | `--format human` pretty-prints `getSalvo` (R5 #482) |
| UC-8 | `stream` | ✅ | ✅ | Provider-side idle-TTL via R9 #472 `--stream-ttl` |
| UC-9 | `profile` | ✅ | n/a | R22 #487: `--format`, `--since`, `--show-events`, `--by-session` |
| UC-10 | `export` / `import` | ⚠️ partial | n/a | Full Glow round-trip pending R4 #461 |
| UC-11 | `extract` | ✅ | n/a | Cache + manifest co-written |
| UC-12 | `validate` | ✅ | n/a | `--report <md\|json>` (R23 #488); `--lua` pending R12 #473 |
| UC-13 | `bench` | ✅ | n/a | RFC 2544 profiles pending R13 #474 |
| UC-14 | `health` | ❌ | ❌ | Tracked at #300 |
| UC-15 | `discover` | ❌ | ❌ | Bidirectional mDNS pending R18 #477 |
| UC-16 | `diff` / `convert` | ✅ | n/a | Offline format ops |

Status icons:

- ✅ — wired both sides, integration test pending only the ADR-0025 #3 batch.
- ⚠️ partial — consumer or provider missing a documented sub-feature; tracking issue named in Caveat.
- ❌ — not implemented today; tracking issue named in Caveat.
- n/a — verb is consumer-side only (export, profile, validate, etc.) or producer-side only by design.

## Divergences found

- **R9 #472 stream idle-TTL** introduces a new compliance event `stream_idle_ttl_expired` on the provider side. Consumer-side `cmd_profile.go --show-events` (R22 #487) is the operator surface that reads this counter end-to-end.
- **OneToOne source-steal** (R5b #482 / #465) — consumer absorbs and fires `onetoone_source_steal_accepted` per pre-flight pair. Provider broadcasts the resulting connection-change as usual; no asymmetry. Follow-up integration test recommended: trigger source-steal N times and assert the compliance counter increments deterministically.
- **`-v` ladder** (R15 #476) is wired in `cmd/dhs/common.go` (every consumer verb) plus `cmd_producer.go` and `cmd_acp1_fuzz.go`. `cmd_tsl_producer.go` is **NOT yet retrofitted** — flagged in [docs/logging.md](../../logging.md). Tracked as part of the runbook follow-up rather than blocking the Ember+ DOD.
- **Logger DI** audited (R15 #476): all production paths under `internal/emberplus/{consumer,provider}/` thread the logger via constructor parameter. No `slog.Default()` bypass found on the hot path.

## Action items rolled out of the audit

| # | Note | Tracked at |
| --- | --- | --- |
| 1 | Integration tests driving the CLI binary for every UC | ADR-0025 #3 batch |
| 2 | `health` verb (consumer + provider) | [#300](https://github.com/by-openclaw/go-acp/issues/300) |
| 3 | Bidirectional mDNS for `discover` | R18 [#477](https://github.com/by-openclaw/go-acp/issues/477) |
| 4 | Full Glow export/import round-trip | R4 [#461](https://github.com/by-openclaw/go-acp/issues/461) |
| 5 | `validate --lua` tshark mode | R12 [#473](https://github.com/by-openclaw/go-acp/issues/473) |
| 6 | RFC 2544 bench profiles | R13 [#474](https://github.com/by-openclaw/go-acp/issues/474) |
| 7 | TSL producer log-flag retrofit | doc note in `docs/logging.md` |

## Cross-references

- Use-case matrix (live): [docs/protocols/use-cases/emberplus.md](../use-cases/emberplus.md)
- Runbook: [internal/emberplus/docs/runbook.md](../../../internal/emberplus/docs/runbook.md)
- Atomic protocol context: [internal/emberplus/CLAUDE.md](../../../internal/emberplus/CLAUDE.md)
- Connector definition of done: [docs/adr/0025-per-connector-definition-of-done.md](../../adr/0025-per-connector-definition-of-done.md)
- Workflow contract: [docs/adr/0027-workflow-contract-dod-windows.md](../../adr/0027-workflow-contract-dod-windows.md)
