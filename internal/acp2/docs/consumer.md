# ACP2 Connector

Consumer connector for ACP v2 (Axon Neuron protocol) over AN2 transport.

---

## References

| Document | Path | Description |
|---|---|---|
| ACP2 spec (authoritative) | [internal/acp2/assets/acp2_protocol.pdf](../../../internal/acp2/assets/acp2_protocol.pdf) | ACP v2 full specification |
| AN2 spec (authoritative) | [internal/acp2/assets/an2_protocol.pdf](../../../internal/acp2/assets/an2_protocol.pdf) | AN2 transport specification |
| Wireshark dissector | [internal/acp2/wireshark/dhs_acpv2.lua](../../../internal/acp2/wireshark/dhs_acpv2.lua) | AN2 + ACP2 byte-exact reference |
| Protocol reference | [CLAUDE.md](../../../CLAUDE.md) — section "ACP2" | Wire format, functions, properties |
| Testdata captures | [tests/fixtures/acp2/](../../../tests/fixtures/acp2/) | Raw JSONL captures from real device |
| Export fixtures | [tests/fixtures/exports/acp2/](../../../tests/fixtures/exports/acp2/) | JSON/YAML/CSV per slot |
| Source code | [internal/protocol/acp2/](../../../internal/protocol/acp2/) | Plugin implementation |
| Unit tests | [tests/unit/acp2/](../../../tests/unit/acp2/) | Replay + spec tests |

---

## Transport

ACP2 runs **exclusively over AN2/TCP**. No direct UDP or TCP variant.

| Layer | Protocol | Port | Description |
|---|---|---|---|
| Transport | TCP | 2072 | Single TCP connection per device |
| Framing | AN2 | — | Magic 0xC635, proto/slot/mtid/type/dlen header |
| Application | ACP2 | — | Carried inside AN2 data frames (proto=2) |

### AN2 initialization sequence

```
connect TCP :2072
→ AN2 GetVersion       (proto=0)
→ AN2 GetDeviceInfo    (proto=0)  → slot count
→ AN2 GetSlotInfo(n)   (proto=0)  → per slot
→ AN2 EnableProtocolEvents([2])   ← REQUIRED for ACP2 announces
→ ACP2 GetVersion      (proto=2)
→ ACP2 GetObject(slot, root)      → walk from root
```

### Firewall rules

```
TCP 2072  outbound    (AN2/TCP — single connection, multiplexes all slots)
```

### Key transport facts

- Single TCP connection handles ALL slots (AN2 frame header slot field)
- AN2 mtid is SEPARATE from ACP2 mtid
- Multiple consumers connect simultaneously (us, Cerebrum, third-party)
- Per-slot lifecycle is LOGICAL (subscribe/unsubscribe), not PHYSICAL (TCP stays open)

---

## Capabilities & Compliance Status

