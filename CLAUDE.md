# CLAUDE.md — acp

Read this file completely before touching any code.
This is the single authoritative context for Claude Code working in this repository.

---

## Project Purpose

A Go toolset to connect to, monitor, and control devices that speak the **ACP family
of protocols** over UDP or TCP (via AN2 transport). Two binaries share one internal
library:

```
cmd/acp          CLI — direct device I/O, no server
cmd/acp-srv      HTTP REST + WebSocket API (consumed by acp-ui)
internal/        core library — imported only by cmd/
```

A separate repository `acp-ui` (React 19) consumes `cmd/acp-srv`.
This repo has zero frontend code.

---

## Protocol Reference Documents

All protocol documents live in `assets/` per protocol. Read them before modifying
any codec or framer code.

```
assets/
  acp1/
    AXON-ACP_v1_4.pdf        ACP v1 full specification (authoritative)
    dissector_acpv1.lua       Wireshark dissector for ACP1 (byte-exact reference)
  acp2/
    acp2_protocol.pdf         ACP v2 full specification (authoritative)
    an2_protocol.pdf          AN2 (Axonnet2) transport specification (authoritative)
    dissector_acp2.lua        Wireshark dissector for ACP2 (byte-exact reference)
```

When any codec question arises: **spec first, dissector second, C# reference third**.
The C# source (ByResearch.DHS.AxonACP.DeviceDriver) is an ACP1 reference
implementation. It does not implement ACP2.

---

## Protocol Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  IProtocol (interface)                                          │
│  All code above this line is protocol-agnostic                  │
├────────────────────────┬────────────────────────────────────────┤
│  ACP1 plugin           │  ACP2 plugin                           │
│  internal/protocol/    │  internal/protocol/                    │
│  acp1/                 │  acp2/                                 │
├────────────────────────┴────────────────────────────────────────┤
│  Transport layer                                                │
│  internal/transport/                                            │
│  udp.go   tcp.go   an2_framer.go                               │
└─────────────────────────────────────────────────────────────────┘
```

New protocols are added by creating `internal/protocol/{name}/` and registering
via `init()`. Nothing else changes. See `internal/protocol/_template/`.

---

## ACP1 — Full Protocol Reference

### Transport Modes

```
Mode A — UDP direct (port 2071)
  No length prefix. Each UDP datagram = one ACP1 message.
  C# reference implements this mode.

Mode B — TCP direct (port 2071)
  MLEN(u32 big-endian) prefix before every message.
  MLEN = byte count starting at MTID (spec: MLEN > 8).
  Added in ACP v1.4.

Mode C — AN2/TCP (port 2072, AN2 proto=1)
  AN2 frame wraps ACP1 payload. AN2 dlen handles framing.
  No MLEN in the ACP1 payload itself.
  Wireshark dissector implements this mode.
```

### Wire Header

```
ACP header = 7 bytes fixed (UDP direct / AN2):
  offset  0-3   MTID    u32 big-endian   0 = broadcast/announcement
  offset  4     PVER    u8               1 = ACP1
  offset  5     MTYPE   u8               0=announce, 1=request, 2=reply, 3=error
  offset  6     MADDR   u8               slot 0-31 (0=rack controller)

MDATA = up to 134 bytes, carries the AxonNet payload:
  MDATA[0]      MCODE   u8               method ID (MTYPE<3) or error code (MTYPE=3)
  MDATA[1]      ObjGrp  u8               0=root,1=identity,2=control,
                                         3=status,4=alarm,5=file,6=frame
  MDATA[2]      ObjId   u8               object index within group
  MDATA[3..]    Value   ≤131 bytes       method args / return value

Total packet ≤ 141 bytes (7 header + 134 MDATA).
Error replies (MTYPE=3) may omit ObjGrp/ObjId; MDATA at minimum is just MCODE.

TCP direct only — 4-byte MLEN prefix before the 7-byte header:
  MLEN    u32 big-endian   byte count starting at MTID (spec: MLEN > 8)
  then the normal 7-byte header + MDATA
