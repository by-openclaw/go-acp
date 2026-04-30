# ACP1 Connector

Consumer connector for ACP v1.4 (Axon Synapse protocol).

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [internal/acp1/assets/AXON-ACP_v1_4.pdf](../../../internal/acp1/assets/AXON-ACP_v1_4.pdf) | ACP v1.4 full specification |
| Wireshark dissector | [internal/acp1/wireshark/dhs_acpv1.lua](../../../internal/acp1/wireshark/dhs_acpv1.lua) | Byte-exact reference |
| C# reference driver | external (ByResearch.DHS.AxonACP.DeviceDriver) | ACP1 only, not ACP2 |
| Protocol reference | [CLAUDE.md](../../../CLAUDE.md) — section "ACP1" | Wire format, methods, object types |
| Testdata captures | [tests/fixtures/acp1/](../../../tests/fixtures/acp1/) | Raw JSONL captures from emulator |
| Export fixtures | [tests/fixtures/exports/acp1/](../../../tests/fixtures/exports/acp1/) | JSON/YAML/CSV per slot |
| Source code | [internal/protocol/acp1/](../../../internal/protocol/acp1/) | Plugin implementation |
| Unit tests | [tests/unit/acp1/](../../../tests/unit/acp1/) | Replay + spec tests |

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

## Capabilities & Compliance Status

| Capability | Spec page | Status | Notes |
|---|---|---|---|
| UDP direct (port 2071) | p.7 "ACP Port Number" | ✅ fully compliant | Subnet broadcast announcements via Listener |
| TCP direct with MLEN prefix (v1.4 addition) | p.7 | ✅ fully compliant | Multiplexes request/reply + announces; routes across VLANs |
| AN2 transport | — | ⛔ not applicable | AN2 is ACP2 only |
| ACP header decode (MTID/PVER/MTYPE/MADDR) | p.11 | ✅ fully compliant | Byte-exact per spec |
| Six method IDs (getValue/setValue/setInc/setDec/setDef/getObject) | p.28 | ✅ fully compliant | Unknown method IDs surface via `acp1_unknown_method` event |
| Eleven object types (Root/Integer/IPAddr/Float/Enum/String/Frame/Alarm/File/Long/Byte) | p.19–27 | ✅ fully compliant | All 11 types decoded; type `11` is reserved per v1.4 |
| Announcements (value-change + frame-status + card-event) | p.16 | ✅ fully compliant | UDP: dedicated Listener on port 2071. TCP: multiplexed with transactions |
| Retry / MTID rules | p.30 | ✅ fully compliant | Keep MTID on retransmit, increment on new request, never zero |
| Value freshness (live / updated / stale / cache) | — (our extension) | ⚠ partial | Walk cache has TTL; per-object freshness tags pending — covered in follow-up |
| Cascade on disconnect (root `isOnline y→n`) | — (our extension) | ⏳ pending | TCP disconnect detection exists; synthetic cascade event pending |
| Auto-reconnect goroutine | — (our extension) | ⏳ pending | TCP-only (UDP has no persistent session); pattern to lift from Ember+ |
| Compliance profile + `acp profile` CLI | — | ✅ fully compliant | Event catalogue in `internal/protocol/acp1/compliance_events.go`. Transport + object-error events wired; remainder fire in follow-up work |
| Canonical JSON export + `--capture <dir>` → `tree.json` | — (our schema) | ✅ fully compliant | Device→Slot→Group→Parameter mapping; no `glow.json` (ACP1 has no Glow layer) |
| Canonical export mode flags `--templates` / `--labels` / `--gain` | — | ⛔ not applicable | ACP1 has no `templateReference`, no `labels[]` SEQUENCE, no `parametersLocation`; flags pass through as no-ops |

Legend: ✅ fully compliant · ⚠ partial · ⛔ not applicable · ⏳ pending (on roadmap).

---

## Timeouts

All timeouts are deterministic, user-overridable via `--timeout`. No silent hangs.