| Capability | Spec page | Status | Notes |
|---|---|---|---|
| AN2/TCP transport (port 2072) | an2_protocol.pdf §"Frame Header" | ✅ fully compliant | Magic `0xC635`, 8-byte header, u16 dlen |
| AN2 handshake (GetVersion → GetDeviceInfo → GetSlotInfo → EnableProtocolEvents) | an2_protocol.pdf §"Connection Setup" | ✅ fully compliant | Runs once on Connect; EnableProtocolEvents([2]) required for ACP2 announces |
| ACP2 header decode (type/mtid/func/pid) | acp2_protocol.pdf p.4 | ✅ fully compliant | Byte-exact per spec; mtid 0 reserved for announces |
| Four functions (GetVersion/GetObject/GetProperty/SetProperty) | acp2_protocol.pdf p.5 | ✅ fully compliant | All four encoded + decoded |
| Six object types (node/preset/enum/number/ipv4/string) | acp2_protocol.pdf p.7 | ✅ fully compliant | Unknown types fire `acp2_unknown_object_type` |
| Twelve number types (s8/s16/s32/s64/u8/u16/u32/u64/float/preset/ipv4/string) | acp2_protocol.pdf p.7 | ✅ fully compliant | Unknown number types fire `acp2_unknown_number_type` |
| Property header + alignment (plen excludes padding) | acp2_protocol.pdf p.6 | ✅ fully compliant | 4-byte alignment rule; plen overrun fires `acp2_property_length_overrun` |
| All 20 property IDs (object_type … preset_parent) | acp2_protocol.pdf p.6 | ✅ fully compliant | Unknown PIDs fire `acp2_unknown_property_id` and are skipped |
| Preset depth + active index (idx=0 substitution) | acp2_protocol.pdf p.8 | ✅ fully compliant | pid=7 drives idx enumeration; out-of-range fires `acp2_preset_index_out_of_range` |
| Announces (type=2, mtid=0) | acp2_protocol.pdf p.9 | ✅ fully compliant | Cross-multiplexed with replies via reader goroutine; non-zero mtid fires event |
| SetProperty confirm echo | acp2_protocol.pdf p.5 | ✅ fully compliant | Coerced values fire `acp2_set_value_coerced` |
| Retry / mtid rules | acp2_protocol.pdf p.4 | ✅ fully compliant | mtid 1..255 pool with defer-based release; never reuse in-flight |
| Value freshness (live / updated / stale / cache) | — (our extension) | ⚠ partial | Walk cache has TTL; per-object freshness tags pending — covered in follow-up |
| Cascade on disconnect (root `isOnline y→n`) | — (our extension) | ⏳ pending | Same scope as ACP1 — pattern to lift from Ember+ |
| Auto-reconnect goroutine | — (our extension) | ⏳ pending | Session reader exits on socket close; reconnect + re-walk + re-subscribe pending |
| Compliance profile + `acp profile` CLI | — | ✅ fully compliant | Event catalog in `internal/protocol/acp2/compliance_events.go`; stat-code events wired today, framing / property events ship as the decoder paths get audited |
| Canonical JSON export + `--capture <dir>` → `tree.json` | — (our schema) | ✅ fully compliant | Device→Slot→Node→Parameter hierarchy rebuilt from walker's DFS path |
| Canonical export mode flags `--templates` / `--labels` / `--gain` | — | ⛔ not applicable | ACP2 has no `templateReference`, no matrix labels SEQUENCE, no `parametersLocation`; flags pass through as no-ops |
| Matrix element | — | ⛔ not applicable | ACP2 has no matrix type (Ember+ only) |

Legend: ✅ fully compliant · ⚠ partial · ⛔ not applicable · ⏳ pending (on roadmap).

---

## Timeouts

All timeouts are deterministic, user-overridable via `--timeout`. No silent hangs.

| Timer | Default | Where | Override |
|---|---|---|---|
| Per-command operation (get / set / single property) | `--timeout` global flag | `commonFlags.timeout` | `acp ... --timeout 5s` |
| Tree walk (any slot size) | **no deadline** | `cmd_walk.go` uses raw ctx | Ctrl-C interrupts |
| Pre-walk for label resolution inside get / set / import | **no deadline** | fixed in `fix/timeout-walk-scoping` | — |
| TCP connect + AN2 init | max(`--timeout`, 5 s) | `connect()` floor in `cmd/acp/common.go` | `acp ... --timeout 30s` |
| Reader goroutine blocking read | no per-read deadline | `session.readLoop` uses `ReadFull` | socket close propagates via `s.done` |
| Walked tree cache TTL | 10 min | `newWalkedTreeCache` | constant — re-walk forces refresh |
| Walked tree cache max entries | 32 | `newWalkedTreeCache` | constant |

**Rule:** the walk is "take as long as it takes" (44 000 objects on a CONVERT Hybrid slot finish in 3–5 minutes). Only individual `GetProperty` / `SetProperty` round-trips are bounded by `--timeout` — those should never exceed 100 ms on a healthy LAN.

---

## Canonical Export Modes