```

### MCODE Semantics

```
MTYPE < 3  → MCODE = method ID
MTYPE = 3 and MCODE < 16   → ACP transport error
MTYPE = 3 and MCODE >= 16  → AxonNet object error
```

### Methods

Spec v1.4 defines exactly six methods (IDs 0–5). No other method IDs exist.

```
ID   Name            Arguments    Returns
0    getValue        —            object value bytes
1    setValue        value bytes  confirmed value bytes
2    setIncValue     —            new value bytes
3    setDecValue     —            new value bytes
4    setDefValue     —            default value bytes
5    getObject       —            all property bytes in sequence
```

### Method Support Matrix

```
           Root  Int  Long  Float  Byte  IPAddr  Enum  String  Alarm  File  Frame
getValue    ✓     ✓                  ✓           ✓     ✓       ✓      ✓     ✓
setValue          ✓                  ✓           ✓     ✓              ✓
setIncValue       ✓                  ✓           ✓
setDecValue       ✓                  ✓           ✓
setDefValue       ✓                  ✓           ✓     ✓
getObject   ✓     ✓                  ✓           ✓     ✓       ✓      ✓     ✓
```

Float and IPAddr follow the same pattern as Int/Long.

### Object Groups

```
ID   Name       Contents
0    Root       1 object — card counts per group
1    Identity   label, user-label, desc, sw-rev, hw-rev, code, serial, card-id
2    Control    writable parameters (count from Root[5])
3    Status     read-only status values (count from Root[6])
4    Alarm      alarm objects (count from Root[7])
5    File       firmware + param table (count from Root[8])
6    Frame      1 object — slot status array (rack controller only)
```

### Object Types and Property Layouts

All values MSB first (big-endian). Strings null-terminated.
Max object payload: 131 bytes. Max total ACP message: 141 bytes.

```
ROOT (type=0, 9 properties):
  byte  object_type=0
  byte  num_properties=9
  byte  access
  byte  boot_mode          ← getValue returns this
  byte  num_identity
  byte  num_control
  byte  num_status
  byte  num_alarm
  byte  num_file

INTEGER (type=1, 10 properties):
  byte   object_type=1
  byte   num_properties=10
  byte   access
  int16  value             ← getValue/setValue
  int16  default_value
  int16  step_size
  int16  min_value
  int16  max_value
  string label             (max 16 + \0)
  string unit              (max 4  + \0)

IP ADDRESS (type=2, 10 properties):
  same as INTEGER but all numeric fields are uint32

FLOAT (type=3, 10 properties):
  same as INTEGER but all numeric fields are float32 (IEEE 754)

ENUMERATED (type=4, 8 properties):
  byte   object_type=4
  byte   num_properties=8
  byte   access
  byte   value             ← index into item list
  byte   num_items
  byte   default_value
  string label             (max 16 + \0)
  string item_list         comma-delimited, null-terminated
                           e.g. "Off,On,Auto\0"

STRING (type=5, 6 properties):
  byte   object_type=5
  byte   num_properties=6
  byte   access
  string value             [MaxLen + \0]
  byte   max_len
  string label             (max 16 + \0)

FRAME STATUS (type=6, 4 properties, read-only):
  byte   object_type=6
  byte   num_properties=4
  byte   access=1
  byte[] num_slots + slot_status_array
         byte[0] = num_slots
         byte[1..N] = status per slot
         0=no card, 1=powerup, 2=present, 3=error, 4=removed, 5=boot

ALARM (type=7, 8 properties):
  byte   object_type=7
  byte   num_properties=8
  byte   access
  byte   priority          0=disabled
  byte   tag               fixed value from Axon
  string label             (max 16 + \0)
  string event_on_msg      (max 32 + \0)
  string event_off_msg     (max 32 + \0)  immediately after event_on

FILE (type=8, 5 properties):
  byte   object_type=8
  byte   num_properties=5
  byte   access
  int16  num_fragments
  string file_name         (max 16 + \0)
  NOTE: Fragment property NOT returned by getObject