| Timer | Default | Where | Override |
|---|---|---|---|
| Per-command operation (get / set / walk step / announce wait) | 30 s | `--timeout` global flag | `acp ... --timeout 10s` |
| Per-attempt receive timeout (UDP reply) | 10 s | `ClientConfig.ReceiveTimeout` spec p.30 | constant; tune via `ClientConfig` at plugin construction |
| Retry count per request (UDP) | 5 | `ClientConfig.MaxRetries` spec p.30 recommendation | same |
| Retry back-off | exponential | `ClientConfig.Backoff` | same |
| TCP connect | 30 s | `--timeout` (inherits) | `acp ... --timeout 60s` |
| Slot tree cache TTL | 10 min | `cacheConfig.TTL` | constant — re-walk forces refresh |
| Slot tree cache max entries | 32 | `cacheConfig.MaxSize` | constant |

**Rule:** retries use the same MTID per spec p.30 to let the device de-duplicate; new MTIDs are allocated only for brand-new requests. MTID never zero — wrap from 0xFFFFFFFF skips to 1.

---

## Canonical Export Modes

The `acp walk --capture <dir>` command writes `tree.json` in the canonical shape documented at [docs/protocols/schema.md](../schema.md). Device → Slot → Group → Parameter, four levels deep:

```
{
  "root": {
    "oid": "1", "identifier": "<host>",
    "children": [
      { "oid": "1.1", "identifier": "slot-0",
        "children": [
          { "oid": "1.1.1", "identifier": "identity",
            "children": [
              { "oid": "1.1.1.0", "identifier": "Card name", "type": "string", "value": "RRS18", ... }
            ]
          },
          { "oid": "1.1.2", "identifier": "control",  "children": [...] },
          { "oid": "1.1.3", "identifier": "status",   "children": [...] },
          { "oid": "1.1.4", "identifier": "alarm",    "children": [...] },
          { "oid": "1.1.5", "identifier": "file",     "children": [...] }
        ]
      }
    ]
  }
}
```

OIDs are synthetic — ACP1 has no wire-level RelOID. Scheme is `1.<slot+1>.<group_number>.<object_id>` where group_number is 1..5 for identity/control/status/alarm/file.

Per-kind → canonical type mapping:

| ACP1 kind | Canonical type | Notes |
|---|---|---|
| `Integer` / `Long` / `Byte` | `integer` | 16/32/8-bit widths collapsed |
| `Float` | `real` | IEEE 754 |
| `IPAddr` | `string` | Dotted-decimal IPv4 |
| `Enum` | `enum` | `enumMap[]` built from comma-delimited item_list; `enumeration` LF-joined for legacy consumers |
| `String` | `string` | `format: "maxLen=N"` hints the declared max length |
| `Alarm` | `boolean` | Active/idle; event on/off messages surface via `description` |
| `File` | `string` | File names; Fragment property never requested |
| `Frame` | — | Slot-status object accessed via `getValue` at slot 0; not mapped to Parameter |

Mode flags `--templates` / `--labels` / `--gain` accepted for CLI parity with Ember+ but have no effect (ACP1 has no constructs they apply to).

---

## Compliance Profile

Every wire tolerance gets a named counter. Run `acp profile <host> --protocol acp1` after a walk to see the classification (strict / partial) and the per-event counts.

### Event catalog

| Event | Meaning |
|---|---|
| `acp1_transport_error_received` | MTYPE=3 reply with MCODE<16 — device internal bus / timeout / out-of-resources (spec p.11) |
| `acp1_object_error_received` | MTYPE=3 reply with MCODE≥16 — unknown group / id / property / access / type (spec p.29) |
| `acp1_short_mdata` | Reply MDATA shorter than expected for its method |
| `acp1_unknown_method` | MCODE outside {0..5} on non-error reply (spec p.28) |
| `acp1_unknown_object_type` | getObject byte 0 outside {0..10} (spec p.19) |
| `acp1_string_missing_terminator` | String field missing NUL; truncated at spec max (Label 16, Unit 4, Alarm 32) |
| `acp1_enum_value_out_of_range` | Enum value >= num_items |
| `acp1_announce_non_zero_mtid` | MTYPE=0 announce with MTID != 0 (spec p.8) |
| `acp1_announce_slot_mismatch` | MADDR on announce does not match currently walked slot |
| `acp1_object_properties_truncated` | num_properties < spec count; missing fields zero-filled |
| `acp1_object_properties_extra` | num_properties > spec count; extras ignored |
| `acp1_set_value_coerced` | setValue echo differs from sent value by more than step size |