`acp walk --protocol acp2 --capture <dir>` writes `tree.json` in the canonical shape documented at [docs/protocols/schema.md](../schema.md). Device → Slot → hierarchical Nodes reconstructed from the walker's DFS path → Parameter leaves:

```
{
  "root": {
    "oid": "1", "identifier": "device",
    "children": [
      { "oid": "1.2", "identifier": "slot-1", "access": "read",
        "children": [
          { "oid": "1.2.1", "identifier": "ROOT_NODE_V2",
            "children": [
              { "oid": "1.2.47431", "identifier": "BOARD",
                "children": [
                  { "oid": "1.2.47431",  "identifier": "ACP Trace",
                    "type": "enum", "access": "readWrite", "value": "Off",
                    "enumMap": [{"key":"Off","value":0},{"key":"On","value":1}] },
                  …
                ]
              },
              { "oid": "1.2.4", "identifier": "IDENTITY",
                "children": [
                  { "oid": "1.2.3",  "identifier": "User Label 1",
                    "type": "string", "access": "readWrite", "value": "",
                    "format": "maxLen=17" },
                  …
                ]
              }
            ]
          }
        ]
      }
    ]
  }
}
```

OID scheme is `1.<slot+1>.<obj_id>` (flat — ACP2 object IDs are already globally unique per slot; no hierarchical dotted form is needed). Hierarchy is carried by the `children[]` links; `oid` stays unique but shallow.

Per-kind → canonical type mapping:

| ACP2 ObjType | NumberType | Canonical type | Notes |
|---|---|---|---|
| `Number` | `S8/S16/S32/S64` | `integer` | Signed widths |
| `Number` | `U8/U16/U32/U64` | `integer` | Go emits unsigned via `Uint` field; still `integer` on wire |
| `Number` | `float` | `real` | IEEE 754 single |
| `Enum` / `Preset` | — | `enum` | pid=15 options lifted into `enumMap[]`; `enumeration` LF-joined for legacy consumers |
| `String` | — | `string` | pid=6 max_length exposed as `format: "maxLen=N"` |
| `IPv4` | — | `string` | Dotted-decimal IPv4 |
| `Node` | — | — | Rendered as `canonical.Node` (container); no Parameter |

Mode flags `--templates` / `--labels` / `--gain` accepted for CLI parity with Ember+ but have no effect (ACP2 has no constructs they apply to).

---

## Compliance Profile

Every wire tolerance gets a named counter. Run `acp profile --protocol acp2 <host>` after a walk to see the classification (strict / partial) and the per-event counts.

### Event catalog

| Event | Spec reference | Meaning |
|---|---|---|
| `acp2_an2_magic_mismatch` | an2_protocol.pdf §"Frame Header" | Framer received bytes not starting with `0xC635`; frame dropped, stream resyncs |
| `acp2_an2_short_payload` | an2_protocol.pdf §"Frame Header" | Payload shorter than `dlen` claimed; partial payload dropped |
| `acp2_announce_before_enable_events` | an2_protocol.pdf §"Protocol Events" | type=2 announce received before `EnableProtocolEvents([2])` was sent |
| `acp2_reply_zero_mtid` | acp2_protocol.pdf p.4 | Reply with mtid=0 (reserved for announces); dropped |
| `acp2_protocol_error_received` | acp2_protocol.pdf p.5 | stat=0 — provider-side parsing defect |
| `acp2_invalid_object_received` | acp2_protocol.pdf p.5 | stat=1 — obj-id not on device (stale tree) |
| `acp2_invalid_index_received` | acp2_protocol.pdf p.5 | stat=2 — preset idx out of declared range |
| `acp2_invalid_pid_received` | acp2_protocol.pdf p.5 | stat=3 — property id unknown on target object |
| `acp2_access_denied_received` | acp2_protocol.pdf p.5 | stat=4 — SetProperty on read-only |
| `acp2_invalid_value_received` | acp2_protocol.pdf p.5 | stat=5 — value out of range / enum / step |
| `acp2_property_length_overrun` | acp2_protocol.pdf p.6 | `plen` would read past message payload; parser stops |
| `acp2_unknown_property_id` | acp2_protocol.pdf p.6 | PID outside [1,20]; property skipped |
| `acp2_unknown_object_type` | acp2_protocol.pdf p.7 | pid=1 value outside {0..5}; falls back to `raw` |
| `acp2_unknown_number_type` | acp2_protocol.pdf p.7 | pid=5 / pid=8 vtype outside {0..11} |
| `acp2_preset_index_out_of_range` | acp2_protocol.pdf p.8 | idx not in the list declared by pid=7 |
| `acp2_active_index_echoed` | — | Provider echoed idx=0 on a reply when an explicit idx was requested |
| `acp2_announce_non_zero_mtid` | acp2_protocol.pdf p.9 | type=2 announce with mtid != 0 |
| `acp2_set_value_coerced` | — | SetProperty echo differs from sent value |