LONG (type=9, 10 properties):
  same as INTEGER but all numeric fields are int32

BYTE (type=10, 10 properties):
  same as INTEGER but all numeric fields are uint8
```

### Access Byte Bits

```
bit 0   read access      (1=yes)
bit 1   write access     (1=yes)
bit 2   setDef access    (1=yes)
bits 3-7  not used
```

### ACP Transport Errors (MCODE < 16)

```
0   undefined
1   internal bus communication error
2   internal bus timeout
3   transaction timeout
4   out of resources
```

### AxonNet Object Errors (MCODE >= 16)

```
16  object group does not exist
17  object instance does not exist
18  object property does not exist
19  no write access
20  no read access
21  no setDefault access
22  object type does not exist
23  illegal method
24  illegal method for this object type
32  file error
39  SPF file constraint violation
40  SPF buffer full — retry fragment later
```

### Announcements

Identified by MTID=0 (first 4 bytes = 0x00000000).
MTYPE may be 0 (status / card-event / frame-status change; MCODE=0) or
2 (value-change echo of a set-family method; MCODE ∈ {1,2,3,4}).
Dispatch by ObjGrp, not by MTYPE.

```
Frame status announcement:
  ObjGrp=6, ObjId=0
  MDATA[0] = num_slots
  MDATA[1..N] = slot status per slot

Value change announcement:
  ObjGrp = group of changed object
  ObjId  = id of changed object
  MADDR  = source slot
  MDATA  = new value bytes
```

### Retry / Timeout Rules (from spec)

```
- Keep same MTID when retransmitting (do NOT increment)
- Increment MTID for every new request
- MTID must never be zero (if increment wraps to 0, skip to 1)
- On timeout of SET: do a GET first to confirm before retrying
- On timeout of GET: retransmit with same MTID
- Recommended: exponential backoff to avoid congestion
- Default: 5 retries, 10s receive timeout
```

### Recommended Practice (from spec appendix)

**Use Label String, not ObjId, as the unique identifier.**
Object IDs can change between firmware revisions.
Label Strings are unique within a group and stable.

Walker must build: `map[group]map[label](group, id)` on first walk.
All CLI/API operations accept labels; translate to (group, id) at send time.

---

## ACP2 — Full Protocol Reference

### Transport

ACP2 runs **exclusively over AN2/TCP** (port 2072, AN2 proto=2).
There is no direct UDP or TCP variant. AN2 is always required.

AN2 must be initialized before any ACP2 traffic:

```
connect TCP :2072
→ AN2 GetVersion      (proto=0)
→ AN2 GetDeviceInfo   (proto=0)  → slot count
→ AN2 GetSlotInfo(n)  (proto=0)  → per slot
→ AN2 EnableProtocolEvents([2])  (proto=0)  ← REQUIRED for ACP2 announces
→ ACP2 GetVersion     (proto=2)
→ ACP2 GetObject(slot, 0)        (proto=2)  → walk from root
```

### AN2 Frame Header (8 bytes, big-endian)

```
magic   u16   0xC635 — validate on every receive
proto   u8    0=AN2 internal, 1=ACP1, 2=ACP2, 3=ACMP
slot    u8    0-254=endpoint, 255=broadcast
mtid    u8    0=data/events, 1-255=request/reply correlation
type    u8    0=request, 1=reply, 2=event, 3=error, 4=data
dlen    u16   payload byte count (not including this 8-byte header)
payload []    dlen bytes
```

AN2 mtid is SEPARATE from ACP2 mtid.
AN2 data frames (type=4) always have AN2 mtid=0.
ACP2 has its own mtid (u8) inside the payload for req/reply correlation.

### ACP2 Message Header (4 bytes, big-endian)

Carried inside AN2 data frame payload (AN2 proto=2, AN2 type=4):

```
byte 0   type    u8    0=request, 1=reply, 2=announce, 3=error
byte 1   mtid    u8    0=announces/events, 1-255=request/reply
byte 2   func    u8    (request/reply) or stat (error)
byte 3   pid     u8    (multipurpose: pid, padding, or version)
```

Followed by request/reply body (for funcs 1-3):
```
bytes 4-7    obj-id   u32 big-endian
bytes 8-11   idx      u32 big-endian   0 = ACTIVE INDEX
bytes 12+    property headers (variable)
```

### ACP2 Functions

```
0   get_version    → reply byte 3 = version number
1   get_object     → all property headers for obj-id
2   get_property   → single property for (obj-id, pid, idx)
3   set_property   → set + returns confirmed property
```

### ACP2 Object Types

```
0   node      container — has children (pid 14)
1   preset    preset child — value repeated per idx
2   enum      enumerated value
3   number    typed numeric
4   ipv4      IP address
5   string    UTF-8 string
```

### ACP2 Error Status Codes

```
0   protocol error    (bad type/func/packet)
1   invalid obj-id
2   invalid idx
3   invalid pid (or object does not have this property)
4   no access (read-only, set attempted)
5   invalid value (enum/preset out of options, etc.)
```

### ACP2 Property Header (4 bytes + data)

```
byte 0       pid      u8    property id (1-20)
byte 1       data     u8    vtype, or inline small value, or padding
bytes 2-3    plen     u16   total bytes including this 4-byte header,
                            EXCLUDING alignment padding
