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

**ACP1 consumer — SHIPPED + canonical-aligned** on `main` as 9c5448e
(PR #33, closes #31). Adds `Canonicalize() → canonical.Export`,
compliance profile (`acp profile --protocol acp1`), 12-event catalog
with spec-page citations, `--capture <dir>` dir-mode writes
`tree.json`. Verified on Synapse Simulator at `10.6.239.113:2071`
(59 objects, classification strict). Consumer doc refreshed to match
Ember+ shape. Plus: `internal/protocol/compliance/profile.go`
extracted as a shared package for all plugins.

**ACP2 consumer — runtime feature complete, canonical alignment PENDING
(#32).** Pre-canonical plugin verified against Axon CONVERT Hybrid at
`10.41.40.195:2072` (VM only — not reachable from dev shell). 214
objects (slot 0), 44 k+ objects (slot 1). Full DFS walker, streaming,
enum u32 optionsMap, background walk for watch. User has VM access —
will run integration checks when the PR opens.

### In progress — ACP2 canonical alignment (#32, last child of #30)

Mirrors the ACP1 scope (shipped in PR #33). Each PR opens with
`Closes #<sub>` + `Advances #<umbrella>` in the PR body (not only
commit body — squash drops per-commit lines per
`memory/feedback_pr_issue_close.md`).

Scope:
- `internal/protocol/acp2/canonicalize.go` — map ACP2 objects (Node
  / Preset / Enum / Number / IPv4 / String across 20 pids) to
  canonical `Node` / `Parameter`, including preset-depth handling
  and the number_type vtype table
- `internal/protocol/acp2/compliance_events.go` — wire tolerance
  events catalog, ACP2 + AN2 spec page cited per label
- `Plugin.ComplianceProfile()` + `acp profile --protocol acp2`
- `--capture <dir>` dir-mode writes `tree.json`
- `docs/protocols/acp2/consumer.md` refreshed to Ember+/ACP1 shape
- Unit tests (synthetic BER + AN2 frames) — offline, CI-friendly
- User runs VM integration checks (`10.41.40.195:2072`) before merge

### Shipped — ACP1 canonical alignment (PR #33 on main)

- `internal/protocol/acp1/canonicalize.go` — SlotTree → canonical.Export
- `internal/protocol/acp1/compliance_events.go` — 12-event catalog
- Walker.SetProfile + transport/object error events on MTYPE=3
- `acp walk --protocol acp1 --capture <dir>` writes `tree.json`
- `acp profile --protocol acp1` returns classification + counters
- Wire-verified on Synapse Simulator 10.6.239.113:2071
- Plus shared `internal/protocol/compliance/profile.go` extracted

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
- Capture pipeline `--capture <dir>` → `raw.<transport>.jsonl`
  (`raw.acp1` / `raw.an2` / `raw.s101` per protocol, issue #41) +
  `tree.json` (all 3) + `glow.json` (Ember+ only), three-file mode
  auto-detected
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
- **CSV lossless round-trip (#38)** — `oid` + `path` + `id` + `label` columns so duplicate labels (Ember+ `gain` per channel, ACP2 `Present` per PSU) can be re-addressed by the importer. Contract: `convert json→csv` then `import csv --dry-run` returns `failed 0` on unchanged device.
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
extract, diff, convert, diag, matrix, invoke, list-protocols, help.

**Selective import (#45)**: `acp import --id N` / `--path P` — mutually
exclusive filter flags; both repeatable. No `--label` flag (labels
collide thousands of times across sub-trees in Ember+ and ACP2).

**DM extract (#47)**: `acp extract <host> --manufacturer M --product P
--direction D --version V --out DIR` produces the three-file product
triple (`meta.json` + `wire.jsonl` + `tree.json`) directly into the
fixture layout. `meta.json` carries SHA-256 fingerprint over tree.json
+ `capture_tool` provenance {name, version, git_tag, git_commit}
stamped from `runtime/debug.BuildInfo` (+ `-X main.*` ldflags
overrides). Dirty worktree flags git_tag with `-dirty`.

**DM diff (#49)**: `acp diff <before-tree.json> <after-tree.json>` —
OID-matched semantic diff. Categories: Breaking / Changed / Added /
Removed. Two formats: `text` (terminal) and `changelog`
(Keep-a-Changelog markdown). `--into PATH` atomically prepends a new
version section into an existing CHANGELOG.md.

**Scenario harness (#51)**: `tests/scenarios/<proto>/<name>.json` files
describe replay scenarios (wire capture + expected compliance events +
expected error class/status). `go test ./tests/unit/scenario/` walks
the tree and runs each as a sub-test. ACP2 runner implemented; ACP1 +
Ember+ stubs pending fixtures.

**CLI global flags**: `--protocol acp1|acp2|emberplus` `--port N`
`--transport udp|tcp` `--timeout DUR` `--log-level LEVEL` `--verbose`
`--capture FILE`.

**Pending**:
- `acp-srv` REST + WebSocket API (REST endpoints needed by future NetBox plugin — discover, snapshot, restore, diff, status, events)
- Provider mode (Part B — Ember+ provider first, then ACP2/ACP1/REST/WS)
- Browse command (search DM + live values)
- Ember+ port 9000 decoder edge cases

**Wireshark dissector family** — install guide in [docs/wireshark.md](docs/wireshark.md). Three Lua dissectors under `assets/*/dissector_*.lua` auto-load into Wireshark's personal plugins directory. Each covers the full wire format of its protocol:

| Dissector | Status | Notes |
|---|---|---|
| ACP1 | ✅ on main | UDP/TCP direct, all 11 object types + MDATA decode |
| ACP2 | ✅ on main | AN2 framing, all 20 PIDs with 4-byte alignment |
| Ember+ | ✅ on main | S101 + CRC + full Glow BER walker + multi-packet reassembly + Info-column content summary (Matrix/Function/Parameter/Stream/InvocationResult) + TCP 9000/9090/9092 auto + any-port heuristic |

**Per-type fixture library** (`tests/fixtures/protocol_types/<proto>/<type>/`): one slimmed capture + frozen `tshark -V` tree + README per wire element type. Generator: `scripts/fixturize.sh <src.pcapng> <dst-dir> <frame-list>` or `make fixtures-emberplus`. Smoke-verified in CI by `tests/unit/fixture_parity/*_test.go` — the test asserts fixture integrity (pcap/tree/README present, expected APP tags in tree) without requiring tshark at runtime.

| Protocol | #types shipped | Gap (capture needed) |
|---|---|---|
| Ember+ | 14 | StreamDescription (APP 12), QualifiedFunction (20), TupleItemDescription (21), Template (24), QualifiedTemplate (25) — TinyEmber+ / TinyEmberPlusRouter don't publish these. Re-capture from a Lawo / DHD / Riedel provider. |
| ACP1 | — | queued: per-type fixture pass against Synapse emulator 10.6.239.113 |
| ACP2 | — | queued: per-type fixture pass against VM 10.41.40.195 |

**Dissector enhancement backlog**:
- #57 — ACP1 Info column watch-style summary + full `slot.group.id` path
- #58 — ACP2 Info column watch-style summary + full `slot.obj_id` path
- #59 — Ember+ watch-parity (dotted identifier, matrix labels, value diff) — needs per-conversation state
- #60 — ✅ Ember+ per-type fixtures shipped (14 types). Layout: `tests/fixtures/protocol_types/emberplus/<type>/{capture.pcapng,tshark.tree,README.md}`. Follow-up issue tracks the 5 missing Glow types.

**Tshark self-verification loop**: developer can iterate on Ember+ autonomously against loopback TinyEmber+ (ports 9000/9092) without user-in-the-loop. ACP1 (emulator 10.6.239.113) + ACP2 (VM 10.41.40.195) require user-side execution for captures.

**Product fixture library** (`tests/fixtures/products/<manufacturer>/<product>/<protocol>/<direction>/<version>/`): generated by `acp extract`, diffed by `acp diff`. Per-segment CHANGELOG.md auto-maintained. Full spec in [docs/fixtures-products.md](docs/fixtures-products.md).

**Fabric vision** ([docs/VISION.md](docs/VISION.md)): long-term architecture — 8-block broadcast facility control plane (Core + inbound/outbound device connectors + control connectors + Catalog + Templates + Instances + Scheduler + Alarm engine), live schema mutations, PTP time, NetBox as Catalog backend, firmware upgrade lifecycle (pre-snapshot → re-discover → restore). Not a spec; a reference so cross-cutting decisions aren't re-derived.

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
