# ADR-0022 — Card data model (Device / Frame / Slot / Card / DM)

Status: accepted

This ADR is a **living document**. Add new facts in the Revisions
trailer at the end. Do not spawn a new ADR number unless the whole
record is being superseded.

## Context

Every connector (ACP1, ACP2, Ember+, Probel, OSC, TSL, NMOS, future)
needs the same entity model for physical inventory and the per-card
setup surface. Without one, each protocol drifts on cache shape, slot
semantics, redundancy modelling, and addressing tokens — and every
atomic refactor breaks the others.

NetBox is the industry-standard data model for physical inventory
(`dcim.Device`, `dcim.Module`, `dcim.ModuleBay`, `dcim.ModuleType`,
`dcim.Interface`, `dcim.PowerPort`, `ipam.IPAddress`). Aligning our
vocabulary with NetBox prevents reinvention as the project grows
(PSU pairs, LACP, OOB management, redundancy).

## Decision

### Entity hierarchy (every connector)

```
Device              physical unit (chassis / rack / standalone box)
└── Frame[]         chassis instance per Device (usually 1)
    └── Slot[]      card bays — sparse, empty bays omitted
        └── Card    instance of a card model in that slot
            └── DM  the card's protocol surface (objects)
```

Matrix is a separate entity — see ADR-0023.

### DM is the schema

| DM carries | DM does NOT carry |
|---|---|
| model, sw_rev, protocol, objects | slot, IP, port, host, num_slots, device block, generator/created_at envelope |

- DM is keyed by `(Model, SwRev)` — nothing else.
- DM is agnostic of IP, frame, slot, host, port.
- Same card walked at any slot at any IP yields the **same DM file**.
- Two slots holding the same card reference the same DM by string; no
  on-disk duplication.

### Storage layout (locked)

```
.cache/dm/<proto>/<Model@SwRev>.json     per-card schema, versioned
.cache/manifest/<device-name>.yaml       per-device wiring
```

DM versioning rules:

- New `(Model, SwRev)` observed → new file.
- Never overwrite an existing DM file.
- Never dedupe — two SwRevs with identical trees both persist.

### Manifest shape

The manifest answers **where**; the DM answers **what**.

```yaml
device:
  name: <stable-name>
  protocol: <proto>
  endpoints:
    - ip: <ipv4>
      port: <int>
      transport: tcp|udp          # L4 only; framing handled by plugin
    # second endpoint = redundant controller (same Frame, standby)
    - ip: <ipv4>
      port: <int>
      transport: tcp|udp
frames:
  - name: <frame-name>
    slots:
      - addr: <protocol-specific>  # opaque token, schema below
        dm: <Model>@<SwRev>        # reference into .cache/dm/<proto>/
```

`addr` is an opaque object whose schema is owned by the plugin. Core
only knows: each entry resolves to one DM file via `Model@SwRev`.

| Protocol | `addr` shape |
|---|---|
| ACP1 | `{ slot: <int> }` |
| ACP2 | `{ slot: <int> }` |
| Ember+ | `{ oid: "<dotted.path>" }` (single tree; root or subtree per "card") |
| Probel | `{ matrix: <int>, level: <int> }` (matrix-native; see ADR-0023) |
| Future | plugin defines its own keys (unit id, mac, channel index, ...) |

### Redundancy

- Redundant controller = **N endpoints on ONE Frame** sharing the same
  slots and DMs. Never modelled as N separate Frames.
- The connector drives **exactly one session at any time**; the others
  are standby. Election is lease-based for stateful protocols (per
  internal/amwa/docs/ha.md).
- LACP / bonded NICs are below the connector's view (OS exposes one
  IP). Modelled in the manifest as a single endpoint.

### Card NICs are DM objects, not Frame entities

A card's NICs, IPs, VLANs, and per-port configuration are **objects
inside the DM tree**, walked over the protocol like any other
parameter. The Frame layer does NOT carry separate `Interface` records
per card. NetBox vocabulary maps to the Frame layer only; below the
Slot, everything is DM.

