# Ember+ — Scope of Work

Spec: **Ember+ Protocol Specification v2.50 rev.15 (2017-11-09)**, Lawo GmbH.
Source PDF: [assets/emberplus/Ember+ Documentation.pdf](../../assets/emberplus/Ember+%20Documentation.pdf).
Authoritative type definitions: **Glow DTD ASN.1 Notation (p.83–93)**.
Reference implementation studied: `assets/smh/` (consumer + provider TypeScript).

All Go identifiers mirror Ember+ spec names exactly. No third-party library terms.
Paths use `.` separator (e.g. `router.oneToN.matrix`).

---

## Spec page index

| Topic | Section | Page |
|---|---|---|
| Introduction (three layers) | Introduction | 9 |
| BER basics, types | EmBER | 11–15 |
| Glow: tag format + application-defined tags | Glow specification | 16–18 |
| Glow-specific properties (access, format, streamIdentifier, streamDescriptor, enumMap, …) | Glow specific properties | 23–30 |
| Commands (GetDirectory 32, Subscribe 30, Unsubscribe 31, Invoke 33) | Application defined commands | 31–32 |
| Matrix extensions | Ember+ 1.1 | 33–46 |
| Function extensions (Tuple, Invocation, InvocationResult) | Ember+ 1.2 | 47–50 |
| Schema extensions | Ember+ 1.3 | 51–53 |
| Template extensions | Ember+ 1.4 | 54–58 |
| Keep-Alive mechanism | Behaviour rules | 74 |
| Notifications (value change announce) | Behaviour rules | 74–75 |
| S101 framing — escaping (variant 1) | Message Framing | 78 |
| S101 framing — non-escaping (variant 2) | Message Framing | 79–80 |
| S101 Messages (types, commands) | S101 Messages | 81–82 |
| Glow DTD ASN.1 Notation (authoritative) | Glow DTD | 83–93 |
| CRC-16 lookup table | Appendix | 94 |

## Application-defined tags (from DTD, p.83–93, verified)

| Type | Tag | Page |
|---|---|---|
| RootElementCollection | APPLICATION[0] | 93 |
| Parameter | APPLICATION[1] | 85 |
| Command | APPLICATION[2] | 86 |
| Node | APPLICATION[3] | 87 |
| ElementCollection | APPLICATION[4] | 92 |
| StreamEntry | APPLICATION[5] | 93 |
| StreamCollection | APPLICATION[6] | 93 |
| StringIntegerPair | APPLICATION[7] | 86 |
| StringIntegerCollection | APPLICATION[8] | 86 |
| QualifiedParameter | APPLICATION[9] | 85 |
| QualifiedNode | APPLICATION[10] | 87 |
| StreamDescription | APPLICATION[12] | 86 |
| Matrix | APPLICATION[13] | 88 |
| Target | APPLICATION[14] | 89 |
| Source | APPLICATION[15] | 89 |
| Connection | APPLICATION[16] | 89 |
| QualifiedMatrix | APPLICATION[17] | 89 |
| Label | APPLICATION[18] | 89 |
| Function | APPLICATION[19] | 91 |
| QualifiedFunction | APPLICATION[20] | 91 |
| TupleItemDescription | APPLICATION[21] | 91 |
| Invocation | APPLICATION[22] | 91 |
| InvocationResult | APPLICATION[23] | 92 |
| Template | APPLICATION[24] | 84 |
| QualifiedTemplate | APPLICATION[25] | 84 |

## Command numbers (p.31, DTD CommandType)

| Action | Number | Extra field |
|---|---|---|
| Subscribe | 30 | — |
| Unsubscribe | 31 | — |
| GetDirectory | 32 | dirFieldMask (context 1) |
| Invoke | 33 | invocation (context 2) |

---

## Phase 0 — Baseline (shipped on `main`)

| Area | Status |
|---|---|
| S101 framing (variant 1) + CRC-16 + byte stuffing | ✓ |
| S101 multi-frame reassembly (First/Last/Single) | ✓ |
| BER reader/writer | ✓ |
| Glow tag constants | ✓ partial (needs correction vs DTD) |
| Numeric path ↔ identifier map | ✓ |
| GetDirectory (walk) | ✓ 4494 objs on TinyEmber+ |
| GetValue / SetValue (path) | ✓ |
| Subscribe / Unsubscribe (path + wildcard) | ✓ |
| Matrix connect (absolute/connect/disconnect) | ✓ |
| Function invoke + InvocationResult decode | ✓ |
| CLI: walk, get, set, watch, matrix, invoke | ✓ |

