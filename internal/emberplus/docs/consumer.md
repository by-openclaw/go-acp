# Ember+ Connector

Consumer connector for the Ember+ protocol (Lawo Glow DTD over S101/TCP).

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [internal/emberplus/assets/Ember+ Documentation.pdf](../../../internal/emberplus/assets/Ember+%20Documentation.pdf) | Ember+ Protocol Specification v2.50 rev.15 (2017-11-09), Lawo GmbH |
| Formulas | [internal/emberplus/assets/Ember+ Formulas.pdf](../../../internal/emberplus/assets/Ember+%20Formulas.pdf) | Parameter formula syntax reference |
| Protocol reference | [CLAUDE.md](../../../CLAUDE.md) — section "Ember+" | Wire format, methods, object types |
| Source code | [internal/protocol/emberplus/](../../../internal/protocol/emberplus/) | Plugin implementation |
| Unit tests | [internal/protocol/emberplus/glow/glow_test.go](../../../internal/protocol/emberplus/glow/glow_test.go) | BER + element decode tests |
| Matrix tests | [internal/protocol/emberplus/matrix/state_test.go](../../../internal/protocol/emberplus/matrix/state_test.go) | canConnect rule coverage |

### Spec page index (for debugging — Ctrl+F in PDF)

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
| Notifications (value-change announce) | Behaviour rules | 74–75 |
| S101 framing — escaping (variant 1) | Message Framing | 78 |
| S101 framing — non-escaping (variant 2) | Message Framing | 79–80 |
| S101 Messages (types, commands) | S101 Messages | 81–82 |
| Glow DTD ASN.1 Notation (authoritative) | Glow DTD | 83–93 |
| CRC-16 lookup table | Appendix | 94 |

---

## Transport

S101 over TCP. One framing layer (variant 1 — escaping) is implemented;
variant 2 (non-escaping) is not needed by any tested provider.

| Field | Value | Notes |
|---|---|---|
| TCP port (typical) | 9000 | Some provider builds use 9092 (TinyEmberPlusRouter) |
| Framing | S101 variant 1 | BOF 0xFE, EOF 0xFF, byte-stuff ≥ 0xF8 with 0xFD prefix |
| CRC | CRC-CCITT16, polynomial 0x8408, init 0xFFFF, inverted | Validated on every frame |
| Keep-alive | Bidirectional | Each side sends Cmd 1 (req), the other replies Cmd 2 (resp); ~10s interval |
| Multi-frame | `FlagFirst` (0x80), `FlagLast` (0x40), `FlagSingle` (0xC0) | Payloads > ~1 KB are reassembled |

### Firewall rules

```
TCP 9000  outbound      (default Ember+ port)
TCP 9092  outbound      (TinyEmberPlus Router variant)
```

### CLI transport selection

```
acp walk 127.0.0.1 --protocol emberplus --port 9000
acp walk 127.0.0.1 --protocol emberplus --port 9092
```

`--slot` is optional for Ember+ (the plugin defaults to 0 — Ember+ has
no slot concept). Pass `--protocol emberplus` on every call; it is
**not** the default.

---

## Capabilities & Compliance Status

| Capability | Spec § | Status | Notes |
|---|---|---|---|
| BER codec (integer, real, boolean, UTF8, RelOID, SET, SEQUENCE, OctetString) | EmBER 11–15 | ✅ fully compliant | Table-driven tests in `glow/glow_test.go` |
| S101 framing variant 1 (escaping, CRC-CCITT16) | Framing 78 | ✅ fully compliant | Wire-verified on both test providers |
| S101 framing variant 2 (non-escaping) | Framing 79–80 | ⛔ not implemented | No tested provider uses it |
| Multi-frame reassembly (`FlagFirst / FlagLast`) | Framing 78–80 | ✅ fully compliant | Auto-handled in `session.readLoop` |
| Keep-alive (bidirectional Cmd 1 / Cmd 2) | Rules 74 | ✅ fully compliant | ~10 s interval, 30 s dead-man timer |
| GetDirectory — root + per-subtree | Cmd 31 | ✅ fully compliant | Qualified + non-qualified fallback |
| Subscribe / Unsubscribe (explicit streams) | Cmd 30/31 | ✅ fully compliant | Wildcard auto-subscribe on discovery |
| Implicit subscribe-via-GetDirectory (regular params) | p.30 | ✅ fully compliant | Announces flow without extra Cmd 30 |
| SetValue with confirming-announce + timeout + coerce detection | Rules 74–75 | ✅ fully compliant | `ErrWriteTimeout` / `ErrWriteCoerced` / `ErrWriteRejected` |
| Matrix GetDirectory with implicit subscribe | p.42 | ✅ fully compliant | `SendMatrixGetDirectory` |
| Matrix crosspoint connect / disconnect (nToN/oneToN/oneToOne) | p.33–34 | ✅ fully compliant | Pre-flight rejects illegal disconnect on oneToN/oneToOne |
| Matrix labels (multi-level `labels[]` SEQUENCE) | p.41 | ✅ fully compliant | N=1 wire-verified on 9092; N≥2 covered by synthetic fixture |
| Matrix `parametersLocation` (targets/sources/connections, gain) | p.38 | ✅ fully compliant | Two-deep connections with `"target.source"` composite key wire-verified |
| Function invocation + decoded `InvocationResult` | p.47–50 | ✅ fully compliant | `acp invoke` with typed args |
| Template decode (`Template` / `QualifiedTemplate`) | p.54–58 | ✅ decode only | Inflation supported on Node + Parameter; Matrix-template / Function-template inflation pending |
| StreamCollection dispatch (single-value and CollectionAggregate) | p.92–93 | ✅ fully compliant | Shared `streamIdentifier` handled; collision w/o descriptor raises compliance event |
| Announces as partial deltas (merge-on-announce) | p.85 | ✅ fully compliant | `mergeAnnouncedParameter` preserves walked metadata |
| Session dead-man + root `isOnline` cascade | p.74 | ✅ fully compliant | Synthetic root event on dead-man trip |
| Auto-reconnect with tree clear + re-walk + re-subscribe | — (our extension) | ✅ fully compliant | 2 s → 30 s back-off |
| Canonical JSON export (pointer mode) | — (our schema) | ✅ fully compliant | Wire-faithful |
| Canonical JSON export (inline / both modes — absorb references) | — (our schema) | ✅ fully compliant | `--templates` / `--labels` / `--gain` |
| Probel `parkSource`, `protect`, `Salvo` element | — (other protocols) | 🕐 parked | Not applicable to Ember+; documented in `project_probel_extensions.md` |
| TSL v5 positional tally | — (other protocols) | 🕐 parked | Not applicable to Ember+; documented in `project_tsl_extensions.md` |
| Bus bridge (NATS / Redis Stream / ES) | — (orchestrator) | 🕐 parked | Plugin stays bus-free by design |

