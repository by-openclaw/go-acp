# dhs — Device Hub Systems

Go toolset to discover, connect, monitor, and control devices across many
protocols: **ACP1**, **ACP2**, **Ember+**, **Probel SW-P-08 / SW-P-88**,
**Probel SW-P-02**, **OSC**, **TSL UMD**, **EVS Cerebrum NB**, and (planned)
**AMWA NMOS**.

One binary covers both directions:

| Command form                      | Role                              |
|-----------------------------------|-----------------------------------|
| `dhs consumer <proto> <verb> ...` | Outbound — query / control device |
| `dhs producer <proto> serve ...`  | Inbound  — serve a canonical tree |

---

## Connector definition of done (ADR-0025)

A connector is **DONE** only when all five deliverables exist together (per [ADR-0025](docs/adr/0025-per-connector-definition-of-done.md)). No "ship now, finish later" — fixes that build on partial connectors cause the regression-and-circle pattern this rule prevents.

The five deliverables, one column each in the per-connector state table below:

1. **Consumer** — strict-spec, every CLI verb the spec defines (Probel general/extended form selection is a tested boundary decision per command; Ember+ is DTD 2.60 only).
2. **Producer** — strict-spec, every CLI verb the spec defines.
3. **Integration test (CLI)** — Go tests under `internal/<proto>/integration/` that drive the actual `dhs consumer/producer <proto> <verb>` binary against a live producer. Per-test classification: PASS / FAIL-real / FAIL-expected (error-handling success) / TIMEOUT. PowerShell verify scripts can sit alongside for live-rig parity.
4. **`.cache/dm` + manifest generator** — Go generator owned by the connector, called from test `setUp`, writes a local `.cache/dm/<proto>/...` + `.cache/manifest/...` next to the test files. Tests **MUST NOT** `t.Skip` when the cache is empty.
5. **Wireshark `.lua` dissector** — `internal/<proto>/wireshark/dhs_<proto>.lua` covering every transport + version + command with per-frame Info column detail that uniquely identifies the message.

Legend: ✅ done · 🟡 partial · ❌ missing or stub

| Connector | Consumer | Producer | Integration test (CLI) | `.cache` generator | Wireshark `.lua` | Notes |
|---|---|---|---|---|---|---|
| **ACP1** | ✅ all verbs | ✅ | 🟡 smoke (4 funcs) — does not drive full CLI surface | ❌ | ✅ `dhs_acpv1.lua` | Needs CLI integration test covering walk/get/set/watch/discover end-to-end |
| **ACP2** | ✅ all verbs | ✅ | ❌ `t.Skip("not implemented yet")` placeholder | ❌ | ✅ `dhs_acpv2.lua` | Integration test is a stub. Every PR claiming "ACP2 green" since this file landed has not been ACP2-tested at all. |
| **Ember+ (DTD 2.60)** | 🟡 — dotted-path `resolveMatrix` bug fixed locally, not yet merged | 🟡 — same | 🟡 `scripts/emberplus/verify-emberplus-integration.ps1` (22 assertions, labels + per-tgt/src/XPT params only). No matrix-connect, no salvo, no reject paths, no streams. | ❌ — `scripts/emberplus/gen-emberplus-demo-dms.ps1` is PowerShell, not Go, lives outside the integration test folder | ✅ `dhs_emberplus.lua` | Highest-priority connector to bring to DoD next. |
| **Probel SW-P-08** | ✅ all §3.2 cmds; general/extended form selection covered in `codec/` tests | ✅ multi-session tally + salvo fan-out validated | 🟡 heavy codec coverage (151 funcs) + smh emulator validation, but no `internal/probel-sw08p/integration/` CLI-binary tests | ❌ | ✅ `dhs_probel_sw08p.lua` | Codec layer exemplary; CLI integration layer absent. |
| **Probel SW-P-02** | ✅ all §3 cmds; protect-blocks-connect with state echo | ✅ | 🟡 codec + provider unit tests; no CLI-binary integration | ❌ | ✅ `dhs_probel_sw02p.lua` | |
| **OSC 1.0 / 1.1** | ✅ | ✅ | ✅ 6 files / 23 funcs under `internal/osc/integration/` — closest thing to a reference shape | ✅ pcap replay fixture + osc.js byte oracle | ✅ `dhs_osc.lua` | Reference for what a complete connector looks like under this ADR. |
| **TSL UMD v3.1/v4/v5** | ✅ | ✅ | 🟡 4 files / 10 funcs — partial CLI coverage | ✅ testdata fixtures | ✅ `dhs_tsl.lua` | |
| **EVS Cerebrum NB** | ✅ 12 verbs (UPPERCASE wire-form live-verified on production fleet) | ❌ deferred | ❌ 2 consumer unit tests, no CLI integration | ❌ | ✅ `dhs_cerebrum_nb.lua` | Provider intentionally deferred per scope. |
| **AMWA NMOS** | 🟡 IS-04 walk + IS-04 Controller; IS-05/07/08/12/MS-05 plugins in-flight | 🟡 Node + Registry + Events serve | 🟡 AMWA Testing tool harness (external Python) — full conformance on IS-04-01/02 v1.0–v1.3 | 🟡 AMWA fixtures | 🟡 HTTP/WS layer in dissector | Spec-strict every published minor per root CLAUDE.md "AMWA NMOS strict" |