Port 9000 provider: connects + keep-alive OK, GetDirectory reply missing. Tracked separately.

---

# PART A — Consumer (priority)

## A1. Glow decoder — spec-complete rebuild

Audit every tag and CTX against the DTD. Apply SET unwrap on every `contents` container. BER TLVs self-delimit — advance by length only (no padding).

### NodeContents — SET, p.87

| CTX | Field | Type | Notes |
|---|---|---|---|
| 0 | identifier | EmberString | |
| 1 | description | EmberString | |
| 2 | **isRoot** | BOOLEAN | ← spec CTX 2, not isOnline |
| 3 | isOnline | BOOLEAN | default true |
| 4 | schemaIdentifiers | EmberString | newline-separated |
| 5 | templateReference | RelOID | |

Node wrapper (APPLICATION[13] — wait, [3]): `{ 0:number, 1:contents, 2:children }`.

### ParameterContents — SET, p.85

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | value | Value (CHOICE int/real/string/bool/octets/null) |
| 3 | minimum | MinMax |
| 4 | maximum | MinMax |
| 5 | access | ParameterAccess (0 none, 1 read, 2 write, 3 readWrite) |
| 6 | format | EmberString |
| 7 | enumeration | EmberString (legacy newline list) |
| 8 | factor | Integer32 |
| 9 | isOnline | BOOLEAN |
| 10 | formula | EmberString (provider\|consumer split) |
| 11 | step | Integer32 |
| 12 | default | Value |
| 13 | type | ParameterType (null/integer/real/string/boolean/trigger/enum/octets) |
| 14 | streamIdentifier | Integer32 |
| 15 | enumMap | StringIntegerCollection |
| 16 | streamDescriptor | StreamDescription |
| 17 | schemaIdentifiers | EmberString |
| 18 | templateReference | RelOID |

### MatrixContents — SET, p.88

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | type | MatrixType (0 oneToN, 1 oneToOne, 2 nToN) |
| 3 | addressingMode | MatrixAddressingMode (0 linear, 1 nonLinear) |
| 4 | targetCount | Integer32 |
| 5 | sourceCount | Integer32 |
| 6 | maximumTotalConnects | Integer32 (nToN) |
| 7 | maximumConnectsPerTarget | Integer32 (nToN) |
| 8 | parametersLocation | CHOICE basePath RelOID / inline Integer32 |
| 9 | gainParameterNumber | Integer32 |
| 10 | labels | LabelCollection (SEQ OF CTX[0] Label) |
| 11 | schemaIdentifiers | EmberString |
| 12 | templateReference | RelOID |

Matrix wrapper APPLICATION[13]: `{ 0:number, 1:contents, 2:children, 3:targets, 4:sources, 5:connections }`.

### Connection — APPLICATION[16], p.89

| CTX | Field | Type |
|---|---|---|
| 0 | target | Integer32 |
| 1 | sources | PackedNumbers (RelOID of source numbers) |
| 2 | operation | ConnectionOperation (0 absolute, 1 connect, 2 disconnect) |
| 3 | disposition | ConnectionDisposition (0 tally, 1 modified, 2 pending, 3 locked) |

### Label — APPLICATION[18], p.89

| CTX | Field | Type |
|---|---|---|
| 0 | basePath | RelOID |
| 1 | description | EmberString |

### FunctionContents — SET, p.91

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | arguments | TupleDescription (SEQ OF CTX[0] TupleItemDescription) |
| 3 | result | TupleDescription |
| 4 | templateReference | RelOID |

TupleItemDescription APPLICATION[21]: `{ 0:type ParameterType, 1:name EmberString }`.

### Invocation — APPLICATION[22], p.91

| CTX | Field | Type |
|---|---|---|
| 0 | invocationId | Integer32 |
| 1 | arguments | Tuple (SEQ OF CTX[0] Value) |

### InvocationResult — APPLICATION[23], p.92

| CTX | Field | Type | Notes |
|---|---|---|---|
| 0 | invocationId | Integer32 | |
| 1 | success | BOOLEAN | **default true when omitted** |
| 2 | result | Tuple | SEQ OF CTX[0] Value |