bytes 4+     value    varies
[padding]    0-3 bytes to align next property to 4-byte boundary
```

**Alignment rule**: after each property, skip `(4 - (plen % 4)) % 4` bytes.
`plen` itself does NOT include padding. Read `plen`, then advance
`plen + padding` bytes total.

### ACP2 Property IDs

```
pid   name               access   dynamic   notes
1     object_type        R        no
2     label              R        no        0-terminated UTF-8
3     access             R        yes       1=r, 2=w, 3=rw
4     announce_delay     RW       yes       u32 ms  ← NOT "event_delay"
5     number_type        R        no
6     string_max_length  R        no        u16
7     preset_depth       R        no        optional; list of valid idx values
8     value[depth]       R/RW     yes       repeated per preset idx
9     default_value[d]   R        no        repeated per preset idx
10    min_value[d]       R        yes       repeated per preset idx
11    max_value[d]       R        yes       repeated per preset idx
12    step_size          R        no        optional
13    unit               R        no        optional, 0-terminated UTF-8
14    children           R        no        u32[] child obj-ids
15    options            R        no        enum: 72 bytes per option
16    event_tag          R        no        optional, u16
17    event_prio         RW       yes       optional
18    event_state        R        yes       optional
19    event_messages     R        no        optional, two strings
20    preset_parent      R        no        optional, u32 parent obj-id
```

### ACP2 Number Types (pid 5 and vtype in pid 8-12)

```
0=S8  1=S16  2=S32  3=S64  4=U8  5=U16  6=U32  7=U64  8=float
9=preset/enum  10=ipv4  11=string
```

### ACP2 Property Value Wire Sizes

```
type              wire          plen
u8/u16/u32/s8/s16/s32/float   stored as u32/s32   8
u64/s64           stored as u64/s64               12
preset/enum       u32                              8
ipv4              4×u8                             8
string            bytes + \0   4 + strlen + 1 + padding to 4-byte
```

### ACP2 Preset Depth

When an object is a preset child:
- pid 7 (preset_depth) lists all valid idx values
- pids 8, 9, 10, 11 appear ONCE PER PRESET IDX in get_object reply
- idx=0 is ACTIVE INDEX — device substitutes actual active idx in reply
- NEVER use idx=0 to mean "first preset slot"

### ACP2 Announces (type=2)

```
ACP2 header:
  type=2, mtid=0, stat=0, pid=<property id of changed value>
Body:
  obj-id(u32), idx(u32), property header for pid
