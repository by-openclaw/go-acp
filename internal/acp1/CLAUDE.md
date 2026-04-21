# CLAUDE.md — ACP1 (AxonNet Control Protocol v1.4)

Atomic per-protocol context for the ACP1 plugin. Read the root `CLAUDE.md`
first for cross-cutting rules; this file holds the ACP1-specific wire spec.

Authoritative spec: `assets/acp1/AXON-ACP_v1_4.pdf`.
Wireshark dissector (byte-exact reference): `./wireshark/dissector_acp1.lua`.
C# reference (Mode A only): `ByResearch.DHS.AxonACP.DeviceDriver`. C# does NOT
implement ACP2.

When any codec question arises: **spec first, dissector second, C# third**.

---

## Folder layout (this package)

```
internal/acp1/
├── CLAUDE.md    ← this file
├── consumer/    package acp1 — implements protocol.Protocol
├── provider/    package acp1 — implements provider.Provider
└── wireshark/   dissector_acp1.lua
```

---

## Transport modes

```
Mode A — UDP direct     (port 2071)  — one datagram = one ACP1 message
Mode B — TCP direct     (port 2071)  — MLEN(u32 big-endian) prefix, MLEN > 8
Mode C — AN2/TCP        (port 2072, AN2 proto=1) — AN2 dlen frames; no MLEN
```

## Wire header (7 bytes, big-endian)

```
offset  0-3   MTID    u32   0 = broadcast/announcement
offset  4     PVER    u8    1 = ACP1
offset  5     MTYPE   u8    0=announce, 1=request, 2=reply, 3=error
offset  6     MADDR   u8    slot 0-31 (0 = rack controller)

MDATA = up to 134 bytes (AxonNet payload):
  MDATA[0]   MCODE    u8    method ID (MTYPE<3) or error code (MTYPE=3)
  MDATA[1]   ObjGrp   u8    0=root,1=identity,2=control,3=status,
                            4=alarm,5=file,6=frame
  MDATA[2]   ObjId    u8    object index within group
  MDATA[3..] Value           ≤131 bytes
```

Total ≤ 141 bytes. Error replies (MTYPE=3) may omit ObjGrp/ObjId.
TCP direct prepends a 4-byte MLEN (u32 big-endian, counts from MTID onward).

## MCODE semantics

```
MTYPE < 3          → MCODE = method ID
MTYPE = 3, MCODE < 16  → ACP transport error
MTYPE = 3, MCODE ≥ 16  → AxonNet object error
```

## Methods (v1.4 has exactly 6)

| ID | Name         | Args         | Returns                   |
|---:|--------------|--------------|---------------------------|
|  0 | getValue     | —            | object value bytes        |
|  1 | setValue     | value bytes  | confirmed value bytes     |
|  2 | setIncValue  | —            | new value bytes           |
|  3 | setDecValue  | —            | new value bytes           |
|  4 | setDefValue  | —            | default value bytes       |
|  5 | getObject    | —            | all properties in sequence|

## Method support matrix

```
              Root Int Long Float Byte IPAddr Enum String Alarm File Frame
getValue       ✓   ✓                ✓          ✓    ✓     ✓     ✓    ✓
setValue           ✓                ✓          ✓    ✓           ✓
setIncValue        ✓                ✓          ✓
setDecValue        ✓                ✓          ✓
setDefValue        ✓                ✓          ✓    ✓
getObject      ✓   ✓                ✓          ✓    ✓     ✓     ✓    ✓
```

Float and IPAddr follow the Int/Long pattern.

## Object groups

```
0 Root     — 1 object; card counts per group
1 Identity — label, user-label, desc, sw/hw-rev, code, serial, card-id
2 Control  — writable parameters (count from Root[5])
3 Status   — read-only status values  (count from Root[6])
4 Alarm    — alarm objects            (count from Root[7])
5 File     — firmware + param table   (count from Root[8])
6 Frame    — 1 object; slot status array (rack controller only)
```

## Object types (big-endian; strings null-terminated)

```
ROOT (type=0, 9 props):
  access, boot_mode, num_identity, num_control, num_status, num_alarm, num_file

INTEGER (type=1, 10 props):
  access, value(i16), default_value(i16), step_size(i16), min(i16), max(i16),
  label(max 16+\0), unit(max 4+\0)

IPADDR (type=2, 10 props):  same as INTEGER but u32 numerics
FLOAT  (type=3, 10 props):  same as INTEGER but float32 numerics

ENUMERATED (type=4, 8 props):
  access, value(u8), num_items(u8), default_value(u8),
  label(max 16+\0), item_list("Off,On,Auto\0")

STRING (type=5, 6 props):
  access, value[max_len+\0], max_len(u8), label(max 16+\0)

FRAME STATUS (type=6, 4 props, read-only):
  access=1, num_slots(u8), slot_status[num_slots]
    (0=no-card, 1=powerup, 2=present, 3=error, 4=removed, 5=boot)

ALARM (type=7, 8 props):
  access, priority(u8; 0=disabled), tag(u8),
  label(max 16+\0), event_on_msg(max 32+\0), event_off_msg(max 32+\0)

FILE (type=8, 5 props):   access, num_fragments(i16), file_name(max 16+\0)
  — Fragment property NOT returned by getObject

LONG (type=9, 10 props):  same as INTEGER but i32
BYTE (type=10, 10 props): same as INTEGER but u8
```

## Access byte bits

```
bit 0 = read, bit 1 = write, bit 2 = setDef;  bits 3-7 unused
```

## ACP transport errors (MCODE < 16)

```
0=undefined  1=internal-bus-comm  2=internal-bus-timeout
3=transaction-timeout  4=out-of-resources
```

## AxonNet object errors (MCODE ≥ 16)

```
16 object-group-not-exist   17 object-instance-not-exist
18 object-property-not-exist  19 no-write-access
20 no-read-access             21 no-setDefault-access
22 object-type-not-exist      23 illegal-method
24 illegal-method-for-type    32 file-error
39 SPF-constraint-violation   40 SPF-buffer-full (retry)
```

## Announcements

- MTID = 0 (first 4 bytes zero).
- MTYPE = 0 (status / card-event / frame-status change; MCODE=0)
  or 2 (value-change echo; MCODE ∈ {1,2,3,4}).
- Dispatch by ObjGrp — not by MTYPE.
  - Frame status:  ObjGrp=6, ObjId=0, MDATA[0]=num_slots, MDATA[1..]=per-slot
  - Value change:  ObjGrp/ObjId = changed object; MADDR = source slot;
                   MDATA = new value bytes

## Retry / timeout (from spec)

- Keep MTID on retransmit; increment only for a new request.
- MTID is never 0 (wrap skips 0 → 1).
- On SET timeout: GET first to confirm before retry.
- On GET timeout: retransmit with the same MTID.
- Exponential backoff. Default: 5 retries, 10s timeout.

## Recommended practice

Use the **Label string**, not ObjId, as the stable unique identifier.
ObjIds change across firmware revisions; labels are unique within a group.
Build `map[group]map[label](group, id)` on first walk; translate at send-time.

## What NOT to do

- Do NOT call pid=4 "event_delay" — that is ACP2; irrelevant here.
- Do NOT use the `File` Fragment property outside engineer mode; real
  Synapse rejects it.
- Do NOT reuse MTIDs while in-flight.
- Do NOT treat MTID=0 as a valid request ID.