A session is classified **strict** if zero events fire, **partial** if any fire.

Today's ACP2 wiring fires all six stat-code events (`acp2_protocol_error_received` through `acp2_invalid_value_received`) on every error reply. The framing / property / type / announce events are defined and documented — each ships as the relevant decoder path is audited (see umbrella issue #36).

---

## Error Reference

Every error the consumer surfaces has a stable name, a source layer, and a recovery path.

### Transport layer (TCP + AN2)

| Name | When | Recovery |
|---|---|---|
| `TransportError{Op:"connect"}` | TCP dial failed | Check host + firewall (ACP2 requires port 2072 open) |
| `TransportError{Op:"send"}` | Frame write failed mid-session | Reconnect |
| `TransportError{Op:"receive"}` | Read error or socket closed | Reconnect |
| `AN2Error{Status:…}` | AN2 handshake returned an error payload | Check device supports the protocol version |
| `context deadline exceeded` | Per-op `--timeout` expired (single GetProperty / SetProperty) | Raise `--timeout`; walks use raw ctx and are unaffected |
| `acp2: connection closed while waiting for reply mtid=N` | Session reader exited during a pending request | Reconnect; in-flight mtid was not retried automatically |

### Protocol layer (ACP2 errors)

Wire errors carry `type=3` with a status code in the `func` slot. The consumer surfaces them as a single `*acp2.ACP2Error` whose `Status` field carries the code.

| Wire stat | Condition |
|---|---|
| 0 (`ErrProtocol`) | Protocol error — bad type/func/packet |
| 1 (`ErrInvalidObjID`) | obj-id does not exist on device |
| 2 (`ErrInvalidIdx`) | preset idx out of declared range |
| 3 (`ErrInvalidPID`) | object does not have this property |
| 4 (`ErrNoAccess`) | Read-only (SetProperty rejected) |
| 5 (`ErrInvalidValue`) | Value out of range / enum / step |

Every stat code also fires the corresponding `acp2_*_received` compliance event (see catalog above), so aggregate error frequency is observable without string-matching the error text.

### Addressing

| Name | When | Recovery |
|---|---|---|
| `acp2: label %q not found in tree slot %d` | Label resolution miss | Run `acp walk` first to populate the cache |
| `acp2: no tree cached for slot %d` | Path-addressed get/set before walk | Same |

### CLI exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Protocol error (ACP2 error reply received) |
| 2 | Validation / usage error |
| 3 | Transport error |
| 5 | Bad CLI flags |

---

## Identity

Identity fields vary by slot role:

### CTRL (slot 0) — Neuron controller

| Field | Path | ID | Label | Type |
|---|---|---|---|---|
| Card name | BOARD | 2 | Card Name | string |
| Firmware | BOARD | 13673 | Product Version | string |
| Serial number | BOARD | 8 | Serial Number | string |
| Hardware rev | BOARD | 6 | Hardware Version | string |
| Product code | BOARD | 7 | Product Code | string |
| Card ID | BOARD | 9 | Card ID | string |