Legend: ✅ fully compliant · ⚠ partial · ⛔ not implemented · 🕐 parked (scope extension) · ⏳ pending (in progress).

---

## Timeouts

All timeouts are deterministic, user-overridable via `--timeout`, and
structured so every wait has a bounded worst case. No silent hangs.

| Timer | Default | Where | Override |
|---|---|---|---|
| Per-command operation (connect, device info, slot info) | 30 s | `--timeout` global flag | `acp ... --timeout 10s` |
| TCP dial (session `Connect`) | 10 s | `reconnect.go`, fresh sessions | not user-tunable; spec rule-of-thumb for LAN Ember+ |
| Keep-alive dead-man (session considered dead if no RX) | 30 s (3× keep-alive) | `session.deadManThreshold` | not tunable via CLI; internal constant |
| Keep-alive RX interval (expected from provider) | ~10 s | spec p.74 | n/a (provider-side) |
| SetValue confirming-announce wait | 3 s | `set_pending.go` → `ErrWriteTimeout` | none yet (constant) |
| Walk (full GetDirectory + deferred subtrees) | signal-only (no timeout) | `Plugin.Walk` | Ctrl-C is the only interrupt |
| Reconnect back-off | 2 s → 30 s exponential | `defaultReconnectPolicy` | not tunable |
| Reconnect attempt cap | 0 (unlimited) | `defaultReconnectPolicy.MaxAttempts` | not tunable; `Disconnect()` cancels |

**Rule:** the walk is unbounded because tree size varies from a few
hundred to 20 000+ objects — a fixed timeout would break large
providers. Every other network wait is bounded.

---

## CLI Commands Reference

Every subcommand usable against an Ember+ provider, with a runnable
example and a decoded wire capture. Captures shown are real frames
from `127.0.0.1:9092` (TinyEmberPlusRouter).

For each TX/RX line below the full wire bytes include:

```
fe                BOF
00 0e 00 01       slot + msgType(0x0E=EmBER) + cmd(0x00=EmBER) + version(0x01)
c0                flags (0xC0 = single packet: FlagFirst|FlagLast)
01 02 1f 02       DTD(0x01=Glow), appBytesLen(2), minor(0x1F=31), major(0x02)
…                 BER payload (APP[0] Root envelope)
xx xx             CRC-16 (little-endian)
ff                EOF
```

### `acp walk` — enumerate the provider tree

```
acp walk 127.0.0.1 --protocol emberplus --port 9092 --timeout 10s
acp walk 127.0.0.1 --protocol emberplus --port 9092 --path router.oneToN
acp walk 127.0.0.1 --protocol emberplus --port 9092 --capture walk.jsonl          # single-file raw trace
acp walk 127.0.0.1 --protocol emberplus --port 9092 --capture cap/                # dir mode — also emits tree.json + glow.json
acp walk 127.0.0.1 --protocol emberplus --port 9092 --capture cap/ --labels inline --gain inline
acp walk 127.0.0.1 --protocol emberplus --port 9092 --filter gain
```

