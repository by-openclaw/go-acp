# ACP2 Connector

Consumer connector for ACP v2 (Axon Neuron protocol) over AN2 transport.

---

## References

| Document | Path | Description |
|---|---|---|
| ACP2 spec (authoritative) | [assets/acp2/acp2_protocol.pdf](../../assets/acp2/acp2_protocol.pdf) | ACP v2 full specification |
| AN2 spec (authoritative) | [assets/acp2/an2_protocol.pdf](../../assets/acp2/an2_protocol.pdf) | AN2 transport specification |
| Wireshark dissector | [assets/acp2/dissector_acp2.lua](../../assets/acp2/dissector_acp2.lua) | AN2 + ACP2 byte-exact reference |
| Protocol reference | [CLAUDE.md](../../CLAUDE.md) — section "ACP2" | Wire format, functions, properties |
| Testdata captures | [testdata/acp2/](../../testdata/acp2/) | Raw JSONL captures from real device |
| Export fixtures | [testdata/exports/acp2/](../../testdata/exports/acp2/) | JSON/YAML/CSV per slot |
| Source code | [internal/protocol/acp2/](../../internal/protocol/acp2/) | Plugin implementation |
| Unit tests | [tests/unit/acp2/](../../tests/unit/acp2/) | Replay + spec tests |

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