Note: Serial Number belongs to the Neuron controller, not individual device cards.

### Device (slot 1+) — processing card

| Field | Path | ID | Label | Type |
|---|---|---|---|---|
| Card name | IDENTITY | 2 | Card Name | string |
| Firmware | IDENTITY | 13673 | Product Version | string |
| Description | IDENTITY | 4 | Card Description | string |
| Serial number | — | — | Not available | — |

---

## Object Types

| ID | Type | Description |
|---|---|---|
| 0 | node | Container — has children (pid 14). No scalar value |
| 1 | preset | Preset child — value repeated per idx |
| 2 | enum | Enumerated value (arbitrary u32 wire indices) |
| 3 | number | Typed numeric (S8/S16/S32/S64/U8/U16/U32/U64/float) |
| 4 | ipv4 | IP address (4 bytes) |
| 5 | string | UTF-8 null-terminated string |

### Number types (pid 5)

| ID | Type | Wire size |
|---|---|---|
| 0 | S8 | u32 |
| 1 | S16 | u32 |
| 2 | S32 | u32 |
| 3 | S64 | u64 |
| 4 | U8 | u32 |
| 5 | U16 | u32 |
| 6 | U32 | u32 |
| 7 | U64 | u64 |
| 8 | float | u32 (IEEE 754) |

---

## Tree Structure

ACP2 has a hierarchical object tree. Typical slot 0 (CONVERT Hybrid):

```
ROOT_NODE_V2
├── BOARD (identity, versions, config)
├── IO BOARD (SDI I/O)
├── PSU
│   ├── Fan Control
│   ├── 1 (Present, Status, Type, Fan Health 1-4)
│   ├── 2 (Present, Status, Type, Fan Health 1-4)
│   └── BOARD (Power, Temperature)
├── TEMPERATURE (USP, ZYNQ)
├── MANAGEMENT PORT (IP config, routes)
├── QSFP 1-4 (optical transceivers)
├── SFP 1-4 (optical transceivers)
└── SMARC (compute module)
```

- Slot 0: ~214 objects (~2 seconds walk)
- Slot 1: ~44k objects (~11 minutes walk)

---

## Discovery (Walk)

Walk = DFS from root via `get_object` + pid=14 (children).

```
1. get_object(obj_id=1)           → root node, children=[BOARD, IO BOARD, PSU, ...]
2. get_object(obj_id=15237)       → BOARD, children=[Card Name, Card ID, ...]
3. get_object(obj_id=2)           → Card Name, leaf (no children)
4. ... recursively for all children
```

Root obj-id: 1 on real devices, 0 on some firmware. Walker tries both.

### Streaming walk

Objects are printed as they're discovered (essential for slot 1 with 44k objects).
No waiting for full tree completion.

### Data model save

After walk: saved to `devices/{ip}/slot_{n}.json` (hierarchical format,
values stripped). Used for instant label resolution on next startup.

---

## Get / Set

| Function | ID | Direction | Description |
|---|---|---|---|
| get_version | 0 | request → reply | ACP2 protocol version |
| get_object | 1 | request → reply | All properties for obj-id |
| get_property | 2 | request → reply | Single property for (obj-id, pid) |
| set_property | 3 | request → reply | Set + returns confirmed property |

### Enum resolution

ACP2 enum wire indices are **arbitrary u32** (not 0-based). Resolved via
options map (pid=15):

```
wire 16 = "NA", wire 17 = "OK", wire 18 = "Error"
```

Set by label uses O(1) reverse map lookup: `"OK" → wire 17`.

### CLI examples

```
acp get 10.41.40.195 --protocol acp2 --slot 0 --id 15239
acp get 10.41.40.195 --protocol acp2 --slot 0 --label "Card Name"
acp set 10.41.40.195 --protocol acp2 --slot 0 --id 21127 --value "Full Speed"
acp set 10.41.40.195 --protocol acp2 --slot 0 --id 15246 --value "10.41.40.195"
```

