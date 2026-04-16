# Architecture

## Three layers

```
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
import _ "acp/internal/protocol/acp1"
import _ "acp/internal/protocol/acp2"
```

No runtime plugin loading. No external config. Adding a protocol means
adding one import line.

## Future direction

- Each protocol will eventually be its own repository/module
- The REST API (`acp-srv`) imports the library, does not access protocol
  code directly
- Documentation is split into small focused files, not one monolith

## Binaries

| Binary    | Purpose                              |
|-----------|--------------------------------------|
| `acp`     | CLI -- direct device I/O, no server  |
| `acp-srv` | HTTP REST + WebSocket API            |

Both share `internal/`. Neither imports the other.

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
