# ADR-0025 — Per-connector definition of done

Status: accepted

This ADR is a **living document**. Add new facts in the Revisions
trailer at the end. Do not spawn a new ADR number unless the whole
record is being superseded.

## Context

Earlier ADRs define **how** a connector is built:

- ADR-0001 — one binary per connector, its own repo
- ADR-0002 — canonical CLI verbs + flags
- ADR-0009 — plugin supervisor
- ADR-0013 — one approved unit = one commit
- ADR-0014 — issue → branch → tests → PR → CI green → codeowner approval → merge
- ADR-0015 — single source of truth per concern
- ADR-0022 — Card / Frame / Slot / DM data model
- ADR-0023 — Matrix as parallel entity

Nothing in the existing ADR set defines **when a connector is finished**.
Without that gate, PRs have repeatedly landed that fix one symptom
against a partial connector — the test surface that voted green for the
fix did not exercise the broken path, so the next session discovers a
parallel gap on a different verb and the cycle restarts.

The pattern is concrete:

- A consumer-side bug ships green because the integration test
  exercises only the codec layer, not the CLI binary end-to-end.
- A provider-side bug ships green because the only integration test is
  a `t.Skip` placeholder.
- A CLI verb works against one identifier scheme (numeric OID) but not
  another (dotted path) because the test surface doesn't drive the
  CLI through both schemes.
- A spec command is silently absent on a verb because no test asserts
  the command surface.

Each missing piece compounds the next: a partial consumer cannot be
trusted against a partial provider, and a partial integration test
cannot vouch for either side. The result is sustained churn without
forward progress on the connector.

A binding definition of done closes the gate at the connector boundary:
no per-connector PR is approved for merge unless the connector has all
five deliverables below.

## Decision

A connector is **DONE** only when **all six** deliverables below exist
together. Missing any one of them means the connector is incomplete;
work continues on that connector before the next connector starts and
before any per-connector PR is approved for merge. No "ship now, finish
later" framing.

