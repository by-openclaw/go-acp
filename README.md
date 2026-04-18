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
| ACP1 | UDP / TCP direct | 2071 | done | [docs/protocols/acp1/consumer.md](docs/protocols/acp1/consumer.md) |
| ACP2 | AN2/TCP | 2072 | done | [docs/protocols/acp2/consumer.md](docs/protocols/acp2/consumer.md) |
| Ember+ | S101/TCP | 9000-9092 | working | [docs/protocols/emberplus/consumer.md](docs/protocols/emberplus/consumer.md) (planned) |

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

| Command | Description | Example |
|---|---|---|
| `info` | Device info + slot status | `acp info 10.41.40.195 --protocol acp2` |
| `walk` | Enumerate all objects on a slot | `acp walk 10.41.40.195 --protocol acp2 --slot 0` |
| `get` | Read one value by ID or label | `acp get 10.41.40.195 --protocol acp2 --slot 0 --id 15239` |
| `set` | Write one value by ID or label | `acp set 10.41.40.195 --protocol acp2 --slot 0 --id 15239 --value "OK"` |
| `watch` | Live announcements (Ctrl-C to stop) | `acp watch 10.41.40.195 --protocol acp2 --slot 0` |
| `export` | Dump tree + values to file | `acp export 10.41.40.195 --format yaml --out device.yaml` |
| `import` | Apply values from file | `acp import 10.41.40.195 --file device.yaml --dry-run` |
| `discover` | LAN device discovery (ACP1 only) | `acp discover` |
| `diag` | Protocol diagnostic probes (ACP2) | `acp diag 10.41.40.195 --protocol acp2` |
| `list-protocols` | Show registered protocols | `acp list-protocols` |
| `help` | Per-command help | `acp help walk` |

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
| `--capture` | Write raw traffic to JSONL file | — |
| `--transport` | ACP1: `udp` or `tcp` | `udp` |

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
testdata/             Raw captures + export fixtures
tests/unit/           Table-driven + replay tests
tests/integration/    Real-device tests (build tag)
docs/                 Architecture, connector design, references
```

---

## License

Copyright (c) 2026 BY-SYSTEMS SRL — [www.by-systems.be](https://www.by-systems.be)

All rights reserved. See [LICENSE.md](LICENSE.md) and [COPYRIGHT](COPYRIGHT).