```

Announces are only received after AN2 EnableProtocolEvents([2]) is called.
AN2 proto=0 slot events are always received regardless.

### ACP2 Key Invariants

```
- AN2 mtid always 0 for data frames (ACP2 payload carried in type=data)
- ACP2 mtid pool: 1–255, 0 reserved for announces
- mtid NEVER reused while request is in-flight
- EnableProtocolEvents MUST be called after every (re)connect
- pid=4 name is "announce_delay" — never call it "event_delay"
- type=2 name is "announce" — never call it "event"
- All strings ACP2: UTF-8. All strings ACP1: ASCII.
- All multi-byte values: big-endian in both protocols
```

---

## Repository Structure

```
acp/
├── cmd/
│   ├── acp/                     CLI — 14 files (split from monolithic main.go)
│   │   ├── main.go              entrypoint — imports protocol plugins
│   │   ├── cmd_info.go          info command
│   │   ├── cmd_walk.go          walk command
│   │   ├── cmd_get.go           get command
│   │   ├── cmd_set.go           set command
│   │   ├── cmd_watch.go         watch command
│   │   ├── cmd_discover.go      discover command
│   │   ├── cmd_export.go        export command
│   │   ├── cmd_import.go        import command
│   │   ├── cmd_diag.go          diag command (ACP2)
│   │   ├── cmd_list.go          list-protocols command
│   │   ├── common.go            shared CLI helpers (connect, flags)
│   │   ├── format.go            output formatting
│   │   └── help.go              per-command help pages
│   └── acp-srv/                 (planned) API server entrypoint
│
├── internal/
│   ├── protocol/
│   │   ├── errors.go            ACPError hierarchy
│   │   ├── registry.go          register + lookup by name
│   │   ├── types.go             DeviceInfo, SlotInfo, Object, Value,
│   │   │                        ValueRequest, EventFunc — shared
│   │   ├── acp1/
│   │   │   ├── plugin.go        ACP1Factory, init() — registers self
│   │   │   ├── types.go         ACP1 constants, enums, error types
│   │   │   ├── message.go       encode/decode ACP1 header + MDATA
│   │   │   ├── property.go      decode all 11 object type layouts
│   │   │   ├── value_codec.go   typed value encode/decode
│   │   │   ├── client.go        UDP send+receive, retry loop
│   │   │   ├── tcp_client.go    TCP direct transport
│   │   │   ├── listener.go      UDP broadcast receiver goroutine
│   │   │   ├── discover.go      LAN device discovery
│   │   │   ├── browser.go       walker, getValue, setValue, getObject
│   │   │   └── cache.go         LRU+TTL tree cache
│   │   ├── acp2/
│   │   │   ├── plugin.go        ACP2Factory, init() — registers self
│   │   │   ├── types.go         ACP2 constants, enums, error types
│   │   │   ├── codec.go         encode/decode ACP2 messages
│   │   │   ├── property_codec.go property header encode/decode + alignment
│   │   │   ├── session.go       AN2 TCP session, mtid pool, goroutines
│   │   │   ├── framer.go        AN2 frame encode/decode (magic, dlen)
│   │   │   ├── walker.go        DFS tree walk via get_object + children
│   │   │   ├── cache.go         LRU+TTL tree cache
│   │   │   └── diag.go          protocol diagnostic probes
│   │   └── _template/
│   │       └── plugin.go        copy this to add a new protocol
│   │
│   ├── transport/
│   │   ├── udp.go               UDP send/receive primitives
│   │   ├── tcp.go               TCP read/write with context + timeout
│   │   └── capture.go           traffic capture (JSONL)
│   │
│   ├── export/
│   │   ├── export.go            export orchestrator
│   │   ├── json.go              JSON export
│   │   ├── csv.go               CSV export
│   │   ├── yaml.go              YAML export
│   │   ├── importer.go          parse → dry-run → apply
│   │   ├── read_csv.go          CSV import reader
│   │   └── read_yaml.go         YAML import reader
│   │
│   ├── device/                  (planned) unified Device model
│   ├── validator/               (planned) Value constraint validation
│   ├── storage/                 (planned) file-backed persistence
│   └── logging/                 (planned) slog.Handler
│
├── api/                         (planned) REST + WebSocket API
│
├── assets/
│   ├── acp1/
│   │   ├── AXON-ACP_v1_4.pdf   ACP v1 full specification
│   │   └── dissector_acpv1.lua  Wireshark dissector for ACP1
│   └── acp2/
│       ├── acp2_protocol.pdf    ACP v2 full specification
│       ├── an2_protocol.pdf     AN2 transport specification
│       └── dissector_acp2.lua   Wireshark dissector for ACP2
│
├── testdata/
│   ├── acp1/                    ACP1 walk/export test captures
│   └── acp2/                    ACP2 walk/export test captures
│
├── tests/
│   ├── fixtures/                test fixture data
│   ├── unit/
│   │   ├── acp1/                table-driven byte-exact tests
│   │   ├── acp2/                table-driven byte-exact tests
│   │   ├── export/              export round-trip tests
│   │   └── transport/           TCP framer tests
│   └── integration/             //go:build integration
│       ├── acp1/                ACP1_TEST_HOST env var
│       └── acp2/                ACP2_TEST_HOST env var
│
├── docs/
│   ├── ARCHITECTURE.md          system architecture documentation
│   ├── references/              protocol analysis notes
│   └── ...                      deployment, examples, links
│
├── go.mod
├── go.sum
├── Makefile
├── CLAUDE.md                    ← this file
├── agents.md
└── README.md
```

---

## IProtocol Interface

```go
// internal/protocol/iface.go