Flags: `--path P` (subtree only), `--filter TEXT` (case-insensitive
line filter), `--capture` (JSONL file or directory — see
[Canonical Export Modes](#canonical-export-modes)), `--all` (all
slots, no-op for Ember+), `--slot N` (default 0), `--templates`,
`--labels`, `--gain` (canonical output modes; default `pointer`).

Output begins with the tree structure, grouped by parent path:

```
slot 0:

slot 0 — 4494 objects


[router]
    1  router                raw     R--
      1  oneToN                raw     R--

[router.oneToN]
      1  labels                raw     R--
      2  matrix                raw     RW-

[router.oneToN.labels.targets]
      1  t-1                   string  RW-  "SDI-T-1"
      2  t-2                   string  RW-  "SDI-T-2"
```

Wire trace — root GetDirectory TX (32 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 10                          APP[0] Root, len 16
  6b 0e                        APP[11] RootElementCollection, len 14
    a0 0c                      CTX[0] wrapping one RootElement, len 12
      62 0a                    APP[2] Command, len 10
        a0 03 02 01 20         CTX[0] number = INTEGER 32 (GetDirectory)
        a1 03 02 01 ff         CTX[1] dirFieldMask = INTEGER -1 (All)
df 94 8f                       CRC(LE)... actually crc = 0x94df
ff
```

First RX — the root Node reply (40 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 19 6b 17 a0 15                  APP[0]{APP[11]{CTX[0]}}
  6a 13                             APP[10] QualifiedNode, len 19
    a0 03 0d 01 01                  CTX[0] path = RelOID "1"
    a1 0c 31 0a                     CTX[1] NodeContents (UNIVERSAL SET)
      a0 08 0c 06 72 6f 75 74 65 72   CTX[0] identifier = "router"
6b f3 ff                           CRC + EOF
```

### `acp get` — read a parameter value

```
acp get 127.0.0.1 --protocol emberplus --port 9092 --path router.oneToN.labels.targets.t-1
acp get 127.0.0.1 --protocol emberplus --port 9092 --path 1.1.1.1.1           # numeric
acp get 127.0.0.1 --protocol emberplus --port 9092 --label t-1                # first match
```

Flags: `--path P` (dot-separated, numeric or identifier), `--label L`
(first-match, may warn on ambiguity), `--id N` (least-specific).

Output:

```
value = "SDI-T-1"
```

Values come from the walk cache; `get` is a local lookup unless the
tree is empty (in which case a walk is auto-triggered). No wire traffic
is generated by `get` itself after the initial walk.

### `acp set` — write a parameter value

```
acp set 127.0.0.1 --protocol emberplus --port 9092 --path router.oneToN.labels.targets.t-1 --value "DEMO-T-1"
acp set 127.0.0.1 --protocol emberplus --port 9092 --path router.oneToN.parameters.sourceGain --value -10.0
acp set 127.0.0.1 --protocol emberplus --port 9000 --path 1.2.3 --value true
acp set 127.0.0.1 --protocol emberplus --port 9092 --path router.enumParam --value 2   # enum index
```

`--value` is auto-coerced to the parameter's declared type (int / real
/ string / boolean / enum). `--raw HEX` bypasses typed encoding for
escape hatches.

Output:

```
confirmed value = "DEMO-T-1"
```

Wire trace — SetValue TX for a string (46 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 1f 6b 1d a0 1b                         APP[0]{APP[11]{CTX[0]}}
  69 19                                    APP[9] QualifiedParameter, len 25
    a0 07 0d 05 01 01 01 01 01              CTX[0] path = "1.1.1.1.1.1"  (some providers split further)
    a1 0e 31 0c                             CTX[1] ParameterContents (SET)
      a2 0a 0c 08 44 45 4d 4f 2d 54 2d 31   CTX[2] value = UTF8 "DEMO-T-1"
03 79 ff                                   CRC + EOF
```

The provider echoes the confirmed value back as a normal announcement,
which `session.readLoop` decodes into the tree.

### `acp watch` — follow value-change announcements

```
acp watch 127.0.0.1 --protocol emberplus --port 9092
acp watch 127.0.0.1 --protocol emberplus --port 9092 --label t-1
acp watch 127.0.0.1 --protocol emberplus --port 9092 --id 1
```

Flags: `--path P` / `--label L` / `--id N` (filter; empty = watch all).
Blocks until Ctrl-C. No separate subscribe frame is emitted for
regular parameters — the implicit subscribe-via-GetDirectory (spec
p.30) is sufficient. Stream parameters are handled by `acp stream`.

### `acp stream` — subscribe to stream parameters

```
acp stream 127.0.0.1 --protocol emberplus --port 9092
acp stream 127.0.0.1 --protocol emberplus --port 9092 --id 45     # one streamIdentifier
```

Walks first, then sends `Command 30 (Subscribe)` for every parameter
that carries a `streamIdentifier` (spec p.30). Prints every received
`StreamEntry` with `HH:MM:SS.mmm path = value`. Blocks until Ctrl-C;
sends `Command 31` on exit.

### `acp matrix` — set matrix crosspoints

```
# absolute (replace all sources for target 1 with source 5)
acp matrix 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.oneToN.matrix --target 1 --sources 5 --op absolute

# nToN connect (add source 7 to target 3's set)
acp matrix 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.nToN.matrix --target 3 --sources 7 --op connect

# disconnect
acp matrix 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.nToN.matrix --target 3 --sources 7 --op disconnect
```

Flags: `--path P`, `--target N`, `--sources N,N,N`,
`--op absolute|connect|disconnect` (spec p.89 ConnectionOperation).
Validation fires locally before any wire traffic:

```
error: emberplus: matrix validation:
       oneToN matrix: target 1 would have 2 sources (max 1) [spec p.33]
```

Output on success:

```
matrix connect: target 1 ← sources [5] (op=absolute)
```

Wire trace — oneToN connect TX (40 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 24 6b 22 a0 20                                  APP[0]{APP[11]{CTX[0]}}
  71 1e                                             APP[17] QualifiedMatrix, len 30
    a0 05 0d 03 01 01 02                            CTX[0] path = "1.1.2"
    a5 15 30 13                                     CTX[5] connections, UNIVERSAL SEQ
      a0 11 70 0f                                   CTX[0] inner, APP[16] Connection
        a0 03 02 01 01                              CTX[0] target = 1
        a1 03 0d 01 05                              CTX[1] sources = RelOID "5"
        a2 03 02 01 00                              CTX[2] operation = 0 (absolute)
38 e6 ff                                           CRC + EOF
```

### `acp invoke` — call a function

```
acp invoke 127.0.0.1 --protocol emberplus --port 9092 --path router.functions.add --args 3,5
acp invoke 127.0.0.1 --protocol emberplus --port 9092 --path router.functions.doNothing --args ""
```

Args are comma-separated; each is auto-typed (int → float → bool →
string). The plugin allocates a fresh `invocationId` per call
(monotonic u16, 1..255). A 10 s timeout guards the response.

Output:

```
invocation 1: success=true
result: [8]
```

Wire trace — invoke add(3,5) TX (51 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 31 6b 2f a0 2d                               APP[0]{APP[11]{CTX[0]}}
  74 2b                                          APP[20] QualifiedFunction, len 43
    a0 05 0d 03 01 04 01                         CTX[0] path = "1.3.1"
    a2 22 64 20                                  CTX[2] children → APP[4] ElementCollection
      a0 1e 62 1c                                CTX[0] inner, APP[2] Command, len 28
        a0 03 02 01 21                           CTX[0] number = 33 (Invoke)
        a2 15 76 13                              CTX[2] invocation → APP[22]
          a0 03 02 01 01                         CTX[0] invocationId = 1
          a1 0c 30 0a                            CTX[1] arguments (Tuple = SEQ)
            a0 03 02 01 03                       CTX[0] Value INTEGER 3
            a0 03 02 01 05                       CTX[0] Value INTEGER 5
49 d0 ff                                        CRC + EOF
```

Wire trace — InvocationResult RX (22 bytes):

```
fe 00 0e 00 01 c0 01 02 1f 02
60 10                                         APP[0] Root, len 16
  77 0e                                        APP[23] InvocationResult, len 14
    a0 03 02 01 01                              CTX[0] invocationId = 1
    a2 07 30 05 a0 03 02 01 08                  CTX[2] result = Tuple[INTEGER 8]
42 c6 ff                                      CRC + EOF
```

(`success` field omitted → defaults to `true` per spec p.92.)

### `acp profile` — compliance classification

```
acp profile 127.0.0.1 --protocol emberplus --port 9092
acp profile 127.0.0.1 --protocol emberplus --port 9000
```

Runs a walk, prints object count + classification + every tolerance
event that fired with its hit count:

```
host             127.0.0.1:9000
objects walked   1860
classification   partial

tolerance events
  multi_frame_reassembly           3
  non_qualified_element            2619
```

Use this to build a compatibility matrix for a device fleet. Every
compliance event label is documented in the
[Compliance & Tolerance](#compliance--tolerance) section.

### `acp export` — dump the walked tree

```
acp export 127.0.0.1 --protocol emberplus --port 9092 --format json --out tree.json
acp export 127.0.0.1 --protocol emberplus --port 9092 --format yaml --out tree.yaml
acp export 127.0.0.1 --protocol emberplus --port 9092 --format csv  --out tree.csv
```

Produces a hierarchical snapshot identical to the ACP1/ACP2 export
shape, with the Ember+ identifier path under each element.

### `acp import` — replay a snapshot

```
acp import 127.0.0.1 --protocol emberplus --port 9092 --file tree.json --dry-run
acp import 127.0.0.1 --protocol emberplus --port 9092 --file tree.json
```

Reads a JSON/YAML/CSV snapshot and issues a `set` for every parameter
whose declared value differs from the live one.

### CSV columns (lossless round-trip, issue #38)

Ember+ labels are not unique — the same `gain` appears under every
channel. CSV round-trip uses the numeric dotted OID (e.g. `1.2.1.3`)
from the `oid` column as the importer's primary resolution key. The
`path` column (e.g. `router/inputs/ch1/gain`) mirrors it for humans.

```
acp convert --in tree.json --out tree.csv
acp import  127.0.0.1 --protocol emberplus --port 9092 --file tree.csv --dry-run   # applied N, failed 0
```

`failed 0` on an unchanged device confirms duplicate-label elements
(per-channel `gain`, `mute`, `meter`) round-trip unambiguously.

### `--capture` (global flag, two modes)

**File mode** — `--capture FILE.jsonl` writes every raw S101 frame
(tx + rx, including BOF/EOF/CRC) to one append-only JSONL file. Used
by unit-test replay (`tests/unit/emberplus/*_replay_test.go`).

```
acp walk 127.0.0.1 --protocol emberplus --port 9092 --capture walk.jsonl
```

Record format:

```json
{"ts":"2026-04-18T15:56:07.126Z","proto":"emberplus","dir":"tx",
 "hex":"fe000e0001c001021f0260106b0ea00c620aa003020120a1030201fddf948fff",
 "len":32}
```

**Directory mode** — any `--capture` value that is a directory (or a
path without a `.jsonl` / `.json` extension) triggers a three-file
dump:

```
acp walk 127.0.0.1 --protocol emberplus --port 9092 --capture cap/
→ cap/raw.s101.jsonl   append-only wire trace (same as file mode)
  cap/glow.json        decoded Glow tree (lossless, numeric-sorted)
  cap/tree.json        canonical Export (schema.md shape)
```

The three files let replay tests cross-check at every layer: bytes on
the wire (`raw.s101.jsonl`), decoded elements (`glow.json`), and the
canonical user-facing shape (`tree.json`).

---

## Canonical Export Modes

The `tree.json` output is controlled by three mode flags. Defaults are
all `pointer` — wire-faithful, zero inflation, smallest memory
footprint. `inline` absorbs referenced subtrees into the referring
element. `both` keeps both forms.

| flag | spec § | pointer (default) | inline | both |
|---|---|---|---|---|
| `--templates` | §8 (Templates) | Parameter / Node carry `templateReference` OID only | Template fields (shape, children) copied into referring element; pointer dropped | pointer kept **and** shape inlined |
| `--labels` | §5.1.1 (Matrix Labels) | Matrix `labels[]` array of `{basePath, description}` preserved; label subtrees walked as regular Nodes | label subtree(s) absorbed into Matrix as `targetLabels` / `sourceLabels` keyed by level description; label Nodes removed from tree | pointer + absorbed maps; label Nodes kept |
| `--gain` | §5.1.2 (parametersLocation) | Matrix `parametersLocation` OID preserved; params subtree walked as regular Node | params subtree absorbed into Matrix as `targetParams` / `sourceParams` / `connectionParams`; params Node removed from tree | pointer + absorbed maps; params Node kept |

### Multi-level labels (spec §5.1.1)

A matrix can declare **multiple label levels** (e.g. "Primary",
"Long", "Engineering"). Each level is one entry in `labels[]` with
its own `basePath` and `description`. The canonical shape preserves
this in all modes:

**Pointer mode** (wire-faithful):

```json
"labels": [
  {"basePath": "1.2.3.1", "description": "Primary"},
  {"basePath": "1.2.3.2", "description": "Long"}
]
```

**Inline mode** (absorbed, keyed by description):

```json
"labels": [ … same as pointer … ],
"targetLabels": {
  "Primary": {"0": "OUT 1",  "1": "OUT 2",  …},
  "Long":    {"0": "Output Main Video", "1": "Output Backup", …}
},
"sourceLabels": {
  "Primary": {"0": "MIC 1", "1": "MIC 2", …},
  "Long":    {"0": "Microphone 1 long", …}
}
```

If a `labels[i].description` is empty on the wire, the resolver keys
by `basePath` instead and raises `matrix_label_description_empty`.

### Connection params (two-deep structure)

Matrix `parametersLocation` may include a `connections` subtree
containing per-crosspoint Parameters (typically `gain`). The wire
shape is two-deep: `connections/<target>/<source>/<param>`. The
canonical inline form flattens this to a composite key:

```json
"connectionParams": {
  "3.3": {"gain": 10},
  "6.3": {"gain": 5}
}
```

Split the key on `.` to recover `target` and `source` indices.

### Size impact — wire example (9092 TinyEmberPlus router, 3 matrices × 100 targets × 100 sources)

| mode combo | tree.json size |
|---|---|
| `--labels pointer --gain pointer` (defaults) | 5.1 MB |
| `--labels inline --gain pointer` | 1.4 MB (-73%) |
| `--labels inline --gain inline` | 768 KB (-85%) |
| `--labels both --gain both` | 5.9 MB (+15% for debug/round-trip) |

`inline` eliminates the Parameter envelope overhead per label
(identifier, path, oid, description, isOnline, access, children,
type, value, default, minimum, maximum, step, unit, format, factor,
formula, enumeration, enumMap, streamIdentifier, streamDescriptor,
templateReference, schemaIdentifiers) — replaced by a single
`"0": "SDI-T-0"` pair.

---

## Data Model Layers

```
S101 frame (BOF … CRC … EOF)
  → BER payload (ASN.1 encoding)
    → Glow Root envelope [APPLICATION 0] (spec p.93)
      → one of:
          RootElementCollection  [APPLICATION 11]
          StreamCollection       [APPLICATION  6]
          InvocationResult       [APPLICATION 23]
```

Every consumer request we emit is wrapped with the full
`[APP 0] { [APP 11] { CTX[0] { element } } }` envelope. Some providers
accept bare `[APP 11]` (lax); strict providers reject it.

---

## Glow Element Types

All types verified against the **Glow DTD ASN.1 Notation (pp. 83–93)**.

### Application-defined tags

| Type | Tag | Page | Supported |
|---|---|---|---|
| Root envelope | APPLICATION[0] | 93 | ✓ encode + decode |
| Parameter | APPLICATION[1] | 85 | ✓ |
| Command | APPLICATION[2] | 86 | ✓ |
| Node | APPLICATION[3] | 87 | ✓ |
| ElementCollection | APPLICATION[4] | 92 | ✓ |
| StreamEntry | APPLICATION[5] | 93 | ✓ |
| StreamCollection | APPLICATION[6] | 93 | ✓ |
| StringIntegerPair | APPLICATION[7] | 86 | ✓ |
| StringIntegerCollection | APPLICATION[8] | 86 | ✓ |
| QualifiedParameter | APPLICATION[9] | 85 | ✓ |
| QualifiedNode | APPLICATION[10] | 87 | ✓ |
| RootElementCollection | APPLICATION[11] | 93 | ✓ |
| StreamDescription | APPLICATION[12] | 86 | ✓ |
| Matrix | APPLICATION[13] | 88 | ✓ |
| Target | APPLICATION[14] | 89 | ✓ |
| Source | APPLICATION[15] | 89 | ✓ |
| Connection | APPLICATION[16] | 89 | ✓ |
| QualifiedMatrix | APPLICATION[17] | 89 | ✓ |
| Label | APPLICATION[18] | 89 | ✓ |
| Function | APPLICATION[19] | 91 | ✓ |
| QualifiedFunction | APPLICATION[20] | 91 | ✓ |
| TupleItemDescription | APPLICATION[21] | 91 | ✓ |
| Invocation | APPLICATION[22] | 91 | ✓ encode + decode |
| InvocationResult | APPLICATION[23] | 92 | ✓ decode (success default true when omitted) |
| Template | APPLICATION[24] | 84 | ✓ decode (instantiation = provider; out of consumer scope) |
| QualifiedTemplate | APPLICATION[25] | 84 | ✓ decode |

### Node — NodeContents SET (p.87)

| CTX | Field | Type | Notes |
|---|---|---|---|
| 0 | identifier | EmberString | |
| 1 | description | EmberString | |
| 2 | isRoot | BOOLEAN | — |
| 3 | isOnline | BOOLEAN | default true |
| 4 | schemaIdentifiers | EmberString | newline-separated |
| 5 | templateReference | RelOID | |

Node wrapper APPLICATION[3] / QualifiedNode APPLICATION[10]:
`{ 0:number|path, 1:contents, 2:children }`.

### Parameter — ParameterContents SET (p.85)

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | value | Value CHOICE (int/real/string/bool/octets/null) |
| 3 | minimum | MinMax |
| 4 | maximum | MinMax |
| 5 | access | ParameterAccess (0 none / 1 read / 2 write / 3 readWrite) |
| 6 | format | EmberString (printf-style; `°` introduces unit) |
| 7 | enumeration | EmberString (legacy newline list) |
| 8 | factor | Integer32 |
| 9 | isOnline | BOOLEAN |
| 10 | formula | EmberString (`provider\|consumer` split) |
| 11 | step | Integer32 |
| 12 | default | Value |
| 13 | type | ParameterType (null/integer/real/string/boolean/trigger/enum/octets) |
| 14 | streamIdentifier | Integer32 |
| 15 | enumMap | StringIntegerCollection |
| 16 | streamDescriptor | StreamDescription |
| 17 | schemaIdentifiers | EmberString |
| 18 | templateReference | RelOID |

### Matrix — MatrixContents SET (p.88)

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | type | MatrixType (0 oneToN / 1 oneToOne / 2 nToN) |
| 3 | addressingMode | MatrixAddressingMode (0 linear / 1 nonLinear) |
| 4 | targetCount | Integer32 |
| 5 | sourceCount | Integer32 |
| 6 | maximumTotalConnects | Integer32 (nToN) |
| 7 | maximumConnectsPerTarget | Integer32 (nToN) |
| 8 | parametersLocation | CHOICE basePath RelOID / inline Integer32 |
| 9 | gainParameterNumber | Integer32 |
| 10 | labels | LabelCollection (SEQ OF CTX[0] Label) |
| 11 | schemaIdentifiers | EmberString |
| 12 | templateReference | RelOID |

Matrix wrapper APPLICATION[13] / QualifiedMatrix APPLICATION[17]:
`{ 0:number|path, 1:contents, 2:children, 3:targets, 4:sources, 5:connections }`.

### Connection — APPLICATION[16] (p.89)

| CTX | Field | Type | Default |
|---|---|---|---|
| 0 | target | Integer32 | — |
| 1 | sources | PackedNumbers RelOID | empty = none |
| 2 | operation | ConnectionOperation (0 absolute / 1 connect / 2 disconnect) | absolute |
| 3 | disposition | ConnectionDisposition (0 tally / 1 modified / 2 pending / 3 locked) | tally |

### Label — APPLICATION[18] (p.89)

| CTX | Field | Type |
|---|---|---|
| 0 | basePath | RelOID |
| 1 | description | EmberString |

### Function — FunctionContents SET (p.91)

| CTX | Field | Type |
|---|---|---|
| 0 | identifier | EmberString |
| 1 | description | EmberString |
| 2 | arguments | TupleDescription (SEQ OF CTX[0] TupleItemDescription) |
| 3 | result | TupleDescription |
| 4 | templateReference | RelOID |

TupleItemDescription APPLICATION[21]: `{ 0:type ParameterType, 1:name EmberString }`.

### Invocation — APPLICATION[22] (p.91)

| CTX | Field | Type |
|---|---|---|
| 0 | invocationId | Integer32 (consumer-allocated, 1..255) |
| 1 | arguments | Tuple (SEQ OF CTX[0] Value) |

### InvocationResult — APPLICATION[23] (p.92)

| CTX | Field | Type | Notes |
|---|---|---|---|
| 0 | invocationId | Integer32 | echoed by provider |
| 1 | success | BOOLEAN | **default true when omitted** (spec p.92) |
| 2 | result | Tuple (SEQ OF CTX[0] Value) | empty when void |

### StreamEntry — APPLICATION[5] (p.93)

| CTX | Field | Type |
|---|---|---|
| 0 | streamIdentifier | Integer32 |
| 1 | streamValue | Value CHOICE |

### StreamDescription — APPLICATION[12] (p.86)

Describes how to decode a shared stream blob at a given byte offset:

| CTX | Field | Type |
|---|---|---|
| 0 | format | StreamFormat enum |
| 1 | offset | Integer32 |

`StreamFormat` values implemented (all 14 per DTD p.86):

```
unsignedInt8             0
unsignedInt16BE          2
unsignedInt16LE          3
unsignedInt32BE          4
unsignedInt32LE          5
unsignedInt64BE          6
unsignedInt64LE          7
signedInt8               8
signedInt16BE           10
signedInt16LE           11
signedInt32BE           12
signedInt32LE           13
signedInt64BE           14
signedInt64LE           15
float32BE               20
float32LE               21
float64BE               22
float64LE               23
```

### Template / QualifiedTemplate — APPLICATION[24] / [25] (p.84)

```
Template SET          { 0:number,  1:element TemplateElement, 2:description }
QualifiedTemplate SET { 0:path,    1:element TemplateElement, 2:description }
TemplateElement CHOICE over Parameter / Node / Matrix / Function
```

### Command — APPLICATION[2] (p.86)

```
Command ::= SEQUENCE {
  number   [0] CommandType,                        -- 30 subscribe, 31 unsubscribe,
                                                   -- 32 getDirectory, 33 invoke
  options  CHOICE {
    dirFieldMask [1] FieldFlags,                   -- valid only when number = 32
    invocation   [2] Invocation                    -- valid only when number = 33
  } OPTIONAL
}
```

Mask values sent by our consumer: `dirFieldMask = All (-1)` — asks the
provider to return every property. Other documented values: Default (0),
Identifier (1), Description (2), Tree (3), Value (4), Sparse (-2, Glow
2.50+).

---

## Paths

Ember+ identifies every element by a RELATIVE-OID. We index the tree
three ways after walk:

| Key | Example | Use |
|---|---|---|
| Numeric path (primary) | `1.1.2.190` | Unambiguous, O(1) |
| Identifier path | `router.oneToN.labels.targets.t-190` | Human-readable, O(1) |
| Bare label | `t-190` | May collide — warned on ambiguity |

Separator is `.` everywhere. Non-qualified Nodes/Parameters that omit
the RELATIVE-OID get their numeric path synthesised from the walk
ancestry (see Compliance — `non_qualified_element`).

---

## Discovery (Walk)

Walk sends `Root[Command(GetDirectory, dirFieldMask=All)]` at the top
level, then follows each Node's children using a `QualifiedNode(path) →
Command(GetDirectory)` request. A settle timer ends the walk after 2 s
of silence (bounded at 15 s).

```
acp walk 127.0.0.1 --protocol emberplus --port 9000 --slot 0
acp walk 127.0.0.1 --protocol emberplus --port 9092 --slot 0 --path router.oneToN
```

Typical scales:

| Provider | Objects | Walk time |
|---|---|---|
| TinyEmberPlusRouter (9092) | 4494 | 2–3 s |
| TinyEmberPlus DTD 2.31 (9000) | 1860 | ~2 s |

### Value freshness

Each entry carries a freshness state:

| State | Trigger | UI hint |
|---|---|---|
| `live` | Observed in walk, get, or announcement | Normal |
| `updated` | Value-change announcement just arrived | Flash |
| `stale` | Loaded from disk cache (not yet implemented) | Gray/italic |

---

## Get / Set

Values are cached in-RAM from the walk and updated by announcements.
`get` reads the cached value; `set` sends a SetValue request.

```
acp get 127.0.0.1 --protocol emberplus --port 9000 --path router.oneToN.labels.targets.t-1
acp set 127.0.0.1 --protocol emberplus --port 9000 --path router.oneToN.labels.targets.t-1 --value "A1-TEST"
```

`--value` is parsed according to the parameter's declared type
(integer/real/string/boolean/enum). An unknown type falls back to
string.

---

## Subscriptions (Announcements)

Spec p.30–31. Two mechanisms:

| Parameter kind | Mechanism | Wire traffic |
|---|---|---|
| Regular (streamIdentifier = 0 or absent) | Implicit — provider announces on change after GetDirectory | None beyond Walk |
| Streamed (streamIdentifier ≠ 0) | Explicit `Command 30` per path | Subscribe per param + StreamCollection per tick |

On Disconnect the plugin sends `Command 31` for every streamed sub.

```
acp watch  127.0.0.1 --protocol emberplus --port 9000
acp watch  127.0.0.1 --protocol emberplus --port 9000 --path router.oneToN.labels.targets.t-1
acp stream 127.0.0.1 --protocol emberplus --port 9000 --id 45
```

---

## Matrix Operations

Raw wire state (per provider tally) kept under each Matrix entry as a
`matrix.State`. CLI sends a Connection; plugin validates locally
(`canConnect`) before putting it on the wire.

```
acp matrix 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.oneToN.matrix --target 1 --sources 5 --op absolute
```

### canConnect rules (pre-flight validation, spec p.33–34 + p.89)

| Type | Rule |
|---|---|
| oneToN | Max 1 source per target; connect replaces |
| oneToOne | Max 1 source per target AND source used ≤ 1× globally |
| nToN | `len(sources[t]) ≤ MaxConnectsPerTarget`; `sum(all tgts) ≤ MaxTotalConnects` |
| any | Reject if target's disposition == `locked` |

Rejection on invalid op returns a spec-cited error:

```
error: emberplus: matrix validation:
       oneToN matrix: target 1 would have 2 sources (max 1) [spec p.33]
```

### Derived state (our extension over spec)

Wire Connection stays spec-pure; we hold a richer record alongside for UIs:

| Field | Source |
|---|---|
| `Target, Sources[], Operation, Disposition` | spec Connection verbatim |
| `LastChanged` | audit timestamp |
| `ChangedBy` (walk / announce / user) | attribution |
| `LabelTarget / LabelSources[]` | resolved from `MatrixContents.labels.basePath` |
| `ResolvedGainDb` | follows `gainParameterNumber` under `parametersLocation` |

Derived fields are RAM-only; never serialized onto the wire.

### parametersLocation addressing (spec p.38)

```
parametersLocation is CHOICE { basePath RelOID, inline Integer32 }

Under that node:
  targets.<n>                     → per-target params
  sources.<n>                     → per-source params
  connections.<t>.<s>             → per-connection params (gain lives here)
```

---

## Function Invocation

Spec p.47–50. Session-local monotonic `invocationId` (range 1..255,
0 reserved for announcements).

```
acp invoke 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.functions.add --args 3,5
→ invocation 1: success=true
  result: [8]
```

Args are auto-typed (integer first, then float, then string, then bool).
A 10 s timeout guards the response channel.

---

## Streams

Spec p.22, p.30–31, p.86, p.93.

1. Walk indexes every Parameter with a non-zero `streamIdentifier`.
2. On subscribe, the plugin sends `Command 30` for every streamed path.
3. The provider transmits `StreamCollection { StreamEntry+ }` frames.
4. For each entry, the plugin:
   - looks up every parameter sharing that `streamIdentifier`
   - if `streamDescriptor` is present, slices the blob at `offset` and
     decodes per `format` (all 14 StreamFormat values implemented)
   - delivers the decoded value via the registered callback and flips
     the entry's freshness to `updated`

---

## Templates

Spec p.54–58 (Ember+ 1.4).

Consumer-side: Template APPLICATION[24] and QualifiedTemplate
APPLICATION[25] are decoded and stored. `templateReference` (RelOID) on
any Node/Parameter/Matrix/Function can be resolved to its prototype via
`Plugin.ResolveTemplate(path)`.

Instantiation (merging template properties into referencing elements)
is **not in the consumer**; that's provider-side work (Part B, future).

---

## Compliance & Tolerance

Every provider we tested deviates from the spec somewhere. Our plugin
tolerates each deviation silently and counts it into a per-session
`compliance.Profile`. The `acp profile` CLI surfaces the profile so you
can build a compatibility matrix per device.

### Design principle

**Fail-open, not fail-hard.** Unless the frame is outright corrupt (bad
BER structure, wrong CRC), decode what we can and keep going. Users
should be able to read values from a non-compliant provider without
touching their code.

### Tolerance features

| Feature | Decoder entry point |
|---|---|
| Accept bare `[APP 11]` without outer `[APP 0]` Root envelope | `decodeElements` dispatches both |
| Accept contents without the UNIVERSAL SET envelope | `unwrapSet` falls back to CTX children |
| Accept Tuple as direct CTX[0] values (no SEQUENCE) | `decodeTuple` second pass |
| Accept ElementCollection inlined without APP[4] wrapper | `decodeElementCollection` walks both |
| Accept Label / StringIntegerPair at variable nesting | `flattenForApp` recurses |
| Accept primitive-form scalars inside contents | `unwrapPrimitive` + `decodeAnyValue` |
| Synthesise numeric path for non-qualified elements | `Plugin.resolveNumPath` |
| Reassemble S101 `FlagFirst / FlagLast` chains | `session.readLoop` |
| Default `Operation=absolute`, `Disposition=tally` on Connection | spec p.89 |
| Default `Success=true` on InvocationResult | spec p.92 |
| Skip unknown APP / CTX tags silently | decoder switch default |
| Auto-walk on first `get / set / subscribe` | `Plugin.ensureWalked` |

### compliance.Profile event labels

Every event is a named counter — atomic int64, zero allocations on the
hot path. The full catalog lives in
`internal/protocol/emberplus/compliance/profile.go`. Grouped by
source:

**Wire-level deviations (decoder tolerance)**

| Event | Meaning |
|---|---|
| `non_qualified_element` | Node / Parameter / Matrix / Function delivered without RelOID path |
| `multi_frame_reassembly` | S101 FlagFirst/FlagLast chain observed |
| `invocation_success_default` | InvocationResult omitted the `success` field (spec p.92) |
| `connection_operation_default` | Connection omitted the `operation` field (default `absolute`, p.89) |
| `connection_disposition_default` | Connection omitted the `disposition` field (default `tally`, p.89) |
| `contents_set_omitted` | Contents arrived without UNIVERSAL SET envelope (p.85) |
| `tuple_direct_ctx` | Tuple arrived as bare CTX[0] items (p.92) |
| `element_collection_bare` | ElementCollection inlined without APP[4] wrapper |
| `unknown_tag_skipped` | Vendor-private APP / CTX tag observed |

**Enum / field handling**

| Event | Meaning |
|---|---|
| `enum_masked_item` | Enum option carries smh `~` mask prefix (stripped, flagged non-selectable) |
| `enum_double_source` | Parameter carries both native `EnumMap` AND legacy `Enumeration` with differing count |
| `enum_map_derived` | Canonical `EnumMap` synthesised from legacy `Enumeration` (no native map on wire) |
| `field_inferred` | Canonical field synthesised from a protocol-specific source (e.g. `type` inferred from `Value` CHOICE) |

**Resolver (--templates / --labels / --gain)**

| Event | Meaning |
|---|---|
| `matrix_label_basepath_unresolved` | `matrix.labels[i].basePath` does not point to a walked Node |
| `matrix_label_none` | Matrix ships no `labels[]` array or an empty one |
| `matrix_label_description_empty` | `labels[i].description` blank — resolver keys by basePath |
| `matrix_label_level_mismatch` | Two label levels expose different target/source counts |
| `matrix_parameters_location_unresolved` | `parametersLocation` does not point to a walked Node |
| `template_reference_unresolved` | `templateReference` points to unknown Template path |
| `labels_absorbed` | `--labels=inline` succeeded — label subtree(s) removed from tree, content on matrix |
| `gain_absorbed` | `--gain=inline` succeeded — params subtree removed, content on matrix |
| `template_absorbed` | `--templates=inline` succeeded — template content inflated into referring element |

**Stream dispatch**

| Event | Meaning |
|---|---|
| `stream_id_collision_no_descriptor` | Two Parameters share a `streamIdentifier` with at least one missing `streamDescriptor` (spec §7 forbids — provider bug) |

A provider is classified **strict** if zero events fire, **partial** if
any event fires. Info-only events (`multi_frame_reassembly`,
`enum_masked_item`, `matrix_label_none`, `labels_absorbed`, etc.) do
not imply a problem — they document what happened.

### Provider matrix (from this machine)

Run `acp profile <host>` to regenerate.

| Host:Port | Provider | Objects | Classification | Deviations |
|---|---|---|---|---|
| 127.0.0.1:9092 | TinyEmberPlusRouter | 4494 | partial | `multi_frame_reassembly=4` |
| 127.0.0.1:9000 | TinyEmberPlus DTD 2.31 | 1860 | partial | `multi_frame_reassembly=3`, `non_qualified_element=2619` |

Disconnect log line (structured, ready for Loki / any aggregator):

```
emberplus: compliance profile host=127.0.0.1 port=9000 classification=partial
  deviations="multi_frame_reassembly=3 non_qualified_element=2619"
```

### CLI

```
acp profile 127.0.0.1 --protocol emberplus --port 9000
```

---

## Raw Capture

```
acp walk 127.0.0.1 --protocol emberplus --port 9000 --slot 0 --capture walk9000.jsonl
```

Format: JSONL, one line per wire message. Used for unit-test replay
and protocol analysis.

---

## Error Reference

Every error surfaced by the consumer has a stable name, a source
layer, and a recovery path. Errors are classified so operators know
whether to retry, reconfigure, or escalate.

### Transport layer (TCP / S101)

| Name | When | Wire evidence | Recovery |
|---|---|---|---|
| `protocol: not connected` | Any op (`get`/`set`/`watch`/…) called after session dropped | no session alive | auto-reconnect goroutine retries 2 s → 30 s back-off; user's `acp watch` keeps showing last known values until tree clears |
| `S101SocketError` | TCP connect / read / write failure | OS syscall error | Reconnect goroutine covers transient; persistent = firewall / device down |
| `context deadline exceeded` | Per-op timeout (`--timeout`) trips | any unanswered request | Raise `--timeout`; walks on large trees (20 k+ objects) can take minutes |
| CRC mismatch (no named error — frame silently dropped, logged at `debug`) | Frame byte-corrupt on wire | `emberplus: crc mismatch` debug log | Line-quality issue; check switch / cable |

### Decode layer (BER + Glow)

| Name | When | Recovery |
|---|---|---|
| `InvalidBERFormat` | Frame passes CRC but BER parse fails | Provider bug; open capture, inspect frame, file bug |
| `MissingElementNumber` | Element arrived without `[0] number` | Same — decoder can't place it in the tree |
| `MissingElementContents` | Contents `CTX[1]` absent where spec requires | Same |
| Unknown APP/CTX tag | Vendor-private extension encountered | Silent — fires `unknown_tag_skipped` compliance event; no user-visible error |

### Addressing / tree layer

| Name | When | Recovery |
|---|---|---|
| `UnknownElement` | User referenced a path / label / OID not in the walk | Check `acp walk --path <prefix>` output; path may be under a subtree that hasn't been GetDirectory'd yet |
| `PathDiscoveryFailure` | Walk couldn't resolve a RelOID prefix | Usually partial GetDirectory response; retry walk after reconnect |
| `InvalidMatrixSignal` | Target / source number outside `[0, targetCount)` or `[0, sourceCount)` | User input error — check matrix dimensions via `acp walk --path <matrix>` |

### Write / invoke layer

| Name | When | Wire evidence | Recovery |
|---|---|---|---|
| `protocol: write confirmation timeout` (`ErrWriteTimeout`) | `acp set` sent but provider didn't echo back within 3 s | SetValue frame TX, no confirming announce RX | Value state unknown; do a `get` to check, then retry if still off |
| `protocol: write accepted but value coerced: expected=X actual=Y` (`ErrWriteCoerced`) | Provider clamped / rounded / rejected precision | confirming announce carries different value | Reissue with the coerced value OR accept provider's limits |
| `protocol: write rejected by provider` (`ErrWriteRejected`) | Provider echoed unchanged | confirming announce with unchanged value | Target likely locked, offline, or user lacks permission |
| `oneToN/oneToOne matrices reject disconnect; reroute to a silence source instead, or use op=absolute` | `acp matrix --op disconnect` on oneToN/oneToOne matrix | pre-flight rejects before wire TX | Per spec p.33 invariant; use `--op absolute` with a silence source |
| `InvalidFunctionCall` | Arg count / type mismatch vs function's `TupleDescription` | none (pre-flight) | Match args to `TupleDescription` (see `acp walk` on the function path) |
| `UnsupportedValue` | Go value can't be encoded to any Glow scalar | none (pre-flight) | Coerce to one of: integer / real / string / boolean / enum index / octets |

### CLI exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Protocol error (reply from device = failure, or decode error) |
| 2 | Validation / usage error |
| 3 | Transport error (can't reach device) |
| 5 | Bad CLI flags |

---

## Test Devices

| Device | IP | Port | Notes |
|---|---|---|---|
| TinyEmberPlusRouter | 127.0.0.1 | 9092 | Router variant, DTD 2.31, 4494 objects |
| TinyEmberPlus | 127.0.0.1 | 9000 | Plain DTD 2.31, 1860 objects, strict spec |

Both are available in the `Ember+Tools` bundle from Lawo's portal.
