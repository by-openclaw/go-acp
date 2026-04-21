# acp

Go toolset to discover, connect, monitor, and control devices that
speak the ACP protocol family (ACP1, ACP2, and future Ember+).

Two binaries share one internal library:

| Binary      | Purpose                                      |
|-------------|----------------------------------------------|
| `acp`       | CLI — direct device I/O, no server           |
| `acp-srv`   | HTTP REST + WebSocket API for `acp-ui`       |

`acp-ui` is a separate React 19 repo; it consumes `acp-srv` only.

---

## Protocols

Each protocol has a dedicated connector page with transport, firewall rules,
identity, object types, discovery, get/set, subscriptions, export, capture,
and CLI examples.

| Protocol | Transport | Port | Consumer | Provider | Documentation |
|---|---|---|---|---|---|
| ACP1 | UDP / TCP direct | 2071 | done, canonical-aligned | ✅ merged (#74), SynapseSetUp + Lawo VSM Controller validated, `--announce-demo` ticker (#81) | [docs/protocols/acp1/consumer.md](docs/protocols/acp1/consumer.md) |
| ACP2 | AN2/TCP | 2072 | done, canonical alignment pending (#32) | 🟡 PR #76, 5/6 object types Lawo-VSM-validated; Enum (pid 15) parked in #79 pending Cerebrum | [docs/protocols/acp2/consumer.md](docs/protocols/acp2/consumer.md) |
| Ember+ | S101/TCP | 9000-9092 | consumer done (resolver + multi-level labels) | ✅ merged (#67 + #72) | [docs/protocols/emberplus/consumer.md](docs/protocols/emberplus/consumer.md) |
| Probel SW-P-02 | TCP | — | planned (audit YELLOW) | — | [memory: project_probel_extensions.md] |
| Probel SW-P-08+ | TCP | 2008 | 🟡 PR #84 — scaffold + 11 commands end-to-end (Crosspoint Interrogate/Connect/TallyDump, Maintenance, Dual Controller, Protect ×6) | 🟡 PR #84 — paired provider per command, demo 2×64×64 matrix | [memory: project_probel_sw08.md] |
| TSL UMD v3.1/v4/v5 | UDP push | — | planned (audit GREEN) | — | [memory: project_tsl_extensions.md] |

Canonical JSON schema shared across all protocols: [docs/protocols/schema.md](docs/protocols/schema.md).
Per-type element docs with realistic samples: [docs/protocols/elements/](docs/protocols/elements/).

---

## 2. CLI — export / import

| Command | Description | ACP1 | ACP2 |
|---|---|---|---|
| `acp export --format json` | Hierarchical tree + values to JSON | done | done |
| `acp export --format yaml` | Hierarchical tree + values to YAML | done | done |
| `acp export --format csv` | Flat rows with `oid`, `path`, `id`, `label` columns — **lossless round-trip** for writable objects | done | done |
| `acp import --file X.json` | Apply values from JSON file | done | done |
| `acp import --file X.yaml` | Apply values from YAML file | done | done |
| `acp import --file X.csv` | Apply values from CSV file | done | done |
| `acp import --dry-run` | Validate without writing | done | done |
| `acp import --id N` / `--path P` | **Selective import** — narrow apply set to specific object(s). Mutually exclusive flags; both repeat. No `--label` (labels collide thousands of times) | done | done |
| `acp extract` | Walk a device and emit the DM fixture triple (`meta.json` + `wire.jsonl` + `tree.json`) into `tests/fixtures/products/<manufacturer>/<product>/<protocol>/<direction>/<version>/` where `<direction>` is `consumer`/`provider`/`both`. `meta.json` carries the SHA-256 fingerprint + `capture_tool` build provenance (version + git_tag + git_commit). | done | done |
| `acp diff` | Compare two canonical `tree.json` files by OID; classify every field change as **Breaking** / **Changed** / **Added** / **Removed**. Two formats: `text` (terminal report) and `changelog` (Keep-a-Changelog markdown). With `--into PATH` auto-maintains per-product `CHANGELOG.md`. Offline, all 3 protocols. | done | done |

Export/import rules:
- Resolver key per protocol: **Ember+ = `oid`** (numeric dotted path), **ACP1 = `group` + `id`**, **ACP2 = `id`** (globally-unique u32)
- Read-only objects are skipped on import
- Values in export are live (from walk)
- Round-trip: export → edit → import works for all 3 formats. CSV carries `oid` + `path` + `id` + `label` columns so duplicate labels (Ember+ `gain` per channel, ACP2 `Present` per PSU) round-trip unambiguously
- Hierarchical tree format: identity > Card name > {id, kind, value}

### CSV column contract (issue #38)

| Column | Purpose | Used by importer |
|---|---|---|
| `oid` | Ember+ numeric dotted path (`1.2.1.3`). Empty for ACP1/ACP2. | Ember+ resolver (preferred) |
| `path` | Slash-joined tree path (`router/inputs/ch1/gain`). | Ember+ fallback; hierarchical identity for humans |
| `id` | ACP1 ObjectID (byte), ACP2 obj-id (u32), Ember+ sibling Number. | ACP1 + ACP2 resolvers (primary) |
| `label` | Human-readable identifier. | ACP1 resolver (with `group`); last-resort elsewhere |

`acp convert --in device.json --out device.csv` followed by `acp import device.csv --dry-run` prints `applied N, skipped M, failed 0` on an unchanged device — the **zero-failed contract** is what guarantees CSV round-trip works.

### Import summary line

`acp import` prints `applied N, skipped M, failed X` when it finishes. The
counts mean:

| Count | What it is | Not an error |
|---|---|---|
| `applied` | RW objects the importer wrote (or would write, in `--dry-run`) | — |
| `skipped` | Objects deliberately not attempted: read-only (no W bit), node containers (BOARD / IDENTITY / PROCESSING VIDEO / …), sub-group markers | ✅ expected |
| `failed` | `SetValue` returned an error from the device (`no access`, `invalid value`, …) | ❌ real failure |

On an ACP2 slot 1 export of ~39 k objects, `skipped ≈ 16 k` is normal —
that's the read-only metric / identity / structural subtree the spec
defines as non-writable. Only `failed > 0` indicates a problem.

---

## 3. CLI — commands

Every command has a fixed **IN / OUT** contract. Run `acp help <cmd>` for the full detail; the summary below is the one-line form.

| Command | IN | OUT |
|---|---|---|
| `info` | `acp info <host>` | device info + per-slot status |
| `walk` | `acp walk <host> --slot N` | one line per object (+ `raw.<transport>.jsonl` / `tree.json` under `--capture <dir>` — `raw.acp1` / `raw.an2` / `raw.s101` per protocol) |
| `get` | `acp get <host> --slot N --label L \| --id I` | decoded value + metadata (range, step, unit, enum) |
| `set` | `acp set <host> --slot N --id I --value V` | confirmed value echoed by device (or typed error) |
| `watch` | `acp watch <host> [filters]` | stream of live announcements until Ctrl-C |
| `export` | `acp export <host> --format json\|yaml\|csv --out FILE` | snapshot file (json/yaml lossless, csv flat) |
| `import` | `acp import <host> --file SNAPSHOT [--id N ...\| --path P ...] [--dry-run]` | `applied N, skipped M, failed X[, filtered Y]`; dry-run also prints per-reason skip table |
| `extract` | `acp extract <host> --protocol P --manufacturer M --product X --direction D --version V --out DIR` | meta.json + wire.jsonl + tree.json into the product fixture layout + SHA-256 fingerprint |
| `diff` | `acp diff <before-tree.json> <after-tree.json> [--format text\|changelog] [--into PATH]` | OID-matched semantic diff; text report or Keep-a-Changelog markdown section |
| `convert` | `acp convert --in FILE --out FILE` **(offline)** | same snapshot in the requested format, no device needed |
| `discover` | `acp discover` | list of ACP1 devices on the local subnet (ACP1 only) |
| `matrix` | `acp matrix <host> --path P --target N --sources N,N,...` | confirmed crosspoint connections (Ember+ only) |
| `invoke` | `acp invoke <host> --path P [--args v1,v2,...]` | function return value(s) (Ember+ only) |
| `stream` | `acp stream <host> [--id N]` | live stream parameter values until Ctrl-C (Ember+ only) |
| `profile` | `acp profile <host>` | compliance classification + event counts |
| `diag` | `acp diag <host> --slot N` | per-probe success/failure table (ACP2 only) |
| `list-protocols` | `acp list-protocols` | table of registered plugins |
| `help` | `acp help <cmd>` | detailed IN/OUT + flags + examples for one command |

### Filtering

| Flag | Description | Example |
|---|---|---|
| `--path` | Subtree prefix filter (`.` separator) | `--path BOARD`, `--path PSU.1`, `--path router.oneToN` |
| `--filter` | Text search on output | `--filter Temperature` |
| `--path + --filter` | Combine both | `--path PSU --filter Temperature` |

### Global flags

| Flag | Description | Default |
|---|---|---|
| `--protocol` | Protocol plugin | `acp1` |
| `--port` | Override default port | auto |
| `--timeout` | Per-operation timeout | `30s` |
| `--log-level` | trace/debug/info/warn/error/critical | `info` |
| `--verbose` | Shortcut for `--log-level debug` | false |
| `--capture` | Traffic capture. If value is a directory or has no `.jsonl` ext, writes `raw.<transport>.jsonl` (`raw.acp1` / `raw.an2` / `raw.s101` named after the protocol's wire framing) + `tree.json` (all 3 protocols) + `glow.json` (Ember+ only, canonical resolver output). Single file (`.jsonl`) keeps legacy single-stream log. | — |
| `--templates` | Canonical export mode for `templateReference` (Ember+). `pointer` = wire-faithful, `inline` = absorb template shape into referring element, `both` = keep ref + absorbed shape. | `pointer` |
| `--labels` | Canonical export mode for matrix labels (Ember+). `pointer` = wire-faithful (multi-level `labels[]` preserved), `inline` = absorb label subtree(s) into matrix (populates `targetLabels`/`sourceLabels` keyed by level description), `both` = keep pointer + absorbed maps. | `pointer` |
| `--gain` | Canonical export mode for `parametersLocation` (Ember+). `pointer` = wire-faithful, `inline` = absorb params subtree (populates `targetParams`/`sourceParams`/`connectionParams`), `both` = both. | `pointer` |
| `--transport` | ACP1: `udp` or `tcp` | `udp` |

### Ember+ watch output columns

`time | oid | path | label | acc | fr | value | desc="..." | changed: name old→new, ...`

- `acc`: R / W / RW (access bitmask).
- `fr`: `live` / `updated` (stream tick) / `stale` (cached, session dead or awaiting refresh) / `cache` (loaded from disk, not confirmed).
- `changed:` appears only when a field moved since the last notification; includes value, description, access, min/max/step/default, format, factor, formula, enumeration, streamIdentifier, isOnline.
- Matrix crosspoint changes render as `t=N ← [sources] op=<o> disp=<d>` instead of the value column.
- Session disconnect fires a synthetic event on the root with `changed: isOnline y→n, reason <...>`. Auto-reconnect (2s→30s backoff) re-walks + re-subscribes on return.

### Ember+ write confirmation

`acp set` waits for the provider's confirming announce. Possible outcomes:

| Error | Meaning |
|---|---|
| `protocol: not connected` | Session down; no wire traffic sent. |
| `protocol: write confirmation timeout` | Sent but provider didn't echo within 3s. Tree value unchanged. |
| `protocol: write accepted but value coerced: expected=X actual=Y` | Provider clamped/rounded. Returned value is what the provider applied. |
| `protocol: write rejected by provider` | Provider echoed unchanged — likely lock / offline. |

---

## Architecture

See [docs/CONNECTOR.md](docs/CONNECTOR.md) for the full connector architecture,
data model library, Ember+ terminology, and provider mode design.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the three-layer system overview.

See [CLAUDE.md](CLAUDE.md) for the complete protocol reference (ACP1 + ACP2 wire format).

---

## Getting started

```bash
# Build
make build                    # → bin/acp, bin/acp-srv

# Setup pre-commit hooks
make setup

# Test
make test                     # unit tests
make lint                     # golangci-lint

# Run
bin/acp info 10.6.239.113
bin/acp walk 10.6.239.113 --slot 0
bin/acp walk 10.41.40.195 --protocol acp2 --slot 0 --path BOARD

# Integration tests (need device access)
ACP1_TEST_HOST=10.6.239.113 make test-integration-acp1
ACP2_TEST_HOST=10.41.40.195 make test-integration-acp2
```

---

## Repository layout

```
cmd/acp/              CLI (14 files, split by command)
cmd/acp-srv/          HTTP + WebSocket server (planned)
internal/
  protocol/           IProtocol interface, registry, shared types
    acp1/             ACP1 plugin (UDP/TCP, codec, walker, cache)
    acp2/             ACP2 plugin (AN2/TCP, codec, walker, cache)
    _template/        Starting point for new protocols
  transport/          UDP/TCP/AN2 framer, traffic capture
  export/             JSON/YAML/CSV export + import
  storage/            File-backed tree store
  logging/            Structured logging (slog, Loki-compatible)
assets/
  acp1/               ACP1 spec PDF + Wireshark dissector
  acp2/               ACP2 spec PDF + Wireshark dissector
  emberplus/          Ember+ spec PDF + Wireshark dissector
tests/unit/           Table-driven + replay tests
tests/integration/    Real-device tests (build tag)
tests/smoke/          Simple-path sanity per protocol
tests/fixtures/       Version-controlled test input (walk captures + export fixtures)
docs/                 Architecture, connector design, references
```

## Debugging with Wireshark

All three protocols ship Lua dissectors under `assets/`. Install once (copy
into your Wireshark personal plugins directory) and live captures from
`acp walk` / `acp watch` / `acp extract` are auto-decoded. Full install +
filter guide: [docs/wireshark.md](docs/wireshark.md).

**Info column content (PR #80, 2026-04-21):** ACP1 and ACP2 Info columns
now carry short type / mtid / dotted OID path / typed value inline, so
you can scroll through a capture without expanding every tree:

```
ACP1 Req slot=1 mtid=0x4A2F setValue control.3 value(s16)=-30
ACP1 Ann slot=1 mtid=0x0   setValue control.0 value(s16)=-10
AN2 > ACP2 Rep mtid=116 SetProperty 0.3 pid=value value(string)="ACP2-Frame"
AN2 > ACP2 Evt mtid=0   Announce    pid=value 1.18 value(float)=-35.3
```

Announce detection uses MTID=0 per spec "Announcements" — MType=2+MTID=0
is labelled `Ann`, not `Rep`. Per-value type tagging (s8/s16/s32/s64/u8/u16/u32/u64/float/enum/ipv4/string)
matches the declared object category derived from the ACP2 vtype byte.

---

## License

Copyright (c) 2026 BY-SYSTEMS SRL — [www.by-systems.be](https://www.by-systems.be)

All rights reserved. See [LICENSE.md](LICENSE.md) and [COPYRIGHT](COPYRIGHT).
