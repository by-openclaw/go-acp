# agents.md — ACP Project

Shared instructions for all AI agents working on any part of this project.
Read this alongside the `CLAUDE.md` for the specific repo you are in.

---

## Repositories

```
acp/       Go — core library, CLI binary (acp), API server (acp-srv)
acp-ui/    React 19 — optional browser frontend for acp-srv
```

One integration point: `acp/api/openapi.yaml` defines the REST + WebSocket contract.
`acp-ui` is generated from that spec and has zero knowledge of Go or protocol internals.

---

## Protocol Quick Reference

### ACP1

```
Port (direct UDP/TCP)   2071
Port (via AN2)          2072 (AN2 proto=1)
Transport               UDP / TCP direct / AN2 TCP
Header                  10 bytes (UDP/AN2) or 14 bytes (TCP direct with MLEN)
MtId                    u32 big-endian, 0=broadcast, never reuse on retransmit
Object address          ObjGroup(u8) + ObjId(u8)
Object model            Flat groups: root/identity/control/status/alarm/file/frame
Methods                 getValue(0)…getObject(5), getPresetIdx(6)…setPrp(9)
Announcements           MTID=0, MTYPE=0
Error replies           MTYPE=3, MCODE<16=transport, MCODE>=16=object
Strings                 ASCII, null-terminated, max 16 (label) / 4 (unit)
All multi-byte          big-endian MSB first
```

### ACP2

```
Port                    2072 (AN2 proto=2, TCP only)
Transport               TCP via AN2 exclusively
AN2 header              8 bytes: magic(0xC635) proto slot mtid type dlen
ACP2 header             4 bytes: type mtid func/stat pid/pad
Object address          obj-id(u32) global flat namespace
Object model            Tree: node objects have children (pid 14)
Functions               get_version(0) get_object(1) get_property(2) set_property(3)
Announces               type=2, mtid=0  ← "announce" NOT "event"
Announce delay          pid=4          ← "announce_delay" NOT "event_delay"
Error replies           type=3, stat 0-5
Property encoding       pid(u8) data(u8) plen(u16) value[varies] padding-to-4
Preset depth            pids 8-11 repeat once per preset idx
idx=0                   ACTIVE INDEX — not "first slot"
Strings                 UTF-8, null-terminated
All multi-byte          big-endian MSB first
```

### AN2 Transport

```
Magic         0xC635 — validate every frame
proto=0       AN2 internal (version, slot info, slot events)
proto=1       ACP1 payload
proto=2       ACP2 payload
proto=3       ACMP (not implemented)
AN2 mtid      0 for data/events, 1-255 for AN2 req/reply correlation
ACP2 mtid     SEPARATE — 0=announce, 1-255=ACP2 req/reply
EnableProtocolEvents([2]) MUST be called before ACP2 announces arrive
```

---

## Protocol Reference Documents

```
acp/assets/
  acp1/
    AXON-ACP_v1_4.pdf        ACP v1 authoritative spec
    dissector_acpv1.lua       Wireshark ACP1 dissector (byte-exact)
  acp2/
    acp2_protocol.pdf         ACP v2 authoritative spec
    an2_protocol.pdf          AN2 transport authoritative spec
    dissector_acp2.lua        Wireshark ACP2 dissector (byte-exact)
  emberplus/
    Ember+ Documentation.pdf  Ember+ v2.50 full spec (Lawo GmbH)
    Ember+ Formulas.pdf       Ember+ formula expressions
```

**Rule**: before modifying any codec, read the relevant spec section.
When spec and C# reference disagree, the spec wins.

---

## Task Patterns

### Add a new ACP1 object type

1. Add constants in `internal/protocol/acp1/types.go`
2. Add decode case in `internal/protocol/acp1/property.go`
3. Add encode case in `internal/protocol/acp1/message.go`
4. Add table-driven test with expected bytes in `tests/unit/acp1/`
5. Verify against Wireshark capture if possible

### Add a new ACP2 property (new pid)

1. Add pid constant in `internal/protocol/acp2/types.go`
2. Add encode/decode case in `internal/protocol/acp2/property_codec.go`
3. Check alignment: plen must be correct, padding calculated as `(4-(plen%4))%4`
4. Add table-driven test in `tests/unit/acp2/`

### Add a new CLI command

1. Add file in `cmd/acp/commands/{name}.go`
2. Register command in `cmd/acp/main.go`
3. Use `registry.Get(protocolName)` to get `Protocol` — never hardcode
4. Update `README.md` command reference

### Add a new API endpoint

1. Add handler in `api/handlers/{domain}.go`
2. Register route in `api/server.go`
3. Update `api/openapi.yaml`
4. In `acp-ui`: run `npm run generate:types`

### Add a new protocol

