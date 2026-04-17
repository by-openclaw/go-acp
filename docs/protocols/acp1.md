# ACP1 Connector

Consumer connector for ACP v1.4 (Axon Synapse protocol).

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [assets/acp1/AXON-ACP_v1_4.pdf](../../assets/acp1/AXON-ACP_v1_4.pdf) | ACP v1.4 full specification |
| Wireshark dissector | [assets/acp1/dissector_acpv1.lua](../../assets/acp1/dissector_acpv1.lua) | Byte-exact reference |
| C# reference driver | external (ByResearch.DHS.AxonACP.DeviceDriver) | ACP1 only, not ACP2 |
| Protocol reference | [CLAUDE.md](../../CLAUDE.md) — section "ACP1" | Wire format, methods, object types |
| Testdata captures | [testdata/acp1/](../../testdata/acp1/) | Raw JSONL captures from emulator |
| Export fixtures | [testdata/exports/acp1/](../../testdata/exports/acp1/) | JSON/YAML/CSV per slot |
| Source code | [internal/protocol/acp1/](../../internal/protocol/acp1/) | Plugin implementation |
| Unit tests | [tests/unit/acp1/](../../tests/unit/acp1/) | Replay + spec tests |

---

## Transport

| Mode | Transport | Port | Description |
|---|---|---|---|
| Mode A | UDP direct | 2071 | Default. No length prefix. Each datagram = one message |
| Mode B | TCP direct | 2071 | MLEN (u32 BE) prefix before every message. Added in v1.4 |
| Mode C | AN2/TCP | 2072 | AN2 frame wraps ACP1 payload. AN2 dlen handles framing |

### Firewall rules

```
UDP 2071  inbound + outbound    (Mode A — default)
TCP 2071  outbound              (Mode B — TCP direct)
TCP 2072  outbound              (Mode C — AN2)
UDP 2071  inbound broadcast     (announcements + discovery)
```

### CLI transport selection

```
acp info 10.6.239.113                              # UDP (default)
acp info 10.6.239.113 --transport tcp              # TCP direct
```

---

## Identity

Read from `getObject(group=identity, id=0)` on any slot.

| Field | Group | ID | Label | Type |
|---|---|---|---|---|
| Card name | identity | 0 | Card name | string |
| Firmware | identity | 3 | Software rev | string |
| Serial number | identity | 6 | Serial number | string |
| Hardware rev | identity | 4 | Hardware rev | string |
| Product code | identity | 5 | Product code | string |
| Card ID | identity | 7 | Card ID | string |

---

## Object Groups

| ID | Group | Description | Objects |
|---|---|---|---|
| 0 | root | Card counts per group | 1 (boot mode + counts) |
| 1 | identity | Label, description, serial, sw/hw rev | 8 |
| 2 | control | Writable parameters | variable |
| 3 | status | Read-only status values | variable |
| 4 | alarm | Alarm objects with priority + tag | variable |
| 5 | file | Firmware + param table | variable |
| 6 | frame | Slot status array (slot 0 only) | 1 |

---

## Object Types

| ID | Type | Properties | Value field |
|---|---|---|---|
| 0 | Root | 9 | boot_mode (byte) |
| 1 | Integer | 10 | int16 |
| 2 | IP Address | 10 | uint32 (4 bytes) |
| 3 | Float | 10 | float32 (IEEE 754) |
| 4 | Enumerated | 8 | byte index into item list |
| 5 | String | 6 | null-terminated ASCII |
| 6 | Frame Status | 4 | slot status array |
| 7 | Alarm | 8 | priority byte |
| 8 | File | 5 | fragment count |
| 9 | Long | 10 | int32 |
| 10 | Byte | 10 | uint8 |

---

## Discovery (Walk)

Walk = `getObject` per object in each group. Root object provides counts.

```
1. getObject(root, 0)        → num_identity, num_control, num_status, num_alarm
2. getObject(identity, 0..N) → label, type, value, constraints
3. getObject(control, 0..N)
4. getObject(status, 0..N)
5. getObject(alarm, 0..N)
```

Typical slot 0 (rack controller): ~60 objects, < 1 second.

### Data model save

After walk: saved to `devices/{ip}/slot_{n}.json` (hierarchical format,
values stripped). Used for instant label resolution on next startup.

---

## Get / Set

| Method | ID | Direction | Description |
|---|---|---|---|
| getValue | 0 | request → reply | Read current value |
| setValue | 1 | request → reply | Write value, returns confirmed |
| setIncValue | 2 | request → reply | Increment, returns new value |
| setDecValue | 3 | request → reply | Decrement, returns new value |
| setDefValue | 4 | request → reply | Reset to default, returns default |
| getObject | 5 | request → reply | All properties in sequence |

### CLI examples

```
acp get 10.6.239.113 --slot 0 --id 4 --group control       # by ID
acp get 10.6.239.113 --slot 0 --label "Broadcasts"          # by label
acp set 10.6.239.113 --slot 0 --label "Broadcasts" --value "On"
acp set 10.6.239.113 --slot 0 --id 4 --group control --value "Off"
```

---

## Subscriptions (Announcements)

- Transport: UDP broadcast, MTID=0
- Requires `Broadcasts` param = `On` on rack controller (slot 0, control group)
- If `Broadcasts` = `Off`, no value-change announcements from the device
- Announcements carry: slot, group, object ID, new value

### CLI

```
acp watch 10.6.239.113 --slot 0
acp watch 10.6.239.113 --filter Temperature
```

---

## Export / Import

Hierarchical tree format matching the group structure:

```yaml
identity:
  Card name:
    id: 0
    kind: string
    access: R--
    value: RRS18
  Serial number:
    id: 6
    kind: string
    access: R--
    value: "001633"
control:
  Broadcasts:
    id: 4
    kind: enum
    access: RW-
    enum_items: [Off, On]
    value_name: "On"
```

### CLI

```
acp export 10.6.239.113 --slot 0 --format yaml --out slot0.yaml
acp export 10.6.239.113 --slot 0 --format json --out slot0.json
acp export 10.6.239.113 --slot 0 --format csv  --out slot0.csv
acp import 10.6.239.113 --file slot0.yaml --dry-run
acp import 10.6.239.113 --file slot0.yaml              # apply
```

---

## Raw Capture

```
acp walk 10.6.239.113 --slot 0 --capture slot0_walk.jsonl
acp watch 10.6.239.113 --capture watch.jsonl
```

Format: JSONL, one line per wire message:
```json
{"ts":"2026-04-16T20:57:56Z","proto":"acp1","dir":"tx","hex":"39d29002010100050000","len":10}
{"ts":"2026-04-16T20:57:56Z","proto":"acp1","dir":"rx","hex":"39d2900201020005000000090100080e1f0620","len":19}
```

Used for: unit test replay, protocol analysis, debugging.

---

## Test Device

| Device | IP | Protocol | Notes |
|---|---|---|---|
| Synapse Simulator | 10.6.239.113 | ACP1/UDP | Emulator, 31 slots, 4 present |
