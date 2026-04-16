# acp

Go toolset to discover, connect, monitor, and control Axon devices that
speak the ACP protocol family (ACP1, ACP2, and future protocols).

Two binaries share one internal library:

| Binary      | Purpose                                      |
|-------------|----------------------------------------------|
| `acp`       | CLI — direct device I/O, no server           |
| `acp-srv`   | HTTP REST + WebSocket API for `acp-ui`       |

`acp-ui` is a separate React 19 repo; it consumes `acp-srv` only.

---

## Status

Early implementation. See [CLAUDE.md](CLAUDE.md) for the full protocol
reference and architecture. The spec documents live in [assets/](assets/)
(PDFs + Wireshark dissectors).

| Component                    | Status        |
|------------------------------|---------------|
| Protocol core + registry     | landed        |
| UDP transport (SO_REUSEADDR) | landed        |
| TCP direct transport         | landed        |
| ACP1 codec (all 11 types)    | landed        |
| ACP1 walker + cache (LRU+TTL)| landed        |
| ACP1 announcement listener   | landed        |
| ACP1 plugin (Protocol iface) | landed        |
| CLI: info / walk / walk --all| landed        |
| CLI: get / set / watch       | landed        |
| CLI: export (json/yaml/csv)  | landed        |
| CLI: import (json)           | landed        |
| CLI: discover (passive+active)| landed       |
| CLI: list-protocols / help   | landed        |
| ACP1 AN2 transport           | not started   |
| ACP2                         | not started   |
| `acp-srv` HTTP/WS API        | not started   |
| Persistence (devices.yaml)   | not started   |

Out of scope for v1: AN2 transport (ACP1 AN2 and ACP2), ACP1 methods 6–9,
file-object firmware reprogramming, authentication, multi-user sessions.
See `agents.md` for the full exclusion list.

---

## Protocols

| Protocol | Transport                       | Port        | Status         |
|----------|---------------------------------|-------------|----------------|
| ACP1     | UDP direct                      | 2071        | in progress    |
| ACP1     | TCP direct / AN2 TCP            | 2071 / 2072 | not started    |
| ACP2     | AN2 TCP                         | 2072        | not started    |

---

## Getting started

- **Developers**: read [runbook.md](runbook.md) — devcontainer setup, build,
  test, integration test, cross-compile, emulator workflow.
- **Agents (Claude, Copilot, etc.)**: read [CLAUDE.md](CLAUDE.md) first,
  then [agents.md](agents.md) for task patterns.
- **Protocol deep-dive**: read the PDFs in [assets/](assets/). Spec is
  authoritative; the C# reference driver (external) is secondary.

---

## Repository layout

```
cmd/
  acp/                  CLI entrypoint
  acp-srv/              HTTP + WebSocket server entrypoint
internal/
  protocol/             IProtocol interface, registry, shared types
    acp1/               ACP1 plugin (UDP / TCP direct / AN2)
    acp2/               ACP2 plugin (AN2 only)
    _template/          Starting point for new protocols
  transport/            UDP / TCP / AN2 framer primitives
  device/               Protocol-agnostic Device model + file-backed registry
  validator/            Value-vs-object constraint checks
  export/               JSON / CSV / YAML export + import
  storage/              OS-aware config + devices files
  logging/              slog handler, Loki-format JSON
api/
  server.go             net/http router
  handlers/             REST handlers
  ws/                   WebSocket hub + protocol-announce bridge
  openapi.yaml          OpenAPI 3.1 source of truth
tests/
  unit/                 Table-driven byte-exact codec tests
  integration/          Real-device tests (//go:build integration)
assets/                 Protocol spec PDFs + Wireshark dissectors
.devcontainer/          VS Code Dev Container config
```

---

## Documentation

| File                                                        | Audience        | Purpose                                       |
|-------------------------------------------------------------|-----------------|-----------------------------------------------|
| [README.md](README.md)                                      | everyone        | What this project is                          |
| [runbook.md](runbook.md)                                    | developers      | How to build, test, run, release              |
| [CLAUDE.md](CLAUDE.md)                                      | AI agents, devs | Protocol reference + architecture rules       |
| [agents.md](agents.md)                                      | AI agents       | Shared cross-repo instructions                |
| [LICENSE.md](LICENSE.md)                                     | everyone        | License terms                                 |
| [COPYRIGHT](COPYRIGHT)                                      | everyone        | Copyright notice                              |
| [LICENSES-THIRD-PARTY.md](LICENSES-THIRD-PARTY.md)         | everyone        | Third-party dependency licenses               |
| [CONTRIBUTING.md](CONTRIBUTING.md)                          | contributors    | Contribution guidelines                       |
| [CHANGELOG.md](CHANGELOG.md)                               | everyone        | Version history                               |
| [SECURITY.md](SECURITY.md)                                  | everyone        | Security policy + vulnerability reporting     |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)               | developers      | Three-layer architecture overview             |
| [docs/protocols/acp1/README.md](docs/protocols/acp1/README.md) | developers  | ACP1 protocol runbook                         |
| [docs/protocols/acp2/README.md](docs/protocols/acp2/README.md) | developers  | ACP2 protocol placeholder                     |
| [docs/references/README.md](docs/references/README.md)     | developers      | External protocol references                  |
| [docs/deployment/README.md](docs/deployment/README.md)     | ops             | Cross-compile + firewall rules                |
| [docs/examples/](docs/examples/)                            | developers      | Example export files (JSON, YAML, CSV)        |

---

## License

Copyright (c) 2026 BY-SYSTEMS SRL — [www.by-systems.be](https://www.by-systems.be)

All rights reserved. See [LICENSE.md](LICENSE.md) and [COPYRIGHT](COPYRIGHT).