1. Copy `internal/protocol/_template/` → `internal/protocol/{name}/`
2. Implement `Protocol` interface
3. `func init() { protocol.Register(&Factory{}) }`
4. `import _` in both `cmd/` main files
5. Tests in `tests/unit/{name}/`
6. Nothing else changes in CLI, API, or UI

### Fix a wire format bug

1. Write a failing test first with exact expected bytes from the spec
2. Fix the codec
3. Verify test passes
4. Run `go test ./internal/protocol/...`

---

## Testing Rules

```
Unit tests (always run, no device needed):
  go test ./internal/...
  MockTransport injected — never real sockets
  Table-driven, expected bytes from spec documents

Integration tests (real device or emulator):
  go test -tags integration ./tests/integration/...
  ACP1_TEST_HOST=192.168.1.5  (ACP1 emulator)
  ACP2_TEST_HOST=192.168.1.8  (ACP2 device)
  Skip if env var not set

Protocol captures:
  Use dissector_acpv1.lua / dissector_acp2.lua in Wireshark
  Verify byte sequences match test expectations
  Methods 6-9 (ACP1): capture on emulator before implementing
```

---

## File Write Rules

```
config.yaml         on config change only
devices.yaml        on add/remove device only
device.yaml         on first connect + reconnect
slot_{n}.yaml       after every successful walk
property values     NEVER to disk
log entries         NEVER to disk
```

---

## Critical Invariants — Never Violate

```
1. AN2 magic=0xC635 must be validated on every received frame
2. ACP2 mtid pool: 1-255. Release in defer. Never reuse while in-flight.
3. ACP1 MTID: never change during retransmit. Increment for new requests only.
4. idx=0 in ACP2 = ACTIVE INDEX. Never treat as "first slot index".
5. AN2 EnableProtocolEvents MUST be called after every (re)connect.
6. ACP2 pid=4 = "announce_delay". Never name it "event_delay".
7. ACP2 type=2 = "announce". Never call it "event".
8. ACP1 strings: ASCII. ACP2 strings: UTF-8. Never swap.
9. All multi-byte values: big-endian in both protocols.
10. IProtocol is the only interface the rest of the system sees.
    Never import acp1/ or acp2/ packages outside their init() and cmd/.
```

---

## Implementation status (as of 2026-04-18)

**ACP1 — feature complete.** Verified against Synapse Simulator at
`10.6.239.113`. All 11 object types, LRU+TTL cache, announcements,
export/import round-trip.

**ACP2 — feature complete.** Verified against Axon CONVERT Hybrid at
`10.41.40.195`. 214 objects (slot 0), 44k+ objects (slot 1). Full DFS
walker, streaming, enum u32 optionsMap, background walk for watch.

**Ember+ — consumer working.** BER codec + S101 framing + Glow DTD.
Full tree walk on TinyEmber+ Router (51 objects, port 9092).
QualifiedNode spec-compliant. Set/subscribe/matrix/function coded,
not wire-tested yet. Port 9000 (DHD) needs debugging.

**Cross-protocol features**:
- Hierarchical export (JSON/YAML/CSV) — same tree format for all protocols
- --path subtree filter + --filter text search
- File-backed tree store (devices/{ip}/slot_{n}.json)
- Disk cache with labels/units for instant watch startup
- [cache]/[live] source tag in watch output
- Pre-commit hook (go vet + golangci-lint)
- --log-level trace/debug/info/warn/error/critical
- Enum map lookup O(1) for both ACP1 and ACP2
- Replay unit tests from real device captures
- Protocol-aware cache (prevents cross-protocol collisions)

**CLI commands**: info, walk, get, set, watch, discover, export, import,
diag, list-protocols, help.

**CLI global flags**: `--protocol acp1|acp2|emberplus` `--port N`
`--transport udp|tcp` `--timeout DUR` `--log-level LEVEL` `--verbose`
`--capture FILE`.

**Pending**:
- Ember+ set/subscribe/matrix/function wire testing
- Ember+ port 9000 (DHD provider) debugging
- `acp-srv` REST + WebSocket API
- Data model library (card_name + fw_version key)
- Browse command (search DM + live values)
- Provider mode (REST/WS, Ember+)

## Out of Scope — v1

```
ACP1 methods 6-9    (getPresetIdx, setPresetIdx, getPrp, setPrp)
ACMP protocol       (AN2 proto=3)
Authentication / TLS
Historical data / retention
Multi-user sessions
ACP1 File objects   (firmware reprogramming)
Apple notarization
ARM Linux packages
```

---

## acp-ui Specific Rules

```
Never call device protocols directly — only through acp-srv REST/WS
Never manually edit src/types/api.ts — regenerate from openapi.json
Never hardcode protocol names — fetch from GET /api/protocols
Never poll REST for live values — use WebSocket announces
TypeScript strict mode everywhere
useOptimistic for all SET operations with rollback on error
```