type Protocol interface {
    Connect(ctx context.Context, ip string, port int) error
    Disconnect() error
    GetDeviceInfo(ctx context.Context) (DeviceInfo, error)
    GetSlotInfo(ctx context.Context, slot int) (SlotInfo, error)
    Walk(ctx context.Context, slot int) ([]Object, error)
    GetValue(ctx context.Context, req ValueRequest) (Value, error)
    SetValue(ctx context.Context, req ValueRequest, val Value) (Value, error)
    Subscribe(req ValueRequest, fn EventFunc) error
    Unsubscribe(req ValueRequest) error
}

type ProtocolMeta struct {
    Name        string
    DefaultPort int
    Description string
}

type ProtocolFactory interface {
    Meta() ProtocolMeta
    New(logger *slog.Logger) Protocol
}
```

All CLI commands, API handlers, and the device registry talk to `Protocol` only.
Never import `internal/protocol/acp1` or `internal/protocol/acp2` from outside
their own package — only `init()` registration and `cmd/` main files may do so.

---

## Error Hierarchy

```
ACPError (base — all errors implement this)
├── TransportError
│   ├── ConnectionRefusedError
│   ├── ConnectionLostError
│   └── FrameDecodeError        (bad magic, truncated frame)
│
├── ACP1Error                   (MTYPE=3 received)
│   ├── ACP1TransportError      MCODE < 16
│   └── ACP1ObjectError         MCODE >= 16
│
├── ACP2Error                   (type=3 received)
│   ├── InvalidObjectError      stat=1
│   ├── InvalidIndexError       stat=2
│   ├── InvalidPropertyError    stat=3
│   ├── AccessDeniedError       stat=4
│   └── InvalidValueError       stat=5
│
├── ValidationError             (client-side, before send)
│   ├── TypeMismatchError
│   ├── OutOfRangeError
│   ├── InvalidEnumError
│   └── StringTooLongError
│
└── ExportImportError
    ├── ParseError
    ├── FormatError
    └── SchemaError
```

---

## Storage

No database. No Redis. Files only.

```
Linux:    ~/.local/share/acp/
macOS:    ~/Library/Application Support/acp/
Windows:  %APPDATA%\acp\
Override: --data-dir flag or config.yaml

