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

| Protocol | Transport | Port | Status | Documentation |
|---|---|---|---|---|
| ACP1 | UDP / TCP direct | 2071 | done, canonical-aligned | [docs/protocols/acp1/consumer.md](docs/protocols/acp1/consumer.md) |
| ACP2 | AN2/TCP | 2072 | done, canonical alignment pending (#32) | [docs/protocols/acp2/consumer.md](docs/protocols/acp2/consumer.md) |
| Ember+ | S101/TCP | 9000-9092 | consumer done (resolver + multi-level labels) | [docs/protocols/emberplus/consumer.md](docs/protocols/emberplus/consumer.md) |
| Probel SW-P-02 | TCP | — | planned (audit YELLOW) | [memory: project_probel_extensions.md] |
| Probel SW-P-08+ | TCP | — | planned (audit YELLOW) | [memory: project_probel_extensions.md] |
| TSL UMD v3.1/v4/v5 | UDP push | — | planned (audit GREEN) | [memory: project_tsl_extensions.md] |

Canonical JSON schema shared across all protocols: [docs/protocols/schema.md](docs/protocols/schema.md).
Per-type element docs with realistic samples: [docs/protocols/elements/](docs/protocols/elements/).

---

## 2. CLI — export / import

| Command | Description | ACP1 | ACP2 |
|---|---|---|---|
| `acp export --format json` | Hierarchical tree + values to JSON | done | done |
| `acp export --format yaml` | Hierarchical tree + values to YAML | done | done |
| `acp export --format csv` | Flat rows, path in group column | done | done |
| `acp import --file X.json` | Apply values from JSON file | done | done |
| `acp import --file X.yaml` | Apply values from YAML file | done | done |
| `acp import --file X.csv` | Apply values from CSV file | done | done |
| `acp import --dry-run` | Validate without writing | done | done |

Export/import rules:
- Object ID is the primary key (unambiguous)
- Read-only objects are skipped on import
- Values in export are live (from walk)
- Round-trip: export → edit → import works for all 3 formats
- Hierarchical tree format: identity > Card name > {id, kind, value}

---

## 3. CLI — commands

Every command has a fixed **IN / OUT** contract. Run `acp help <cmd>` for the full detail; the summary below is the one-line form.

| Command | IN | OUT |
|---|---|---|
| `info` | `acp info <host>` | device info + per-slot status |
| `walk` | `acp walk <host> --slot N` | one line per object (+ `raw.s101.jsonl` / `tree.json` under `--capture <dir>`) |
| `get` | `acp get <host> --slot N --label L \| --id I` | decoded value + metadata (range, step, unit, enum) |
| `set` | `acp set <host> --slot N --id I --value V` | confirmed value echoed by device (or typed error) |
| `watch` | `acp watch <host> [filters]` | stream of live announcements until Ctrl-C |
| `export` | `acp export <host> --format json\|yaml\|csv --out FILE` | snapshot file (json/yaml lossless, csv flat) |
| `import` | `acp import <host> --file SNAPSHOT [--dry-run]` | `applied N, skipped M, failed X`; dry-run also prints per-reason skip table |
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
| `--capture` | Traffic capture. If value is a directory or has no `.jsonl` ext, writes `raw.s101.jsonl` + `glow.json` + `tree.json` (Ember+ only, canonical shape). Single file (`.jsonl`) keeps legacy single-stream log for ACP1/ACP2. | — |
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
tests/unit/           Table-driven + replay tests
tests/integration/    Real-device tests (build tag)
tests/smoke/          Simple-path sanity per protocol
tests/fixtures/       Version-controlled test input (walk captures + export fixtures)
docs/                 Architecture, connector design, references
```

---

## License

Copyright (c) 2026 BY-SYSTEMS SRL — [www.by-systems.be](https://www.by-systems.be)

All rights reserved. See [LICENSE.md](LICENSE.md) and [COPYRIGHT](COPYRIGHT).