| # | Deliverable | Location | Notes |
|---|---|---|---|
| 1 | **Consumer** strict-to-spec, every CLI verb the spec defines | `internal/<proto>/consumer/` + `cmd/dhs/cmd_*.go` | Every spec command + every wire-form variant. Probel general/extended form selection is a tested boundary decision per command. Ember+ is DTD 2.60 only — no time spent on older DTDs. |
| 2 | **Producer** strict-to-spec, every CLI verb the spec defines | `internal/<proto>/provider/` + `cmd/dhs/cmd_producer.go` | Same coverage rule as deliverable 1, inbound. |
| 3 | **Integration test** driving the actual `dhs consumer/producer <proto> <verb>` CLI binary | `internal/<proto>/integration/` (Go, `-tags integration`) and/or `scripts/<proto>/verify-*.ps1` for live-rig parity | Per-test classification: `PASS` / `FAIL-real` (a real bug) / `FAIL-expected` (an error-handling reject path correctly rejected) / `TIMEOUT` (consumer sent CLI against a dead producer). Output must say *why* on every non-pass so the operator acts without reading source. |
| 4 | **DM + manifest generator** | Go function under `internal/<proto>/integration/` that writes a local `.cache/dm/<proto>/...` + `.cache/manifest/...` next to the test files (NOT the codeowner's repo-root `.cache/`) | Tests invoke the generator in `setUp`. Tests **MUST NOT** `t.Skip` when the cache is empty — they must generate. The generator's Go source is the durable artefact. |
| 5 | **Wireshark dissector** `internal/<proto>/wireshark/dhs_<proto>.lua` | Covers every transport, every wire version, every command / type-tag the connector implements. Per-frame Info column carries the discriminating arguments (matrix/level/dst/src for SW-P-08; slot/type/pid/stat for ACP2; address+typetag+argcount for OSC; etc.). | See `internal/<proto>/wireshark/` + `feedback_wireshark_fully_implemented`. |
| 6 | **Replay fixture set** under `internal/<proto>/testdata/` | Layout: `testdata/protocol_types/<typename>/` (one folder per spec type / command / message kind, each containing the raw wire capture + canonical tree + per-type doc), `testdata/fixtures/` (multi-frame golden scenarios), `testdata/exports/` (canonical exports for round-trip checks). Reference shape: `internal/acp1/testdata/` (`.pcapng` raw + `.tree` canonical + per-type `.md`). | Lets every connector replay raw / pcap at any time, in CI, without needing live device access. Promotion rules from local `captures/<proto>/<ip>/<scenario>/` to committed `testdata/` live in `captures/README.md` (size cap, edge-case justification, byte-stability). |

Documentation deliverables travel alongside but are not gated by the
ADR — they are required at PR time per ADR-0015 (single source of
truth):

- **README** with a per-connector coverage table (what verbs, integration test status, what's tested live, what's NOT yet tested + reason — e.g. awaiting sample / real device).
- **Runbook per connector** (`internal/<proto>/docs/runbook.md`) — every CLI verb described with: syntax, captured real-run example, expected output, common error modes.

## How to apply

1. **Before starting work on any connector**, audit the connector against the five-deliverable checklist. Flag missing pieces explicitly.
2. **Each PR must cite which deliverable it advances** for which connector, plus the state of the other four (`done` / `partial` / `missing`).
3. **One PR advances one deliverable for one connector**. No PR can combine "connector A integration test" with "connector B fix" — that's the bundle pattern ADR-0013 already forbids, restated here for the avoidance of doubt.
4. **No connector PR is approved for merge until all six exist** for that connector. The integration test in (3) is the truth source: green there = real green; failing there = the wire is broken, fix the wire, never the test.
5. **CI gates** must run the integration tests (Go `-tags integration` plus the PowerShell verify scripts where applicable). If CI does not run them, CI is not vouching for the connector.
6. **Cross-protocol regressions are blockers** — any PR that touches shared layers (transport, manifest, storage, `cmd/dhs/*`) re-verifies every connector's integration test before merge per `feedback_no_cross_protocol_regression`.

## Per-protocol scope reminders

| Protocol | Scope clarification |
|---|---|
| ACP1 | All wire versions per `internal/acp1/CLAUDE.md`. |
| ACP2 | AN2/TCP only per `internal/acp2/CLAUDE.md`. |
| Ember+ | **DTD 2.60 only** — older DTD variants are not in scope. Spec p.85 ParameterContents, p.86 Commands, p.88-89 Matrix + ConnectionOperation. |
| Probel SW-P-08 | All §3.2 commands + general AND extended form per command. Form selection is a tested boundary decision (`needsExtended()` ceilings per `feedback_probel_form_selection`). Salvo behaviour per `feedback_probel_salvo_connected`. |
| Probel SW-P-02 | All §3 commands; protect-blocks-connect with state echo (`feedback_probel_protect_blocks_connect`). |
| OSC | osc-v10 + osc-v11; UDP + TCP-LP (v10) + TCP-SLIP (v11). |
| TSL UMD | v3.1 + v4 + v5 with per-vendor positional tally mapping (`project_tsl_extensions`). |
| Cerebrum NB | XML-over-WS uppercase wire form; consumer 12 verbs; provider deferred. |
| AMWA NMOS | Every published minor version in scope — no deferrals per `feedback_amwa_strict_all_versions`. |

## What this ADR does NOT change

- ADR-0001 — connectors still each live under `internal/<proto>/`.
- ADR-0006 — codec packages stay stdlib-only.
- ADR-0013 — one approved unit = one commit. ADR-0025 narrows the
  *unit*: each unit advances one deliverable for one connector.
- ADR-0014 — issue → branch → tests → PR → green → approval → merge.
- ADR-0015 — every architectural rule has one ADR. ADR-0025 is the
  single source for "connector definition of done" and is referenced
  from `README.md`, `CLAUDE.md`, per-protocol `CLAUDE.md`, and
  `memory/project_connector_definition_of_done.md`.

## Revisions

- 2026-05-16 — initial proposal (issue #447).
- 2026-05-17 — added deliverable #6 (replay fixture set under
  `internal/<proto>/testdata/`). Every connector must commit a replay
  surface (raw `.pcapng` + canonical `.tree` + per-type `.md`) so CI and
  any developer can re-decode the wire at any time without needing the
  device. Reference shape: `internal/acp1/testdata/`. Promotion rules
  from `captures/` to `testdata/` already documented in
  `captures/README.md`.