---

## Subscriptions (Announcements)

- Transport: AN2 TCP, ACP2 type=2, mtid=0
- Requires `AN2 EnableProtocolEvents([2])` on connect (automatic)
- Announcements carry: slot, obj-id, pid, new value
- Terminology: "announce" (not "event"), "announce_delay" (not "event_delay")

### Watch with disk cache

```
acp watch 10.41.40.195 --protocol acp2 --slot 0
```

Flow:
1. Load labels + units from disk cache → instant display
2. Subscribe → announcements arrive with `[cache]` labels
3. Background walk → populates plugin tree
4. Walk done → switch to `[live]` (decoded values + enum labels)

Output:
```
loaded 190 labels from cache
17:01:04  s0  15212  Temperature  [live]  20 C
17:01:06  s0  47397  Power Rx 1   [live]  0.99 mW
17:01:10  s0  15219  Fan Speed    [live]  10 %
```

---

## Export / Import

Hierarchical tree format matching the device tree:

```yaml
BOARD:
  Card Name:
    id: 2
    kind: string
    access: R--
    value: SHPRM1
PSU:
  Fan Control:
    id: 21127
    kind: enum
    access: RW-
    enum_items: [Automatic, Full Speed]
    value_name: Automatic
  1:
    Present:
      id: 15239
      kind: enum
      access: R--
      value_name: OK
```

### CLI

```
acp export 10.41.40.195 --protocol acp2 --slot 0 --format yaml --out slot0.yaml
acp export 10.41.40.195 --protocol acp2 --slot 0 --format json --out slot0.json
acp export 10.41.40.195 --protocol acp2 --slot 0 --format csv  --out slot0.csv
acp export 10.41.40.195 --protocol acp2 --slot 0 --path BOARD --format yaml
acp import 10.41.40.195 --protocol acp2 --file slot0.yaml --dry-run
acp import 10.41.40.195 --protocol acp2 --file slot0.yaml
```

### CSV columns (lossless round-trip, issue #38)

ACP2 labels collide freely across sub-trees (e.g. `Present` lives
under `PSU.1` and `PSU.2` with different obj-ids). CSV round-trip
carries `id` — the globally-unique u32 obj-id — and uses it as the
importer's matching key. `oid` is empty for ACP2; `path` records the
tree location for human readability.

```
acp convert --in slot0.json --out slot0.csv
acp import  10.41.40.195 --protocol acp2 --file slot0.csv --dry-run   # applied N, failed 0
```

`failed 0` on an unchanged device confirms duplicate-label sub-nodes
round-trip unambiguously.

---

## Raw Capture

```
acp walk 10.41.40.195 --protocol acp2 --slot 0 --capture slot0_walk.jsonl
acp watch 10.41.40.195 --protocol acp2 --capture watch.jsonl
```

Format: JSONL, one line per AN2 frame:
```json
{"ts":"2026-04-16T20:55:07Z","proto":"acp2","dir":"tx","hex":"c63500000100000100","len":9}
{"ts":"2026-04-16T20:55:07Z","proto":"acp2","dir":"rx","hex":"c635000001010003000001","len":11}
```

---

## Error Codes

| Status | Error | Description |
|---|---|---|
| 0 | protocol error | Bad type/func/packet |
| 1 | invalid obj-id | Object does not exist |
| 2 | invalid idx | Preset index out of range |
| 3 | invalid pid | Property does not exist for this object |
| 4 | no access | Read-only, set attempted |
| 5 | invalid value | Enum/preset out of options |

---

## Test Device

| Device | IP | Protocol | Notes |
|---|---|---|---|
| Axon CONVERT Hybrid | 10.41.40.195 | ACP2/AN2/TCP | Real device, fw 5.3.5, 2 slots |
| Slot 0 (SHPRM1) | — | — | Neuron controller, 214 objects |
| Slot 1 (CONVERT) | — | — | Processing card, 44k+ objects |