### NetBox vocabulary mapping (Frame layer)

| dhs concept | NetBox object |
|---|---|
| Device / Frame chassis | `dcim.Device` |
| Frame model | `dcim.DeviceType` |
| Slot (card bay) | `dcim.ModuleBay` |
| Card (instance) | `dcim.Module` |
| Card model | `dcim.ModuleType` |
| PSU (×1 or ×2) | `dcim.PowerPort` |
| Controller card | `dcim.Module` (role=controller) |
| 2× controllers redundant | 2× `dcim.Module` + `dcim.VirtualChassis` |
| OOB / iDRAC / iLO / BMC | `dcim.Interface` `mgmt_only=true` + `Device.oob_ip` |
| Production IP | `ipam.IPAddress` → Interface, `Device.primary_ip4` |
| Static IP | `ipam.IPAddress` `status=active` assigned to Interface |
| Card-internal NIC, port, parameter | NOT in NetBox — lives inside the DM tree |

### Every connector uses the manifest — no legacy single-tree input

Every producer (ACP1, ACP2, Ember+, Probel, OSC, TSL, NMOS, future)
boots from `.cache/manifest/<device>.json` + `.cache/dm/<proto>/<Model@SwRev>.json`
references. The legacy `--tree singlefile.json` path is parity debt and
is being retired. Consumers and admin tooling resolve cards through the
same manifest → DM lookup. No protocol may invent its own producer-input
shape.

### Default value sources

A parameter's default value can come from two places. Both coexist; the
UI override wins at runtime when both are present.

| Source | Lives in | Scope |
| --- | --- | --- |
| Vendor default | the DM file (`.cache/dm/<proto>/<Model@SwRev>.json`, per-object `default` field walked off the card) | every instance of this Model@SwRev — baked into the catalogued schema |
| UI override (per-DM) | side-car under `.cache/manifest/defaults/<Model@SwRev>.json` | every instance of this Model@SwRev — operator/SI overrides the vendor default for a card type |
| UI override (per-instance) | manifest slot entry under `defaults: { obj-id-or-path: value }` | this physical card in this slot only |

Resolution order at runtime: per-instance override → per-DM override →
vendor default (DM file). `setDefValue` returns whatever this resolution
produces.

### Where the Frame view is used

- Monitoring + RCA: when an object disappears, the path
  Object → DM → Card → Slot → Frame → Device → endpoint gives the
  physical locator and the alert target.
- Administration: firmware upgrade, hot-swap, capacity audit, NetBox
  sync.

The Frame view is NOT the operational surface — that is the DM (per
protocol) feeding the federation layer.

## Consequences

- Single canonical layout — connectors stop re-deciding per-protocol
  cache shape.
- DM library and runtime cache collapse into one location
  (`.cache/dm/<proto>/`).
- Manifest enables multi-frame, multi-controller deployments without
  schema changes.
- NetBox alignment documented; any future NetBox sync exporter has a
  pre-mapped vocabulary at the Frame layer.
- ACP2 identity-keyed special case is retired; same layout for every
  protocol.

## Forbidden

- DM files containing IP, port, slot, host, num_slots, device block,
  generator, or created_at envelope.
- IP-keyed DM cache (`.cache/devices/<ip>/...`) for any new protocol.
- Modelling redundancy as multiple Frames per Device.
- Per-card NIC/IP/port modelled at the Frame layer instead of inside
  the DM tree.
- Restating any of this in CLAUDE.md, per-connector CLAUDE.md, agent
  files, or memory — those point at this ADR per ADR-0015.

## Enforcement

- PR review checklist line: "any change to DM cache layout, manifest
  shape, or Frame entity → block until this ADR is updated".
- CI check: DM JSON files MUST NOT contain top-level `device`,
  `slots`, `created_at`, `generator`, `host`, `ip`, or `port`.

## Revisions

- 2026-05-14 — initial — yboujraf
- 2026-05-14 — every connector boots from manifest+DM; legacy `--tree singlefile.json` is retired parity debt. Default value resolves per-instance override → per-DM override → vendor DM default — yboujraf