### StreamEntry — APPLICATION[5], p.93

| CTX | Field | Type |
|---|---|---|
| 0 | streamIdentifier | Integer32 |
| 1 | streamValue | Value |

### StreamDescription — APPLICATION[12], p.86

| CTX | Field | Type |
|---|---|---|
| 0 | format | StreamFormat (unsignedInt8..IEEE754 float/double BE+LE) |
| 1 | offset | Integer32 |

### Template / QualifiedTemplate — APPLICATION[24/25], p.84

Template SET: `{ 0:number, 1:element TemplateElement, 2:description }`.
QualifiedTemplate SET: `{ 0:path RelOID, 1:element, 2:description }`.
TemplateElement CHOICE over Parameter/Node/Matrix/Function.

### Collections

- RootElementCollection APPLICATION[0] (p.93) — top-level reply container.
- ElementCollection APPLICATION[4] — SEQ OF CTX[0] Element (Element CHOICE of Parameter/Node/Command/Matrix/Function/Template).
- StreamCollection APPLICATION[6] — SEQ OF CTX[0] StreamEntry.

### Command — APPLICATION[2], p.86

`{ 0:number CommandType, options CHOICE { [1] dirFieldMask FieldFlags, [2] invocation Invocation } }`.

---

## A2. Plugin tree model (spec-accurate)

| Responsibility | Implementation |
|---|---|
| Element store | `map[pathKey]*Entry`, O(1) lookup |
| Canonical path key | numeric RelOID joined by `.` |
| String-path → numeric | walker fills `numPath` map per Node/Parameter/Matrix/Function |
| Lookup order | numeric path → string path → identifier label → numeric ID |
| Startup | tree empty ⇒ auto-walk on first GetValue/SetValue/Subscribe |
| Value freshness | `stale` (disk) / `live` (confirmed) / `updated` (recent announce) |

Per-element metadata (all spec fields above) held in RAM. Stale cache respects CLAUDE.md rules.

---

## A3. Subscribe + announces (p.74–75, p.31)

| Parameter kind | Mechanism | Spec ref |
|---|---|---|
| Regular (no streamIdentifier) | Implicit: provider announces on change after GetDirectory | p.74 Notifications |
| Streamed (streamIdentifier set) | Explicit `Command 30` required; dispatch via StreamCollection | p.30 StreamIdentifier; p.31 Subscribe |
| Wildcard watch-all | Session-local `"*"` callback; every processed parameter notified | our ext |

Unsubscribe on session close: iterate registered subscriptions, send `Command 31` per path.

---

## A4. Matrix — full feature set (p.33–46)

### Raw wire state (decoded verbatim)

`connections: map[target]*Connection` mirrors wire. Fields: target, sources[], operation, disposition.

### parametersLocation resolution (p.38)

- CHOICE basePath RelOID = absolute path to params node.
- CHOICE inline Integer32 = subidentifier below matrix's own node.
- Addressing under the params node (p.38):
  - `targets.<n>` → per-target params
  - `sources.<n>` → per-source params
  - `connections.<t>.<s>` → per-connection params (gain lives here)

### canConnect validation (pre-flight on client)

| Type | Rule | Spec ref |
|---|---|---|
| oneToN | 1 source per target; connect replaces | p.33 |
| oneToOne | 1 source per target AND source used once globally | p.33 |
| nToN | `len(sources[t]) <= maxConnectsPerTarget`; `sum <= maxTotalConnects` | p.33–34 |
| any | reject if target's disposition == `locked` | p.89 |

### MatrixConnection — our enhancement vs spec

Wire Connection stays spec-pure. We hold a derived record alongside for the UI:

| Field | Source | Note |
|---|---|---|
| target, sources[], operation, disposition | spec Connection | verbatim |
| `lastChangedUnixMs` | us | audit timestamp |
| `changeSource` (user/walk/announce) | us | attribution |
| `resolvedGainDb` | follow gainParameterNumber path, GET value | convenience |
| `labelSource`, `labelTarget` | resolve labels basePath | convenience |

These are derived — never serialized onto the wire.

---

## A5. Function invocation (p.47–50)