config.yaml                       runtime config
devices.yaml                      known devices list
devices/{mac}/device.yaml         device metadata
devices/{mac}/slot_{n}.yaml       slot info + object tree cache
exports/                          default export output dir
```

**Write rules:**
- `devices.yaml` → only on add/remove device
- `slot_{n}.yaml` → only after successful walk
- log entries → NEVER written to disk (in-memory circular buffer, 1000 entries)

### Value Cache

Property values ARE written to disk as a **stale cache** for fast startup.
Values on disk are NEVER trusted — they are marked [stale] on load and must
be confirmed by a live source (announcement or get) before being treated as
current.

**Value freshness states:**
- `stale`   — loaded from disk, not confirmed (gray/italic in UI)
- `live`    — confirmed by announcement, get, or walk (normal display)
- `updated` — just changed by announcement (bold/flash in UI)

**Startup refresh priority:**
1. Load DM + stale values from disk (instant, zero network)
2. Subscribe to objects the client/view uses → announcements update
   subscribed values to [live] within seconds
3. Background walk refreshes remaining objects → [stale] → [live]
4. Walk completes → everything [live]

**Key rule:** subscribed objects are ALWAYS prioritized over background walk.
If an announcement and a walk result arrive for the same object, the
announcement wins (more recent).

> **WARNING — Performance at scale:** With 100–1000 devices and 44k objects
> per slot, the startup refresh strategy needs a smart scheduling algorithm
> to avoid flooding the network with simultaneous walks. Consider:
> staggered walk start, priority queues per device/slot, rate limiting,
> and subscription-first / walk-later ordering. This is NOT implemented
> yet — revisit when acp-srv handles multiple devices concurrently.

---

## Coding Conventions

### Go Specifics

- Go **1.22+**
- `context.Context` as first param on every I/O function
- `log/slog` throughout — never `fmt.Println` for operational output
- `errors.As` / `errors.Is` at call sites — never string-match errors
- `defer` always releases: mtid, connections, file handles
- No `panic` except truly unrecoverable init
- No global mutable state outside `DeviceRegistry`
- File writes: write to `.tmp` then `os.Rename` (atomic)
- All shared state: `sync.RWMutex` or channels

### Testing

- Unit tests: inject `MockTransport` (implements transport interface)
- All codec tests: table-driven, with expected byte sequences from spec
- Integration tests: `//go:build integration`, skip without env var
  - ACP1: `ACP1_TEST_HOST=192.168.1.5`
  - ACP2: `ACP2_TEST_HOST=192.168.1.8`
- Never test against emulator in CI — unit tests only in CI

### Naming

- Acronyms uppercase: `AN2`, `ACP1`, `ACP2`, `MAC`, `IP`, `UDP`, `TCP`
- Error types: `FooError` suffix
- Interface when needed: `ITransport`, `IProtocol`
- Files: `snake_case.go`
- One primary type per file

---

## What NOT to Do

- Never import `internal/protocol/acp1` or `acp2` outside `cmd/` main files
- Never hardcode protocol names in CLI flags or API handlers (use registry)
- Never use fixed byte offsets for ACP2 properties (use pid/plen headers)
- Never call pid=4 "event_delay" — it is "announce_delay"
- Never call ACP2 type=2 "event" — it is "announce"
- Never use idx=0 to mean "first preset slot" — it means ACTIVE INDEX
- Never write property values to disk
- Never add Redis, PostgreSQL, or any external data store
- Never skip AN2 EnableProtocolEvents before expecting ACP2 announces
- Never reuse a live mtid for a new ACP2 request

---

## Adding a New Protocol

```
1. Copy internal/protocol/_template/ to internal/protocol/{name}/
2. Implement Protocol interface
3. Implement ProtocolFactory.Meta() — name, default port
4. Add: func init() { protocol.Register(&Factory{}) }
5. Add import _ "acp/internal/protocol/{name}" in cmd/acp/main.go
6. Add import _ "acp/internal/protocol/{name}" in cmd/acp-srv/main.go
7. Write unit tests in tests/unit/{name}/
8. Done — CLI, API, UI pick it up automatically
```
