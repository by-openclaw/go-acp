# dhs — Device Hub Systems

Go toolset to discover, connect, monitor, and control devices across four
protocols: **ACP1**, **ACP2**, **Ember+**, and **Probel SW-P-08 / SW-P-88**.

One binary covers both directions:

| Command form                      | Role                              |
|-----------------------------------|-----------------------------------|
| `dhs consumer <proto> <verb> ...` | Outbound — query / control device |
| `dhs producer <proto> serve ...`  | Inbound  — serve a canonical tree |

> Go module path is `acp` (legacy, kept to avoid import churn). Binary and
> CLI are `dhs`.

---

## Protocols

| Protocol        | Transport         | Port      | Consumer | Provider | Docs |
|-----------------|-------------------|-----------|----------|----------|------|
| ACP1            | UDP / TCP direct  | 2071      | ✅       | ✅       | [internal/acp1/CLAUDE.md](internal/acp1/CLAUDE.md) · [internal/acp1/docs/consumer.md](internal/acp1/docs/consumer.md) |
| ACP2            | AN2/TCP           | 2072      | ✅       | 🟡 PR #76 (5/6 types Lawo-validated; Enum parked in #79) | [internal/acp2/CLAUDE.md](internal/acp2/CLAUDE.md) · [internal/acp2/docs/consumer.md](internal/acp2/docs/consumer.md) |
| Ember+          | S101/TCP          | 9000-9092 | ✅       | ✅       | [internal/emberplus/CLAUDE.md](internal/emberplus/CLAUDE.md) · [internal/emberplus/docs/consumer.md](internal/emberplus/docs/consumer.md) |
| Probel SW-P-08+ | TCP               | 2008      | 🟡 PR #84 (all §3.2 cmds; VSM+Commie+TS validated) | 🟡 PR #84 (all §3.2 cmds; multi-session tally + salvo fan-out live-validated against VSM + Commie; #92 resolved) | [internal/probel-sw08p/CLAUDE.md](internal/probel-sw08p/CLAUDE.md) |
| Probel SW-P-02  | TCP               | 2002      | ✅ merged on main (PR #106 closed #105, PR #132 closed #128/#129/#130) — 33 bytes + Wireshark; owner-only protect auth + protect-blocks-connect with state echo; consumer matrix-config flags (`--mtx-id --level --dsts --srcs`) + bootstrap rx 01 sweep + 2 s rotating keep-alive ping (mirrors VSM) + TCP SO_KEEPALIVE; VSM + Commie validated 2026-04-25 | ✅ merged on main | [internal/probel-sw02p/CLAUDE.md](internal/probel-sw02p/CLAUDE.md) |
| TSL UMD v3.1/v4/v5 | UDP push       | —         | planned  | —        | — |
| OSC 1.0 / 1.1   | UDP; TCP length-prefix (1.0) or TCP SLIP double-END (1.1) | 8000 (UDP + TCP-LP), 8001 (TCP-SLIP) — all configurable | ✅ merged on main (PR #125 closed #123 + #124) — osc-v10 + osc-v11 codec + every type tag incl. `[ ]` arrays + bundles + full address-pattern matcher; consumer `watch` + producer `send` / `fader` / `serve` CLI verbs; full `dhs_osc.lua` Wireshark dissector with typed Info column; osc.js cross-impl byte oracle; pcap replay fixture; **76 tests** | ✅ merged on main | [internal/osc/CLAUDE.md](internal/osc/CLAUDE.md) |

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
tests/fixtures/               version-controlled test input
docs/                         cross-cutting only:
                              ARCHITECTURE.md · CONNECTOR.md · VISION.md ·
                              wireshark.md · protocols/schema.md ·
                              protocols/elements/ · examples/ ·
                              deployment/ · references/ ·
                              fixtures-products.md · hub/ · links/
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