A session is classified **strict** if zero events fire, **partial** if any fire.

Today's ACP1 wiring fires `acp1_transport_error_received` and `acp1_object_error_received` on every error reply the Walker receives. The rest of the catalog is defined and documented — wire-integration for each event ships incrementally as the relevant decode path gets audited.

---

## Error Reference

Every error the consumer surfaces has a stable name, a source layer, and a recovery path.

### Transport layer (UDP / TCP)

| Name | When | Recovery |
|---|---|---|
| `TransportError{Op:"connect"}` | UDP/TCP dial failed | Check host + firewall |
| `TransportError{Op:"send"}` | Packet write failed mid-session | Reconnect |
| `TransportError{Op:"receive"}` | Read error or socket closed | Reconnect |
| `context deadline exceeded` | Per-attempt timeout (`ClientConfig.ReceiveTimeout`, default 10 s) | Raise `--timeout` or check device responsiveness |
| `acp1: max retries exceeded` (`ErrMaxRetries`) | 5 retries expired without a matching reply | Device offline / wrong slot / wrong group |

### Protocol layer (ACP1 errors)

Wire errors carry `MType=3` + MCODE. The consumer surfaces them as typed Go errors.

| Wire MCODE | Go type | Condition |
|---|---|---|
| 0..4 | `TransportErr` | Spec p.11 transport-level: undefined / bus-comm / bus-timeout / transaction-timeout / out-of-resources |
| 16 | `ObjectErr(OErrGroupNoExist)` | `getValue`/`getObject` on an unknown group |
| 17 | `ObjectErr(OErrInstanceNoExist)` | Group exists, id does not |
| 18 | `ObjectErr(OErrPropertyNoExist)` | Object exists, property does not |
| 19 | `ObjectErr(OErrNoWriteAccess)` | `setValue` on read-only object |
| 20 | `ObjectErr(OErrNoReadAccess)` | `getValue` on write-only object |
| 21 | `ObjectErr(OErrNoSetDefAccess)` | `setDefValue` on non-default object |
| 22 | `ObjectErr(OErrTypeNoExist)` | Object type field undefined |
| 23 | `ObjectErr(OErrIllegalMethod)` | Method not valid at all |
| 24 | `ObjectErr(OErrIllegalForType)` | Method not valid for this object type (spec Method Support Matrix p.28) |
| 32 | `ObjectErr(OErrFile)` | File subsystem error |
| 39 | `ObjectErr(OErrSPFConstraint)` | SPF (Stream Profile Format) constraint violated |
| 40 | `ObjectErr(OErrSPFBufferFull)` | SPF buffer full — retry the fragment later |

### Addressing

| Name | When | Recovery |
|---|---|---|
| `acp1: label %q not found in group %q` | Label resolution miss | Run `acp walk` first to refresh the cache |
| `acp1: no tree cached for slot N` | Cache miss on a path-addressed request | Same |

### CLI exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Protocol error (ACP1 error reply received) |
| 2 | Validation / usage error |
| 3 | Transport error |
| 5 | Bad CLI flags |

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

### CSV columns (lossless round-trip, issue #38)

CSV carries `oid`, `path`, `id`, `label` so the importer can match
every writable object. ACP1 populates `path` with the single-level
group name (e.g. `control`) and leaves `oid` empty — resolution uses
the ACP1-native `group + id` or `group + label` pair.

```
acp convert --in slot0.json --out slot0.csv
acp import  10.6.239.113 --file slot0.csv --dry-run    # applied N, failed 0
```

`failed 0` on an unchanged device confirms CSV round-trip is lossless
for writable objects.

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
