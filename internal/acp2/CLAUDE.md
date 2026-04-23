# CLAUDE.md — ACP2 (AxonNet Control Protocol v2, over AN2)

Atomic per-protocol context for the ACP2 plugin. Read the root `CLAUDE.md`
first for cross-cutting rules; this file holds the ACP2 + AN2 wire spec.

Authoritative specs:
- `internal/acp2/assets/acp2_protocol.pdf`
- `internal/acp2/assets/an2_protocol.pdf` (AN2 transport)

Wireshark dissector (byte-exact reference): `./wireshark/dissector_acp2.lua`.

Viewer under test: Lawo VSM Axon Neuron driver. Spec-strict, no workarounds.

---

## Folder layout (this package)

```
internal/acp2/
├── CLAUDE.md    ← this file
├── consumer/    package acp2 — implements protocol.Protocol
├── provider/    package acp2 — implements provider.Provider
├── wireshark/   dissector_acp2.lua
├── docs/        consumer.md / provider.md / README.md
└── assets/      acp2_protocol.pdf + an2_protocol.pdf + demo_device.json
```

---

## Transport

ACP2 runs **exclusively over AN2/TCP** (port 2072, AN2 proto=2).
No UDP, no direct TCP. AN2 is always required.

AN2 must be initialized before any ACP2 traffic:

```
TCP :2072
→ AN2 GetVersion              (proto=0)
→ AN2 GetDeviceInfo           (proto=0)   slot count
→ AN2 GetSlotInfo(n)          (proto=0)   per slot
→ AN2 EnableProtocolEvents([2])(proto=0)   REQUIRED for ACP2 announces
→ ACP2 GetVersion              (proto=2)
→ ACP2 GetObject(slot, 0)      (proto=2)  walk from root
```

## AN2 frame header (8 bytes, big-endian)

```
magic  u16  0xC635 — validate on every receive
proto  u8   0=AN2-internal, 1=ACP1, 2=ACP2, 3=ACMP
slot   u8   0-254=endpoint, 255=broadcast
mtid   u8   0=data/events, 1-255=request/reply correlation
type   u8   0=request, 1=reply, 2=event, 3=error, 4=data
dlen   u16  payload byte count (does NOT include this 8-byte header)
payload    dlen bytes
```

AN2 mtid is SEPARATE from ACP2 mtid. AN2 data frames (type=4) always have
AN2 mtid=0. ACP2 has its own mtid for req/reply correlation.

## ACP2 message header (4 bytes, big-endian)

Carried inside an AN2 data frame (proto=2, type=4):

```
byte 0  type  u8  0=request, 1=reply, 2=announce, 3=error
byte 1  mtid  u8  0=announces/events, 1-255=request/reply
byte 2  func  u8  (request/reply) or stat (error)
byte 3  pid   u8  multipurpose: pid, padding, or version
```

For funcs 1-3, body is:
```
bytes 4-7    obj-id   u32
bytes 8-11   idx      u32   0 = ACTIVE INDEX
bytes 12+    property headers (variable)
```

## Functions

| ID | Name          | Body                                  |
|---:|---------------|---------------------------------------|
|  0 | get_version   | reply byte 3 = version                |
|  1 | get_object    | all property headers for obj-id       |
|  2 | get_property  | single property for (obj-id, pid, idx)|
|  3 | set_property  | set + return confirmed property       |

## Object types

```
0 node     container — has children (pid 14)
1 preset   preset child — value repeated per idx
2 enum     enumerated value
3 number   typed numeric
4 ipv4     IP address
5 string   UTF-8 string
```

## Error stat codes

```
0 protocol-error (bad type/func/packet)
1 invalid-obj-id
2 invalid-idx
3 invalid-pid (or object lacks this property)
4 no-access (read-only, set attempted)
5 invalid-value (enum/preset out of options, etc.)
```

## Property header (4 bytes + data)

