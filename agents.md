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

## Implementation status (as of 2026-04-19)

### Shipped

**Ember+ consumer — SHIPPED** on `main` as 171f32a (PR #29). Full spec
v2.50 coverage: BER codec, S101 framing, Glow DTD, canonical export
with resolver (templates/labels/gain × pointer/inline/both), multi-level
matrix labels, watch field-diff, SetValue with write-confirm, session
dead-man + auto-reconnect, matrix spec invariants. Wire-verified on
TinyEmberPlus :9092 (4494 objects) and :9000 (~20 000 objects, DHD tree).
Issues #20–#28 closed.

**ACP1 consumer — runtime feature complete**, pre-canonical. Verified
against Synapse Simulator at `10.6.239.113:2071`. All 11 object types,
LRU+TTL cache, announcements, export/import round-trip. Canonical
alignment pending under #31.

**ACP2 consumer — runtime feature complete**, pre-canonical. Verified
against Axon CONVERT Hybrid at `10.41.40.195:2072` (VM only — not
reachable from dev shell). 214 objects (slot 0), 44 k+ objects (slot 1).
Full DFS walker, streaming, enum u32 optionsMap, background walk for
watch. Canonical alignment pending under #32.

### In progress — ACP1 + ACP2 canonical alignment (umbrella #30)

Propagate the Ember+ architecture to ACP1 + ACP2:
- `internal/protocol/<name>/canonicalize.go` — map objects to
  canonical `Node` / `Parameter`
- `internal/protocol/<name>/compliance.go` — wire tolerance events,
  spec-page cited
- Freshness model (live / updated / stale / cache)
- Cascade on disconnect (root `isOnline y→n` synthetic event)
- Auto-reconnect goroutine (pattern lifted from Ember+)
- Dir-mode capture: `--capture <dir>` → `tree.json`
- `acp profile <host> --protocol <name>` CLI
- Consumer doc refresh matching Ember+ shape

**Order:** ACP1 first (locally testable, #31), then ACP2 (VM-blocked, #32).
Each ships as its own PR with `Closes #<sub>` + `Advances #<umbrella>` in
the PR body (not only commit body — squash drops per-commit lines per
`memory/feedback_pr_issue_close.md`).

### Ember+ — what shipped in PR #29

Working:
- Canonical JSON export per `docs/protocols/schema.md` + `elements/*.md`
  via `internal/export/canonical/` Go structs + `WriteCanonicalJSON`
- Glow→canonical translator in-plugin (`Plugin.Canonicalize`) with
  resolver pass (`internal/protocol/emberplus/resolver.go`)
- CLI mode flags `--templates` / `--labels` / `--gain` (each
  `pointer|inline|both`, default `pointer`). Inline absorbs referenced
  subtrees into the referring element and removes them from the tree;
  both preserves both. Multi-level labels supported throughout
  (`targetLabels`/`sourceLabels` keyed by `labels[i].description`).
  Connection params keyed by composite `"target.source"`.
- Capture pipeline `--capture <dir>` → `raw.s101.jsonl` + `glow.json` +
  `tree.json`, three-file mode auto-detected
- Watch with field-diff `changes[]`, description, access, freshness
  (live/updated/stale), matrix crosspoint events
- Wildcard stream auto-subscribe (Command 30 on discovery, uses
  canonical numericPath for non-qualified providers)
- Shared `streamIdentifier` / CollectionAggregate support; collision
  without `streamDescriptor` fires `stream_id_collision_no_descriptor`
- Merge-on-announce: walked metadata preserved when announces carry
  only the changed field; last-known value preserved across
  description-only announces. Announce re-delivery of a parent Node
  with empty `children[]` no longer triggers spurious GetDirectory
  (processNode first-sighting guard, fixed 2026-04-19)
- SetValue with confirming-announce wait, timeout + coerce classification
  (`ErrWriteTimeout`, `ErrWriteCoerced`, `ErrWriteRejected`)
- Session dead-man timer (30s default); on disconnect: all entries
  `freshness=stale`, values preserved, synthetic root event
  `isOnline y→n + reason`; auto-reconnect goroutine (2s→30s backoff)
  re-walks + re-subscribes streams on return
- Matrix state machine with spec invariants:
  - oneToN/oneToOne disconnect pre-flight rejected (spec p.33)
  - nToN bounded by `maximumConnectsPerTarget` / `maximumTotalConnects`
  - Locked targets refuse writes
- Function invoke + InvocationResult decode
- Matrix-scoped compliance events: `matrix_label_basepath_unresolved`,
  `matrix_label_none`, `matrix_label_description_empty`,
  `matrix_label_level_mismatch`, `matrix_parameters_location_unresolved`,
  `template_reference_unresolved`, `labels_absorbed`, `gain_absorbed`,
  `template_absorbed`

**Ember+ known gaps** (accepted for now — not blockers):

1. Matrix-template / Function-template inflation pointer-only
   (no real provider in current captures ships these; fires
   `template_reference_unresolved`).
2. Gain resolver wire-verified on 9092 only (9000 and 9090 captures
   have no `parametersLocation`).
3. Replay tests (s101_replay, ber_roundtrip, glow_decode,
   export_shape, encoder_compliance) not implemented — doc-conformance
   + resolver tests cover the core shape-drift risk.
4. VM integration for PR #29 not run — branch did not touch ACP2;
   ACP1 exists in this codebase but was not the subject of the PR.

---

## Canonical schema — union across all protocols (locked 2026-04-18)

Ember+ templates are the richest per-type shape. ACP1 / ACP2 / Ember+ all
export into the same JSON shape; fields absent on the wire are emitted as
`null` (stable key set). smh reference: `assets/smh/emulator/ember-server/src/data-model-new.ts`.

### Per-type keys (always present)

```
common        number, identifier, path, oid, description, isOnline, access, children[]
Node          + templateReference, schemaIdentifiers
Parameter     + type, value, default, minimum, maximum, step, unit, format,
                factor, formula, enumeration, enumMap,
                streamIdentifier, streamDescriptor{format, offset},
                templateReference, schemaIdentifiers
Matrix        + type (oneToN|oneToOne|nToN), mode (linear|nonLinear),
                targetCount, sourceCount, maximumTotalConnects,
                maximumConnectsPerTarget, parametersLocation (OID string),
                gainParameterNumber,
                labels[{basePath, description}], targets[{number}],
                sources[{number}],
                connections[{target, sources[], operation, disposition, locked}],
                targetLabels{} / sourceLabels{}                  (inline mode)
                targetParams{} / sourceParams{} / connectionParams{}  (inline mode)
Function      + arguments[{name, type}], result[{name, type}]
Stream        inside Parameter only (streamIdentifier + streamDescriptor);
              StreamCollection merged back onto owning Parameters, no section
Template      top-level templates[] = [{number, oid, identifier, description,
              template: {full element shape}}]
```

### Mode flags (default `inline` each)

```
--templates=<inline|separate|both>
   inline   : templateReference resolved + fields inflated onto element,
              templates[] section dropped
   separate : templates[] kept, templateReference kept, no inflation
   both     : templates[] kept AND inflation applied

--labels=<inline|pointer|both>
   inline   : targetLabels{} / sourceLabels{} resolved (from basePath
              or in-matrix subtree), labels[] dropped
   pointer  : labels[{basePath, description}] only
   both     : both representations kept

--gain=<inline|pointer|both>
   inline   : targetParams / sourceParams / connectionParams resolved
              from parametersLocation
   pointer  : parametersLocation + gainParameterNumber only
   both     : both representations kept
```

All pointers emit as numeric OID strings (e.g. `"3.0.3000"`), never bare ints.

### enumMap — universal label↔id resolver

Every enum Parameter carries BOTH `enumeration` (LF-joined) AND
`enumMap` ([{key, value, masked?}]) across all protocols:
- ACP1 — enumMap derived from `item_list` (sequential, position = value)
- ACP2 — enumMap from pid 15 `options` (non-sequential capable)
- Ember+ — native enumMap if provided, else derived from enumeration
- Masked items (smh `~Label`) strip tilde, expose `masked: true`

### Compliance events

```
template_inlined, template_unresolved,
label_basepath_only, label_inline_only, label_duplicate, label_missing,
gain_pointer_only, gain_inline_only, gain_duplicate, gain_missing,
enum_double_source, enum_masked_item, enum_duplicate_label,
field_lossy_down, field_inferred, proto_cross_empty,
auto_pointer_downgrade,
multi_frame_reassembly, non_qualified_element,
invocation_success_default, connection_*_default,
contents_set_omitted, tuple_direct_ctx, element_collection_bare,
unknown_tag_skipped
```

### Capture pipeline

```
acp walk <host> --protocol emberplus --capture tests/fixtures/emberplus/<provider>/
   writes raw.s101.jsonl   (every tx/rx S101 frame, CRC + flags + DTD)
          glow.json         (decoded element tree, internal repr)
          tree.json         (canonical export, smh-aligned)

Replay tests under tests/unit/emberplus/:
   s101_replay       re-frame raw.s101.jsonl → same bytes
   ber_roundtrip     decode/encode BER → identical
   glow_decode       decode ber.hex → matches glow.json
   export_shape      glow.json → export == tree.json golden
   encoder_compliance  encode glow.json → BER matches wire capture
```

---

## Scope sequencing (locked 2026-04-18)

Order: **Ember+ consumer → Ember+ provider → bus / extra protocols**.
No next phase until previous ships with VM integration passing.

### Parked — TODO / SOW (do not start now)

| item | phase | notes |
|---|---|---|
| Ember+ provider | Part B | after consumer closed |
| Bus bridge (`acp-srv --bus=none\|nats\|es\|redis-stream`) | Part C | orchestrator-level; plugins stay bus-free |
| Probel SW-P-02 consumer+provider | later | canonical Matrix shape, no walkable tree |
| Probel SW-P-08 Plus consumer+provider | later | canonical Matrix with level multiplex |
| TSL UMD v3.1 / v5 consumer+provider | later | canonical Node with per-address tally |
| `internal/crossmap/` cross-protocol translator | later | needs ≥2 provider-capable plugins |
| Auto inline→pointer size-threshold downgrade | later | measure real captures first |
| ACP1 / ACP2 migration to canonical shape | later | after Ember+ consumer proves shape |

### Architecture lock — plugins stay bus-free

Plugins translate wire ↔ canonical element shapes only. Events bubble up
via `EventFunc`. `cmd/acp-srv` owns the bus bridge; `internal/protocol/**`
never imports NATS / ES / Kafka clients. Preserves CLI usability, unit
testability, pluggability.

**Cross-protocol features**:
- Hierarchical export (JSON/YAML/CSV) — same tree format for all protocols
- --path subtree filter (dot separator) + --filter text search
- File-backed tree store (devices/{ip}/slot_{n}.json)
- Disk cache with labels/units for instant watch startup
- [cache]/[live] source tag in watch output
- Pre-commit hook (go vet + golangci-lint)
- --log-level trace/debug/info/warn/error/critical
- Enum map lookup O(1) for both ACP1 and ACP2
- Replay unit tests from real device captures
- Protocol-aware cache (prevents cross-protocol collisions)
- Path-based addressing with dot separator (all protocols)

**CLI commands**: info, walk, get, set, watch, discover, export, import,
diag, matrix, invoke, list-protocols, help.

**CLI global flags**: `--protocol acp1|acp2|emberplus` `--port N`
`--transport udp|tcp` `--timeout DUR` `--log-level LEVEL` `--verbose`
`--capture FILE`.

**Pending**:
- Ember+ full spec compliance (decoder + plugin tree model + stream)
- Ember+ port 9000 debugging
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