| Step | Detail |
|---|---|
| ID allocation | monotonic Integer32 per session; skip 0 |
| Encode args | Invocation APP[22] with Tuple (SEQ of CTX[0] Value) |
| Correlate | `map[invocationId]chan *InvocationResult`, 10s timeout |
| Decode result | APP[23]; `success` defaults **true** when field absent; `result` = Tuple |

---

## A6. Stream handling (p.22 + p.30 + p.86 + p.93)

| Task | Detail |
|---|---|
| Build index | on walk, if Parameter has streamIdentifier → add `map[streamIdentifier]path` |
| Explicit subscribe | for each streamed param: send `Command 30` |
| Receive | StreamCollection APP[6] at top level; dispatch each StreamEntry |
| StreamDescription | if present on param, slice payload at `offset` and decode per `format` endianness |
| Deliver | fire subscribed callbacks with decoded value |

---

## A7. Template support — read-only consumer (p.54–58)

| Task | Detail |
|---|---|
| Template APP[24] / QualifiedTemplate APP[25] | decode via TemplateElement CHOICE |
| templateReference RelOID on Node/Parameter/Matrix/Function | resolve lazily to template path |
| GetDirectory on template | normal walk; don't instantiate (consumer) |

---

## A8. CLI surface

| Command | Flags |
|---|---|
| `acp ember walk` | `--path a.b.c` |
| `acp ember get` | `--path a.b.c` |
| `acp ember set` | `--path a.b.c --value X` (typed from tree metadata) |
| `acp ember watch` | `--path a.b.c` or `*` |
| `acp ember matrix` | `--path a.b.c --target N --sources N,N --op absolute\|connect\|disconnect` |
| `acp ember invoke` | `--path a.b.c --args v1,v2,...` (types from FunctionContents.arguments) |
| `acp ember stream` | `--id N` (subscribe to a streamIdentifier) |

---

# PART B — Provider (after Part A is green)

## B1. Tree construction

| Source | Format |
|---|---|
| JSON file (primary) | spec-named keys |
| Go API | programmatic builder |

## B2. S101 server (variant 1 + variant 2)

Listener TCP :9000. Per-connection: keep-alive, fragmentation buffer, subscriber set.

## B3. Request handlers

| Command | Handler |
|---|---|
| 32 GetDirectory | walk, return ElementCollection of direct children |
| 30 Subscribe | register connection (streamed params; implicit for regular) |
| 31 Unsubscribe | deregister |
| 33 Invoke | dispatch to Go function, return InvocationResult |
| SetValue | resolve, validate, mutate, announce |
| Matrix SetConnection | resolve, canConnect, apply, announce tally |

## B4. Announce engine

| Trigger | Emits |
|---|---|
| SetValue applied | Parameter announce to subscribers |
| Matrix op applied | Connection announce, disposition = tally |
| Stream tick | StreamCollection frame |

## B5. Errors

Use Ember+ names. Transport = `S101SocketError`. Codec = `InvalidBERFormat`, `MissingElementNumber`, `MissingElementContents`, `UnknownElement`, `InvalidMatrixSignal`, `PathDiscoveryFailure`, `InvalidFunctionCall`, `UnsupportedValue`.

## B6. Embedded test provider

Small tree (router matrix + gain params + labels + one function) exercising consumer and provider end-to-end.

---

# Delivery order

1. A1 decoder rebuild (all types + SET unwrap + tag corrections)
2. A2 plugin tree model (path-first, metadata, freshness)
3. A3 subscribe (explicit for stream params)
4. A4 matrix full state + canConnect + enhancements
5. A5 function (regression test)
6. A6 stream handling
7. A7 template read-only
8. All of A integration-tested on TinyEmber+ 9092
9. Open PR
10. B1–B6 provider (next milestone)

Each step: `go build ./...` → unit tests → integration (emulator) → commit.

---

# Non-negotiables

- Spec-first. Dufour / smh consulted only after spec + capture.
- No partial types. All ParameterType / MatrixType / StreamFormat / ConnectionOperation / ConnectionDisposition values decoded before testing.
- Path separator `.` everywhere.
- No new runtime plugins. Compile-time registry only.
- Values never trusted from disk (stale until confirmed).
- Ember+ announce logs suppressed (direction-tagged per project_logging).
- Detailed docstrings on every exported Glow type + encoder/decoder func, each citing spec page and CTX tag.