```
byte 0    pid   u8   property id (1-20)
byte 1    data  u8   vtype, or inline small value, or padding
bytes 2-3 plen  u16  total bytes including this 4-byte header,
                     EXCLUDING alignment padding
bytes 4+  value      varies
[padding] 0-3 bytes to align next property to 4-byte boundary
```

Alignment: after each property, skip `(4 - (plen % 4)) % 4` bytes.
`plen` does NOT include padding. Read plen, then advance `plen + padding`.

## Property IDs

| pid | name              | access   | dyn | notes                     |
|----:|-------------------|----------|-----|---------------------------|
|   1 | object_type       | R        |  -  |                           |
|   2 | label             | R        |  -  | 0-terminated UTF-8        |
|   3 | access            | R        |  Y  | 1=r, 2=w, 3=rw            |
|   4 | announce_delay    | RW       |  Y  | u32 ms (NOT "event_delay")|
|   5 | number_type       | R        |  -  |                           |
|   6 | string_max_length | R        |  -  | u16                       |
|   7 | preset_depth      | R        |  -  | optional; valid idx list  |
|   8 | value[depth]      | R/RW     |  Y  | repeated per preset idx   |
|   9 | default_value[d]  | R        |  -  | repeated per preset idx   |
|  10 | min_value[d]      | R        |  Y  | repeated per preset idx   |
|  11 | max_value[d]      | R        |  Y  | repeated per preset idx   |
|  12 | step_size         | R        |  -  | optional                  |
|  13 | unit              | R        |  -  | optional, 0-terminated    |
|  14 | children          | R        |  -  | u32[] child obj-ids       |
|  15 | options           | R        |  -  | enum: 72 bytes per option |
|  16 | event_tag         | R        |  -  | optional, u16             |
|  17 | event_prio        | RW       |  Y  | optional                  |
|  18 | event_state       | R        |  Y  | optional                  |
|  19 | event_messages    | R        |  -  | optional, two strings     |
|  20 | preset_parent     | R        |  -  | optional, u32             |

## Number types (pid 5 and vtype in pid 8-12)

```
0=S8   1=S16   2=S32   3=S64
4=U8   5=U16   6=U32   7=U64
8=float   9=preset/enum   10=ipv4   11=string
```

## Wire sizes of property values

```
u8/u16/u32/s8/s16/s32/float   stored as u32/s32   plen=8
u64/s64                        stored as u64/s64   plen=12
preset/enum                    u32                 plen=8
ipv4                           4×u8                plen=8
string                         bytes+\0            plen=4+len+1 + pad
```

## Preset depth

When an object is a preset child:
- pid 7 (preset_depth) lists all valid idx values.
- pids 8, 9, 10, 11 appear ONCE PER PRESET IDX in get_object reply.
- idx=0 is ACTIVE INDEX — device substitutes the actual active idx.
- NEVER use idx=0 to mean "first preset slot".

## Announces (type=2)

```
ACP2 header:  type=2, mtid=0, stat=0, pid=<property id of changed value>
Body:         obj-id(u32), idx(u32), property header for pid
```

Only received after `AN2 EnableProtocolEvents([2])`. AN2 slot events
(proto=0) are always received regardless.

## Key invariants

- AN2 mtid is always 0 for data frames.
- ACP2 mtid pool: 1-255; 0 reserved for announces.
- mtid NEVER reused while a request is in-flight.
- EnableProtocolEvents MUST be called after every (re)connect.
- pid=4 is "announce_delay" — NEVER call it "event_delay".
- type=2 is "announce" — NEVER call it "event".
- ACP2 strings are UTF-8; ACP1 strings are ASCII.
- All multi-byte values are big-endian.

## What NOT to do

- Do NOT use fixed byte offsets for property data — use pid/plen headers.
- Do NOT skip EnableProtocolEvents before expecting announces.
- Do NOT reuse a live mtid.
- Do NOT call pid=4 "event_delay".
- Do NOT use idx=0 to mean "first preset".
