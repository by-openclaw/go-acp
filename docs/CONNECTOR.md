# Connector Architecture

This document defines the protocol-agnostic connector architecture.
Every protocol plugin (ACP1, ACP2, future Ember+) implements the same
flow. The web UI and CLI are consumers of the connector — they never
manage walks, caching, or subscriptions directly.

---

## Terminology

Aligned with Ember+ conventions for future compatibility.

| Ember+ term | ACP equivalent | Description |
|---|---|---|
| **Provider** | Device | The hardware exposing parameters (Axon card, mixing console) |
| **Consumer** | Connector / CLI / Web UI | Client that reads, writes, subscribes |
| **Node** | Group / Container | Tree structure element (BOARD, PSU, identity, control) |
| **Parameter** | Object | Single readable/writable value with metadata |
| **GetDirectory** | Walk (1 level) | Discover children of a node |
| **Full walk** | Walk (DFS) | Discover entire tree recursively |
| **Subscribe** | Subscribe / Watch | Register for value-change notifications |
| **Notification** | Announcement | Provider pushes value change to consumer |
| **Matrix** | — | Cross-point switching (not in ACP, future Ember+) |
| **Function** | — | Callable operation (not in ACP) |

---

## Data Model Library

### Concept

The **data model** is the tree structure of a card: node hierarchy, parameter IDs,
labels, types, constraints, enum items, units, paths. It does NOT contain values.

The data model is determined by **card type + firmware version**. Same card with
same firmware = identical tree. The library stores discovered models keyed by
`{card_name}_{firmware_version}`.

### Storage

```
models/
  SHPRM1_5.3.5.json        ← Axon CONVERT Hybrid, fw 5.3.5
  DAC24_2.1.0.json          ← Axon DAC24, fw 2.1.0
  RRS18_1601.json           ← Synapse Rack Controller, fw 1601
```

### Identity fields (for library key + validation)

| Field | ACP1 (all slots) | ACP2 CTRL (slot 0) | ACP2 Device (slot 1+) |
|---|---|---|---|
| Card Name | identity > Card name | BOARD > Card Name | IDENTITY > Card Name |
| Firmware | identity > Software rev | BOARD > Product Version | IDENTITY > Product Version |
| Serial | identity > Serial number | BOARD > Serial Number | — (not available) |
| Description | identity > Card description | — | IDENTITY > Card Description |

Library key: `{card_name}_{firmware_version}`.

Note: ACP2 device slots (1+) do NOT expose Serial Number under IDENTITY.
Serial is only available on the CTRL slot (slot 0) under BOARD — it belongs
to the Neuron controller, not the individual device cards.

### Contents (per model file)

```
- card_name          string     "SHPRM1"
- firmware_version   string     "5.3.5"
- serial_number      string     "PR113951"  (for traceability, not matching)
- protocol           string     "acp2"
- tree               nested     full hierarchical tree
  - node/parameter:
    - id             int        unique object ID
    - label          string     human-readable name
    - kind           string     int/uint/float/enum/string/ipaddr/alarm
    - access         string     R--/RW-/RWD
    - path           []string   hierarchical path
    - unit           string     C, %, mW, dBFS, ...
    - min/max/step   number     constraints
    - default        any        default value
    - enum_items     []string   enum option labels
    - max_len        int        string max length
```

**Never stored:** values, IP addresses, slot numbers, timestamps.

### Lifecycle

```
1. First time seeing card+fw combo → full walk → save to library
2. Next time → load from library → instant (no walk)
3. Firmware upgrade → new key → full walk → new model in library
4. Never invalidated — firmware version is immutable
```

---

## Connector Flow

### On connect

```
connect(ip, port)
  → get device info (slot count)
  → for each present slot:
      → read identity (1 quick request):
          ACP1: getObject(identity, 0) → Card name, Software rev
          ACP2: get_object(BOARD) → Card Name, Product Version
      → build key: {card_name}_{fw_version}
      → lookup library: models/{key}.json
      → FOUND:
          load model → instant labels, types, constraints
          subscribe to announcements
          ready
      → NOT FOUND:
          queue background full walk
          subscribe to announcements (raw until walk completes)
          walk completes → save model to library
          switch to decoded values
```

### Addressing

| Method | Scope | Ambiguity |
|---|---|---|
| Object ID | Global per device | None — unique |
| Label | Per group (ACP1) / Per tree (ACP2) | Duplicates possible (e.g. "Present" under PSU/1 and PSU/2) |
| Path | Full tree path | None — unique (BOARD/Card Name, PSU/1/Present) |

**CLI rule:** accept all three. Object ID is the primary key for import/export
round-trip. Path and label are for human convenience.

```
acp get ... --id 15239                    # unambiguous
acp get ... --label "Card Name"           # works if unique
acp get ... --path PSU/1/Present          # unambiguous
acp walk ... --path BOARD                 # subtree filter
acp walk ... --filter Temperature         # text search
```

