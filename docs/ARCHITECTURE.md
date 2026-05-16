# Architecture

## Three layers

```text
┌──────────────────────────────────────────────────┐
│  Serialization layer                             │
│  JSON / YAML / CSV                               │
│  internal/export/                                │
├──────────────────────────────────────────────────┤
│  Normalized layer                                │
│  protocol.Object  protocol.Value                 │
│  internal/protocol/types.go                      │
├──────────────────────────────────────────────────┤
│  Wire layer (per-protocol)                       │
│  internal/protocol/acp1/   acp2/   {future}/    │
│  internal/transport/                             │
└──────────────────────────────────────────────────┘
```

- **Wire layer**: protocol-specific encode/decode. Each protocol lives in
  `internal/protocol/{name}/` and speaks its own binary format.
- **Normalized layer**: `protocol.Object` is the single shared type that
  CLI, REST API, storage, and export all consume. Both ACP1 and ACP2
  plugins fill it with their superset of metadata.
- **Serialization layer**: converts `protocol.Object` to JSON, YAML, or CSV
  for export/import.

## Plugin model

Compile-time registration via `init()`. Each protocol package calls
`protocol.Register(&Factory{})` at import time. The CLI and server
main files import the protocol packages as blank imports:

```go
import _ "dhs/internal/protocol/acp1"
import _ "dhs/internal/protocol/acp2"
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

| Folder | Purpose | Linked from | ADR / memory |
|---|---|---|---|
| `internal/protocol/` | neutral consumer-plugin registry + `Protocol` interface + compliance event hooks + canonical validator | every consumer plugin (`init()` calls `protocol.Register`), `cmd/dhs/*` | ADR-0001, ADR-0015 |
| `internal/provider/` | neutral provider-plugin registry + `Provider` interface | every provider plugin, `cmd/dhs/cmd_producer.go` | ADR-0001 |
| `internal/transport/` | UDP / TCP / AN2 framer + `--capture` JSONL recorder | every connector that talks IP; `cmd_validate.go` | ADR-0020 (capture layout), ADR-0021 (JSONL contract) |
| `internal/wiretrace/` | wire-trace JSONL reader/writer | `cmd/dhs/cmd_validate.go`, every connector's `replay_test.go` | ADR-0021 |
| `internal/manifest/` | Card / Frame / Slot / Card / DM manifest loader + canonical export | every per-product DM lookup; `cmd_producer.go`; consumers when seeding tree from cache | ADR-0022 (Card data model) |
| `internal/dmlib/` | Device Model runtime resolver: looks up the right DM file for `<Model@SwRev>` and serves it through a typed accessor | `cmd_validate.go`, `cmd_producer.go`, every consumer plugin during walk | ADR-0022 |
| `internal/diff/` | semantic diff of two canonical trees (`Object` / `Frame` / `Slot`) used by the `diff` CLI verb | `cmd/dhs/cmd_diff.go` | — |
| `internal/export/` | JSON / YAML / CSV exporter + importer over `protocol.Object` | `cmd/dhs/cmd_export.go`, `cmd_import.go` | — |
| `internal/scenario/` | scenario-driven test runner — replays a JSONL scenario against a live or mock connector | every connector's `replay_test.go` under `-tags integration` | ADR-0021 |
| `internal/storage/` | file-backed persistence (per-OS datadir) for provider state, identity caches, salvo storage | provider state, identity orchestrator, salvo persistence | ADR-0020 (storage buckets) |
| `internal/metrics/` | neutral `ConnectorMetrics` surface (rx/tx/bytes/latency p50/p95/p99/errors/mem/cpu) every plugin exposes through `Metrics()` | every plugin's session, `cmd/dhs/cmd_metrics.go`, `--metrics-addr` flag | project_connector_metrics_v2 |
| `internal/identity/` | sanitiser helpers for vendor-supplied identity strings (vendor / product / serial / mac) | identity orchestrator across all connectors | project_identity_orchestrator |
| `internal/logging/` | `log/slog` structured logging primitives — OPNsense-style direction + source-path + severity tiers, Loki JSON output | every plugin and transport layer | project_logging |
| `internal/registry/` | cross-protocol session catalog — what device is online on which connector, last-seen, health | `cmd/dhs/common.go` keep-alive supervisor | project_keepalive_contract, project_session_health |

**Moving any of these requires a new ADR.** Doing so silently would
break every connector's import graph; they exist *because* the
per-connector code is intentionally thin and offloads cross-cutting
concerns here.

## Future-only or partial entries

| Folder | State |
|---|---|
| `internal/snell-rollcall/` | **future protocol**, gated on every current connector first satisfying the ADR-0025 six-deliverable bar. Today the directory only holds a gitignored local vendor SDK dump (1656 files of `.tpl` / `.mib` / `.zip` / `.exe` / `.doc`); no Go code, no registry entry. When work begins, per ADR-0001 the vendor materials move to `internal/snell-rollcall/assets/` alongside the scaffolded `consumer/` / `provider/` / `codec/` / `wireshark/` / `CLAUDE.md`. |
| `internal/cerebrum-nb/provider/` | **consumer-only by design at this stage** — only the consumer + codec + wireshark layers are shipped. The provider side is not absent by oversight; it is intentionally not in scope at the current stage per `internal/cerebrum-nb/CLAUDE.md`. |

---

Copyright (c) 2026 BY-SYSTEMS SRL — <https://www.by-systems.be>
