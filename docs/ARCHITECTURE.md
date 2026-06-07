# Architecture

## Three layers

```text
┌──────────────────────────────────────────────────┐
│  Serialization layer                             │
│  JSON / YAML / CSV                               │
│  internal/export/                                │
├──────────────────────────────────────────────────┤
│  Normalized layer                                │
│  consumer.Object  consumer.Value                 │
│  internal/consumer/types.go                      │
├──────────────────────────────────────────────────┤
│  Wire layer (per-protocol)                       │
│  internal/<proto>/{codec,consumer,provider}/     │
│  internal/transport/                             │
└──────────────────────────────────────────────────┘
```

- **Wire layer**: protocol-specific encode/decode. Each protocol lives in
  `internal/<name>/{codec,consumer,provider}/` and speaks its own
  binary format.
- **Normalized layer**: `consumer.Object` is the single shared type that
  CLI, REST API, storage, and export all consume. Every per-protocol
  plugin fills it with its superset of metadata.
- **Serialization layer**: converts `consumer.Object` to JSON, YAML, or CSV
  for export/import.

## Plugin model

Compile-time registration via `init()`. Each protocol package calls
`consumer.Register(&Factory{})` at import time. The CLI and server
main files import the protocol packages as blank imports:

```go
import _ "dhs/internal/acp1/consumer"
import _ "dhs/internal/acp2/consumer"
import _ "dhs/internal/acp1/provider"
import _ "dhs/internal/acp2/provider"
```

No runtime plugin loading. No external config. Adding a protocol means
adding one import line.

## Future direction

- Each protocol will eventually be its own repository/module
- The REST API (`dhs-srv`) imports the library, does not access protocol
  code directly
- Documentation is split into small focused files, not one monolith

## Binaries

| Binary    | Purpose                              |
|-----------|--------------------------------------|
| `dhs`     | CLI -- direct device I/O, no server  |
| `dhs-srv` | HTTP REST + WebSocket API            |

Both share `internal/`. Neither imports the other.

## Shared infrastructure (non-connector packages)

Per ADR-0001 each protocol lives under `internal/<proto>/{consumer,
provider, codec?, wireshark, assets}` with its own `CLAUDE.md`. The
folders below are **not** connectors — they are the neutral substrate
every connector imports. They have no spec PDF, no `consumer/`, no
`provider/`. Each is documented in its first `.go` file's
`// Package …` header (Go-idiomatic). The table indexes them so a
reader knows at a glance what each is for and whether it can be moved.

| Folder | Purpose | Linked from | ADR |
|---|---|---|---|
| `internal/consumer/` | neutral consumer-plugin registry + `Protocol` interface + compliance event hooks + canonical validator | every consumer plugin (`init()` calls `consumer.Register`), `cmd/dhs/*` | ADR-0001, ADR-0015 |
| `internal/provider/` | neutral provider-plugin registry + `Provider` interface | every provider plugin, `cmd/dhs/cmd_producer.go` | ADR-0001 |
| `internal/transport/` | UDP / TCP / AN2 framer + `--capture` JSONL recorder | every connector that talks IP; `cmd_validate.go` | ADR-0020 (capture layout), ADR-0021 (JSONL contract) |
| `internal/wiretrace/` | wire-trace JSONL reader/writer | `cmd/dhs/cmd_validate.go`, every connector's `replay_test.go` | ADR-0021 |
| `internal/manifest/` | Card / Frame / Slot / Card / DM manifest loader + canonical export | every per-product DM lookup; `cmd_producer.go`; consumers when seeding tree from cache | ADR-0022 (Card data model) |
| `internal/diff/` | semantic diff of two canonical trees (`Object` / `Frame` / `Slot`) used by the `diff` CLI verb | `cmd/dhs/cmd_diff.go` | — |
| `internal/export/` | JSON / YAML / CSV exporter + importer over `consumer.Object` | `cmd/dhs/cmd_export.go`, `cmd_import.go` | — |
| `internal/scenario/` | scenario-driven test runner — replays a JSONL scenario against a live or mock connector | every connector's `replay_test.go` under `-tags integration` | ADR-0021 |
| `internal/metrics/` | neutral `ConnectorMetrics` surface (rx/tx/bytes/latency p50/p95/p99/errors/mem/cpu) every plugin exposes through `Metrics()` | every plugin's session, `cmd/dhs/cmd_metrics.go`, `--metrics-addr` flag | — |
| `internal/identity/` | sanitiser helpers for vendor-supplied identity strings (vendor / product / serial / mac) | identity orchestrator across all connectors | — |
| `internal/logging/` | `log/slog` structured logging primitives — direction + source-path + severity tiers, Loki JSON output | every plugin and transport layer | — |
| `internal/registry/` | cross-protocol session catalog — what device is online on which connector, last-seen, health | `cmd/dhs/common.go` keep-alive supervisor | — |

**Moving any of these requires a new ADR.** Doing so silently would
break every connector's import graph; they exist *because* the
per-connector code is intentionally thin and offloads cross-cutting
concerns here.

## Future-only or partial entries

| Folder | State |
|---|---|
| `internal/snell-rollcall/` | **future protocol**, gated on every current connector first satisfying the ADR-0025 six-deliverable bar. Today the directory only holds `assets/` with a gitignored local vendor SDK dump (1656 files of `.tpl` / `.mib` / `.zip` / `.exe` / `.doc`) — already laid out per ADR-0001 so when work begins the scaffolded `consumer/` / `provider/` / `codec/` / `wireshark/` / `CLAUDE.md` land alongside it. No Go code, no registry entry yet. |
| `internal/cerebrum-nb/provider/` | **consumer-only by design at this stage** — only the consumer + codec + wireshark layers are shipped; no `provider/` folder exists yet on disk. Intentionally not in scope at the current stage per `internal/cerebrum-nb/CLAUDE.md`. |

---

Copyright (c) 2026 BY-SYSTEMS SRL — <https://www.by-systems.be>