### Export / Import

Export reads the **current state** (values + model). Import writes values back.
Both operate on the same file format — no re-walk needed.

```
Export:
  walk (or use cached model) → read values → write file

Import:
  read file → dry-run (validate) → apply
  - R-- objects: skip (read-only)
  - RW- objects: set value by object ID
  - unknown objects: skip + warn
```

| Format | Lossless | Round-trip | Human-editable |
|---|---|---|---|
| JSON | yes | yes | yes |
| YAML | yes | yes | yes (best for editing) |
| CSV | lossy | yes (values only) | yes (spreadsheet) |

### Watch (real-time monitoring)

```
watch:
  1. Load model from library → instant labels + units + types
  2. Subscribe to announcements
  3. Display announces with:
     - [cache]  label from library, value raw (walk not done)
     - [live]   label + decoded value from walked tree
  4. Background walk (if library outdated or missing)
  5. Walk completes → all [live], save model to library
```

---

## Connector Interface

Protocol-agnostic. Every plugin implements this.

```go
type Connector interface {
    // Connection
    Connect(ctx, ip, port) error
    Disconnect() error

    // Identity (1 quick request, no walk)
    GetDeviceInfo(ctx) DeviceInfo
    GetSlotIdentity(ctx, slot) (cardName, fwVersion string, err error)

    // Model (library-backed)
    HasModel(cardName, fwVersion) bool
    LoadModel(cardName, fwVersion) (*DataModel, error)
    SaveModel(cardName, fwVersion, *DataModel) error

    // Walk
    WalkFull(ctx, slot) (*DataModel, error)            // DFS entire tree
    WalkNode(ctx, slot, objID) ([]Parameter, error)    // 1 level (lazy)

    // Values
    GetValue(ctx, slot, objID) (Value, error)
    SetValue(ctx, slot, objID, value) (Value, error)

    // Subscriptions
    Subscribe(slot, objID, callback) error
    Unsubscribe(slot, objID) error
}
```

---

## Data Model Reuse

### Same card across slots

```
Slot 0: SHPRM1 fw 5.3.5 → walk → save models/SHPRM1_5.3.5.json
Slot 1: SHPRM1 fw 5.3.5 → found in library → no walk
Slot 2: SHPRM1 fw 5.3.5 → found in library → no walk
Slot 3: DAC24  fw 2.1.0 → not found → walk → save models/DAC24_2.1.0.json
```

10 identical cards = 1 walk, 10 instant loads.

### Across devices

```
Device A, Slot 0: SHPRM1 fw 5.3.5 → walk → library
Device B, Slot 3: SHPRM1 fw 5.3.5 → found → no walk
```

Library is device-independent. Same card+fw = same model everywhere.

---

## CLI Capture for Tests

CLI provides raw inbound/outbound capture for unit tests and new protocol
discovery.

```
acp walk ... --capture slot0_walk.jsonl        # save raw traffic
acp watch ... --capture slot0_watch.jsonl       # save announcements
acp get ... --capture get_card_name.jsonl       # save single request
```

Capture format: JSONL with `{ts, proto, dir, hex, len}` per line.
Direction: `tx` = outbound (consumer → provider), `rx` = inbound (provider → consumer).

These captures are:
- Replayed in unit tests (codec regression)
- Used to discover new object types or protocol variants
- Stored in `testdata/{protocol}/`

---

## Responsibilities

| Component | Owns |
|---|---|
| **Connector** | Walk scheduling, model library, identity check, subscribe/unsubscribe, value decode, announce routing |
| **CLI** | User interaction, formatting, flags, capture |
| **Web UI** | View selection (master/client), display, user input |
| **Export** | Serialize model + values to file (JSON/YAML/CSV) |
| **Import** | Parse file, validate, apply values via connector |

The web UI and CLI are **consumers**. They ask the connector for data.
They never walk, cache, or manage subscriptions directly.

---

## Protocol-Specific Notes

### ACP1

- Identity: `getObject(group=identity, id=0)`:
  - **Device (all slots):** identity > Card name, identity > Software rev, identity > Serial number
- Groups: identity, control, status, alarm, file, frame
- No hierarchy within groups (flat list)
- Walk: 1 root + N objects per group
- Announcements: UDP broadcast, MTID=0
- Announcements require `Broadcasts` param = `On` on the rack controller (slot 0, control group)
- If `Broadcasts` = `Off`, no value-change announcements are sent by the device
- The connector should check/enable this on connect if watch is requested

### ACP2

- Identity fields vary by slot role:
  - **CTRL (slot 0):** BOARD > Card Name, BOARD > Product Version, BOARD > Serial Number
  - **Device (slot 1+):** IDENTITY > Card Name, IDENTITY > Product Version, IDENTITY > Serial Number
