# Changelog

All notable changes documented here. Format follows [Keep a Changelog](https://keepachangelog.com/).

**Version source of truth: git tags.** The Makefile reads `git describe --tags`
and injects it into the binary via `-ldflags`. No hardcoded version strings
anywhere. Workflow:

```
1. Work on main branch
2. Update this file with the new version section
3. git tag -a vX.Y.Z -m "vX.Y.Z"
4. git push origin vX.Y.Z
5. make build-all / make package
```

---

## [Unreleased]

_Changes on main not yet tagged._

---

## [0.1.0] — 2026-04-16

Initial release. ACP1 protocol fully implemented.

### Added

- ACP1 plugin: full codec for all 11 object types (root, int, ipaddr, float, enum, string, frame, alarm, file, long, byte)
- ACP1 walker with LRU+TTL cache and live event updates
- ACP1 announcement listener (UDP broadcast, SO_REUSEADDR multi-instance)
- ACP1 typed value codec: encode/decode for read/write with step-based precision
- UDP direct transport (port 2071)
- TCP direct transport (ACP v1.4, port 2071)
- Protocol registry with compile-time plugin model
- CLI commands: `info`, `walk`, `walk --all`, `get`, `set`, `watch`, `discover`
- CLI commands: `export` (JSON, YAML, CSV), `import` (JSON, YAML, CSV)
- CLI commands: `list-protocols`, `help` (with per-command help pages)
- CLI flag: `--transport udp|tcp`
- Label-based addressing for all CLI operations
- Sub-group marker detection (both enum-space and leading-whitespace string conventions)
- Frame-status decoding with human-readable slot status symbols
- Cross-compile targets: Linux amd64/arm64, macOS amd64/arm64, Windows amd64
- Cross-platform verified: Windows 11 + Ubuntu 24
- Export/import lossless round-trip for all three formats (49/62/0 parity)

### Not yet implemented

- ACP1 AN2 transport
- ACP2 protocol
- REST API server (`acp-srv`)
- Persistence (`devices.yaml`)

---

Copyright (c) 2026 BY-SYSTEMS SRL — https://www.by-systems.be — MIT License