### What's NOT YET tested (and why)

| Connector | Untested area | Blocker |
|---|---|---|
| Ember+ | Real Lawo router (production hardware) — DTD 2.60 + matrices + streams | Waiting for VPN restoration to reach the production rig (codeowner note 2026-05-16) |
| Ember+ | DHD provider full surface (1605 stream-param fanout) | Need a live DHD sample on the test rig — TinyEmberPlus DHD_Example1 covers the wire bug surface but not throughput/load |
| Probel | Real EMTWO / Lawo VSM matrix at full N×M scale | Waiting for cross-vendor testbed access |
| ACP2 | Real Synapse cards with enum-typed objects (#79) | Awaiting card sample with enum property exposure |
| NMOS | Real-peer Cerebrum NMOS HA failover under load | Production rig access |

Per-connector runbook (every CLI verb with a captured real-run example) is a separate deliverable per connector — tracked under `internal/<proto>/docs/runbook.md`.

---

## Protocols

| Protocol        | Transport         | Port      | Consumer | Provider | Docs |
|-----------------|-------------------|-----------|----------|----------|------|
| ACP1            | UDP / TCP direct  | 2071      | ✅       | ✅       | [internal/acp1/CLAUDE.md](internal/acp1/CLAUDE.md) · [internal/acp1/docs/consumer.md](internal/acp1/docs/consumer.md) |
| ACP2            | AN2/TCP           | 2072      | ✅       | 🟡 PR #76 (5/6 types Lawo-validated; Enum parked in #79) | [internal/acp2/CLAUDE.md](internal/acp2/CLAUDE.md) · [internal/acp2/docs/consumer.md](internal/acp2/docs/consumer.md) |
| Ember+          | S101/TCP          | 9000-9092 | ✅ on main | ✅ on main · 🟡 PR #135 (BER REAL ecosystem mantissa bias + S101 BoF resync, closes #68) + PR #136 (broadcast value-change to all active sessions) in flight 2026-04-26 — both live-validated against EmberViewer v2.40.0.35 + Lawo VSM Studio concurrent | [internal/emberplus/CLAUDE.md](internal/emberplus/CLAUDE.md) · [internal/emberplus/docs/consumer.md](internal/emberplus/docs/consumer.md) |
| Probel SW-P-08+ | TCP               | 2008      | 🟡 PR #84 (all §3.2 cmds; VSM+Commie+TS validated) | 🟡 PR #84 (all §3.2 cmds; multi-session tally + salvo fan-out live-validated against VSM + Commie; #92 resolved) | [internal/probel-sw08p/CLAUDE.md](internal/probel-sw08p/CLAUDE.md) |
| Probel SW-P-02  | TCP               | 2002      | ✅ merged on main (PR #106 closed #105, PR #132 closed #128/#129/#130) — 33 bytes + Wireshark; owner-only protect auth + protect-blocks-connect with state echo; consumer matrix-config flags (`--mtx-id --level --dsts --srcs`) + bootstrap rx 01 sweep + 2 s rotating keep-alive ping (mirrors VSM) + TCP SO_KEEPALIVE; VSM + Commie validated 2026-04-25 | ✅ merged on main | [internal/probel-sw02p/CLAUDE.md](internal/probel-sw02p/CLAUDE.md) |
| TSL UMD v3.1/v4/v5 | v3.1 UDP-only · v4.0 UDP-only · v5.0 UDP **and** TCP/DLE-STX | v3.1 4000 · v4.0 4004 (testbed) · v5.0 8901 UDP / 8902 TCP (testbed) | ✅ merged on main (PR #134 closed #119/#120/#121/#122) — codec + consumer + provider for all 3 versions; `dhs_tsl.lua` Wireshark dissector; consumer `listen` + producer `send` / `serve` CLI verbs (v5.0 multi-DMSG via repeatable `--dmsg "index=N,lh=...,text-tally=...,rh=...,brightness=...,umd=STR"`); SO_KEEPALIVE 30 s on v5 TCP; **VSM + Miranda IP Emulator interop validated 2026-04-26** | ✅ merged on main | [internal/tsl/CLAUDE.md](internal/tsl/CLAUDE.md) |
| OSC 1.0 / 1.1   | UDP; TCP length-prefix (1.0) or TCP SLIP double-END (1.1) | 8000 (UDP + TCP-LP), 8001 (TCP-SLIP) — all configurable | ✅ merged on main (PR #125 closed #123 + #124) — osc-v10 + osc-v11 codec + every type tag incl. `[ ]` arrays + bundles + full address-pattern matcher; consumer `watch` + producer `send` / `fader` / `serve` CLI verbs; full `dhs_osc.lua` Wireshark dissector with typed Info column; osc.js cross-impl byte oracle; pcap replay fixture; **76 tests** | ✅ merged on main | [internal/osc/CLAUDE.md](internal/osc/CLAUDE.md) |
| EVS Cerebrum NB | XML over WebSocket (ws/wss) | 40007 (configurable) | ✅ merged on main (PR #144 closed #143; v0.6.0 tagged 2026-04-30) — codec + WS framing + 12 CLI verbs (`connect` / `listen` / `route` / `list-devices` / `device-details` / `device-value` / `list-categories` / `category-details` / `list-salvo-*` / `keepalive-probe`) + `dhs_cerebrum_nb.lua` + portable Windows binary; UPPERCASE wire form + listen + route live-verified on the .95 production fleet 2026-04-30 (redundancy + license behaviour confirmed). | deferred | [internal/cerebrum-nb/CLAUDE.md](internal/cerebrum-nb/CLAUDE.md) · [internal/cerebrum-nb/docs/consumer.md](internal/cerebrum-nb/docs/consumer.md) |
| AMWA NMOS       | DNS-SD + HTTP/JSON REST + WebSocket; (IS-07 MQTT tracker #185) | per-API (DNS-SD discovered) | 🟢 codec / 🟡 plugin + integration — epic #146; multi-version IS-04 / IS-05 / IS-07 / IS-08 / IS-09 / IS-12 / MS-05-01/02 / BCP-002/004/006/008 codec layer landed; CLI today `dhs consumer nmos {discover,system,walk,events watch}` | 🟢 codec / 🟡 Registry + Node serving | [internal/amwa/CLAUDE.md](internal/amwa/CLAUDE.md) · [internal/amwa/reference.md](internal/amwa/reference.md) · [internal/amwa/docs/architecture.md](internal/amwa/docs/architecture.md) |

Canonical JSON schema shared across all protocols:
[docs/protocols/schema.md](docs/protocols/schema.md).
Per-type element docs: [docs/protocols/elements/](docs/protocols/elements/).

---

## CLI

### Consumer verbs (acp1 / acp2 / emberplus)

```
info       read device info (slot count, per-slot status)
walk       enumerate every object on a slot
get        read one object value
set        write one object value
watch      subscribe to live announcements
export     dump a walked device to json / yaml / csv
import     apply values from a snapshot file
extract    capture a per-product DM triple (meta + wire + tree)
diff       compare two canonical tree.json files
convert    translate a snapshot file between json / yaml / csv (offline)
discover   passive + active scan for devices on the local subnet (ACP1)
matrix     set matrix crosspoint connections (Ember+ only)
invoke     invoke an Ember+ function (RPC)
stream     subscribe to Ember+ stream parameters
profile    classify provider compliance (strict / partial)
diag       run ACP2 diagnostic probes against a device
```

### Consumer verbs (probel-sw08p)

```
interrogate connect tally-dump watch maintenance dual-status
protect-interrogate protect-connect protect-disconnect
protect-name protect-dump master-protect
all-source-names single-source-name
all-dest-names   single-dest-name
all-source-assoc-names single-source-assoc-name
discover   (one-shot dual-status + names + tally-dump)
...        (see `dhs consumer probel-sw08p -h`)
```

### Producer

```
dhs producer <proto> serve --tree FILE.json [--port N] [--host H]
                           [--announce-demo ...]
```

### Examples

```bash
# ACP1
dhs consumer acp1      walk        10.6.239.113
dhs consumer acp1      get         10.6.239.113 --slot 1 --label GainA
dhs consumer acp1      set         10.6.239.113 --slot 1 --label GainA --value 50.0
dhs consumer acp1      discover    --duration 10s

# ACP2
dhs consumer acp2      walk        10.41.40.195
dhs consumer acp2      diag        10.41.40.195 --slot 0

# Ember+
dhs consumer emberplus walk        10.0.0.10:9000
dhs consumer emberplus invoke      10.0.0.10:9000 --path router.salvo.fire
dhs consumer emberplus stream      10.0.0.10:9000

# Probel
dhs consumer probel-sw08p    interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
dhs consumer probel-sw08p    connect     127.0.0.1:2008 --matrix 0 --level 0 --dst 5 --src 12
dhs consumer probel-sw08p    watch       127.0.0.1:2008

# Producer (every protocol)
dhs producer acp1      serve --tree tree.json --port 2071
dhs producer acp2      serve --tree tree.json --port 2072
dhs producer emberplus serve --tree tree.json --port 9000
dhs producer probel-sw08p    serve --tree matrix.json --port 2008
```

### Export / import

Hierarchical JSON / YAML, plus lossless CSV with `oid` + `path` + `id` +
`label` columns so duplicate labels (Ember+ `gain` per channel, ACP2
`Present` per PSU) round-trip unambiguously. See the full column contract
in [docs/protocols/schema.md](docs/protocols/schema.md).

Round-trip guarantee: `dhs consumer <proto> convert --in tree.json --out tree.csv`
followed by `dhs consumer <proto> import --file tree.csv --dry-run` returns
`applied N, skipped M, failed 0` on an unchanged device.

### Global flags

| Flag | Description | Default |
|---|---|---|
| `--port N` | Override default port | auto |
| `--timeout DUR` | Per-operation timeout | `30s` |
| `--log-level LEVEL` | trace / debug / info / warn / error | `info` |
| `--verbose` | Shortcut for `--log-level debug` | false |
| `--capture PATH` | Traffic capture. If `PATH` is a directory or has no `.jsonl` ext, writes `raw.<transport>.jsonl` + `tree.json` (+ `glow.json` for Ember+). Single `.jsonl` keeps legacy single-stream log. | — |
| `--templates <pointer\|inline\|both>` | Ember+ template resolution mode | `pointer` |
| `--labels <pointer\|inline\|both>` | Ember+ matrix-label resolution mode | `pointer` |
| `--gain <pointer\|inline\|both>` | Ember+ parametersLocation resolution mode | `pointer` |
| `--transport <udp\|tcp>` | ACP1 transport | `udp` |

---

## Architecture

See [docs/CONNECTOR.md](docs/CONNECTOR.md) for the full connector design,
data model library, and provider architecture.

[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — system overview.

[CLAUDE.md](CLAUDE.md) — cross-cutting Go conventions, registry pattern,
compliance, error hierarchy, storage rules, **scale targets, performance
metrics, architecture principles**.

[internal/<proto>/CLAUDE.md](internal/) — atomic per-protocol wire-format
context (one file per protocol).

### Scale targets

Every plugin is sized for broadcast-industry minimums: **65 535 × 65 535
crosspoints per matrix** and **20 – 100 matrices per plant**. No dense
arrays, no small-matrix shortcuts; sparse `(matrix, level, dst) → src`
maps and streaming codecs across the board.

### Performance metrics

Every connector exposes rx/tx frame + byte rates, rx→tx handler latency
(p50/p95/p99), NAK/decode errors, and memory + CPU attribution via a
neutral `ConnectorMetrics` accessor. Printed at session close, emitted
as `protocol.Event` ticks, and scrapable from the future dhs-srv.

### Architecture principles

Encapsulation (narrow public surface), dependency injection (no
globals), separation of concerns (codec / consumer / provider /
compliance / wireshark), library independence (codec is stdlib-only,
lift-ready), no hidden state.

---

## Getting started

```bash
# Build
make build                    # -> bin/dhs(.exe)

# Setup pre-commit hooks
make setup

# Test
make test                     # unit tests
make lint                     # golangci-lint

# Run
bin/dhs consumer acp1 info 10.6.239.113
bin/dhs consumer acp1 walk 10.6.239.113 --slot 0
bin/dhs consumer acp2 walk 10.41.40.195 --slot 0 --path BOARD

# Integration tests (need device access)
ACP1_TEST_HOST=10.6.239.113 make test-integration-acp1
ACP2_TEST_HOST=10.41.40.195 make test-integration-acp2
```

---

## Repository layout

```
cmd/dhs/                      single CLI binary (consumer + producer)
internal/
  protocol/                   neutral consumer-plugin registry + iface
  provider/                   neutral provider-plugin registry + iface
  acp1/  acp2/  emberplus/  probel-sw08p/
                              one folder per protocol, self-contained:
                              CLAUDE.md, codec/ (optional), consumer/,
                              provider/, wireshark/, docs/, assets/
  tsl/                        placeholder for future TSL UMD plugin
                              (assets only; no Go code yet)
  transport/                  UDP / TCP / AN2 framer, traffic capture
  export/                     JSON / YAML / CSV export + importer
  scenario/                   scenario-driven test runner
  storage/                    file-backed persistence (planned)
tests/unit/                   table-driven + replay tests
tests/integration/            real-device tests (build tag)
tests/smoke/                  simple-path sanity per protocol
tests/fixtures/products/      per-product DM library (per ADR-0020 Bucket 3)
docs/                         shared meta docs (per ADR-0019 Tier 2):
                              adr/ · CONNECTOR.md · ARCHITECTURE.md · VISION.md
                              wireshark.md · protocols/{schema,use-cases,elements/}.md
                              deployment/

.cache/                       CLI tree cache (gitignored, per ADR-0020 Bucket 4)
captures/                     manual wire-trace archive (gitignored, per ADR-0020 Bucket 4)
```

---

## Debugging with Wireshark

Each protocol ships a Lua dissector under
`internal/<proto>/wireshark/dissector_<proto>.lua`. Install once (copy into
your Wireshark personal plugins directory) and captures taken during
`dhs consumer <proto> walk/watch/extract` auto-decode.

Full install + filter guide: [docs/wireshark.md](docs/wireshark.md).

---

## License

Copyright (c) 2026 BY-SYSTEMS SRL — [www.by-systems.be](https://www.by-systems.be)

All rights reserved. See [LICENSE.md](LICENSE.md) and [COPYRIGHT](COPYRIGHT).