- Hierarchy: ROOT_NODE_V2 > BOARD > Card Name, PSU > 1 > Present
- Walk: DFS from root, pid=14 (children) for recursion
- Announcements: AN2 TCP, type=2, mtid=0
- Announcements require AN2 `EnableProtocolEvents([2])` on connect

---

## Provider Mode (future)

### Concept

`acp-srv` acts as a **gateway**: consumer on the device side, provider on
the control side. External systems (Cerebrum, NMOS, third-party controllers)
connect to `acp-srv` as if it were a device.

```
┌─────────────────────────────────────────────────────┐
│  External consumers                                  │
│  Cerebrum, NMOS, third-party                         │
│         │              │              │              │
│    Ember+/TCP     REST/WS       ACP/TCP              │
│         │              │              │              │
│  ┌──────┴──────────────┴──────────────┴──────┐      │
│  │              acp-srv (PROVIDER)            │      │
│  │  - Expose aggregated tree                  │      │
│  │  - Accept subscriptions                    │      │
│  │  - Route notifications                     │      │
│  │  - Proxy get/set to devices                │      │
│  └──────┬──────────────┬──────────────┬──────┘      │
│         │              │              │              │
│    ACP1/UDP       ACP2/TCP       Ember+/TCP          │
│         │              │              │              │
│    Axon Rack 1    Axon Rack 2    Ember+ device       │
│  (PROVIDER)      (PROVIDER)      (PROVIDER)          │
└─────────────────────────────────────────────────────┘
```

### Provider responsibilities

| Responsibility | Description |
|---|---|
| **Listen** | Accept TCP connections from external consumers |
| **Expose tree** | Serve aggregated data model (all devices, all slots) |
| **GetDirectory** | Respond to tree discovery requests (lazy, per-level) |
| **GetValue** | Proxy read to the real device via consumer connector |
| **SetValue** | Proxy write to the real device, return confirmed value |
| **Subscribe** | Track per-client subscriptions, route notifications |
| **Notify** | Push value changes from device announcements to subscribed consumers |
| **Session management** | Track multiple concurrent consumers, clean up on disconnect |

### What exists vs what's needed

| Component | Consumer (done) | Provider (to add) |
|---|---|---|
| Data model / tree | yes | reuse |
| Value read/write | yes | proxy through |
| Subscriptions | yes (device → us) | add (us → external client) |
| Codec (ACP1/ACP2) | yes | reuse for device side |
| Codec (Ember+ Glow) | no | encode our tree as Glow DTD |
| TCP client (connect out) | yes | reuse |
| TCP server (listen) | no | accept inbound connections |
| Session manager | no | track consumers, subscriptions, cleanup |
| Notification router | no | device announce → fan out to subscribed consumers |

### Aggregated tree

The provider exposes a **single unified tree** combining all connected devices:

```
Root
├── Device_10.41.40.195 (ACP2)
│   ├── Slot 0 (SHPRM1)
│   │   ├── BOARD
│   │   │   ├── Card Name = "SHPRM1"
│   │   │   └── ...
│   │   ├── PSU
│   │   └── TEMPERATURE
│   └── Slot 1 (CONVERT)
│       ├── IDENTITY
│       ├── INPUT
│       └── OUTPUT
├── Device_10.6.239.113 (ACP1)
│   └── Slot 0 (RRS18)
│       ├── identity
│       ├── control
│       ├── status
│       └── alarm
└── Device_10.50.1.20 (Ember+, future)
    └── ...
```

### Provider protocols (planned)

| Protocol | Use case | Priority |
|---|---|---|
| REST + WebSocket | Web UI (`acp-ui`) | phase 1 |
| Ember+ (Glow/S101) | Cerebrum, broadcast controllers | phase 2 |
| ACP2 provider | Expose as virtual ACP2 device | phase 3 |

### Use cases (to be defined)

- Cerebrum connects to `acp-srv` via Ember+ → sees all Axon devices as one tree
- Web UI connects via WebSocket → master/client view with live updates
- Third-party NMOS controller discovers `acp-srv` → reads/writes parameters
- Redundancy: two `acp-srv` instances, both consumers of same devices, one active provider
- Multi-site: `acp-srv` per site, central aggregator connects to all

### Scope (to be reviewed)

- Phase 1: REST + WebSocket provider for `acp-ui`
- Phase 2: Ember+ provider (Glow DTD encoder + S101 framer + TCP server)
- Phase 3: ACP2 provider (virtual device)
- Out of scope: ACP1 provider (ACP1 is UDP-only, no server mode in spec)

---

### Ember+ (future)

- Identity: node attributes at root
- Hierarchy: arbitrary depth nodes + parameters
- Walk: `GetDirectory` per node (lazy)
- Notifications: provider pushes on change
- Same connector interface, different plugin
