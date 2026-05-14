# ADR-0023 — Matrix entity

Status: accepted

This ADR is a **living document**. Add new facts in the Revisions
trailer at the end. Do not spawn a new ADR number unless the whole
record is being superseded.

## Context

Routing matrices (Probel SW-P-08/SW-P-88, Ember+ matrix nodes, NMOS
IS-05 connections, future fieldbus crossbars) are not cards in a slot
and don't fit the Frame → Slot → Card hierarchy from ADR-0022. They
need their own entity with its own addressing tokens.

A matrix can be:
- the entire payload of a protocol (Probel — every command is a matrix
  operation),
- a node inside a tree (Ember+ — matrix lives at a specific OID under
  the device's tree),
- a per-card surface (an SDI router card in a Synapse-style frame
  exposing a small matrix via its DM objects).

## Decision

### Matrix is a parallel entity

Matrix sits beside the Card/Frame hierarchy (ADR-0022), not under it.
A single Device can carry:
- zero or more Cards (Frame → Slot → Card → DM)
- zero or more Matrices, each with its own coordinates

### Matrix identity

| Field | Meaning |
|---|---|
| `matrix_id` | integer per device |
| `level_id` | level index (audio L/R/embed/multi-format/etc) |
| `size` | `(destinations, sources)` tuple — scale rules per `project_scale_requirements` |
| `behavior` | `1to1` / `1toN` / `NtoM` / `dynamic` |

### Behavior values

| Value | Meaning |
|---|---|
| `1to1` | one source feeds exactly one destination |
| `1toN` | multiple destinations can take the same source (multicast / fan-out) |
| `NtoM` | full crosspoint freedom |
| `dynamic` | size or topology can change at runtime |

### Addressing a crosspoint

| Protocol | Coordinates to reach a crosspoint |
|---|---|
| Probel | `(matrix_id, level_id, dst_id, src_id)` |
| Ember+ | OID path to matrix node + `(connection_target, connection_source)` per glow connection |
| NMOS IS-05 | `(receiver_id, sender_id)` over `/connection/v1.x/single/receivers/<id>/staged|active` |
| Future | plugin defines its own coordinates (channel, voice, group, ...) |

### Manifest entry for a matrix

When a Device exposes one or more matrices, the manifest adds a
parallel `matrices` block beside `frames`:

```yaml
device:
  name: <stable-name>
  protocol: <proto>
  endpoints: [...]
frames: [...]      # optional — empty for pure-matrix devices like Probel
matrices:
  - matrix_id: 1
    level_id: 0
    size: [1024, 1024]
    behavior: NtoM
    label: "Main video"
  - matrix_id: 1
    level_id: 1
    size: [1024, 1024]
    behavior: NtoM
    label: "Audio L"
```

For Ember+ matrix nodes, the matrix entry additionally carries the OID
path of the matrix node in the tree:

```yaml
matrices:
  - matrix_id: 1
    level_id: 0
    oid: "1.4.2"
    size: [128, 128]
    behavior: NtoM
```

### Scale floor

Every plugin must cope with the minimums in
`project_scale_requirements`:

- 65 535 × 65 535 destinations × sources per matrix
- 20 – 100 matrices per plant
- streaming decoder mandatory (never buffer a full tally dump)
- sparse maps (`map[(matrix, level, dst)] → src`), never dense arrays

### Dual-controller matrix

A matrix can be fronted by two controllers (redundancy). The model:

| Aspect | Rule |
| --- | --- |
| Identifying the active controller | Protocol-native if available — for Probel SW-P-08 that is cmd 8 (Rx Dual Controller Status Request) → cmd 9 (Tx Dual Controller Status Response). For protocols without a native indicator, external lease (per `project_ha_architecture`). |
| Writes (route change / SetValue) | Go to **active controller only**. The offline controller drops / no-ops — does NOT relay, does NOT act. |
| Reads (GetStatus, tally, mnemonics) | Either controller serves; DB is mirrored. Read symmetry. |
| Failover | Active indicator changes → consumer flips writes to the new active immediately; existing read sessions stay open on both. |

This applies symmetrically to the provider (emulator): the emulator
must answer cmd 8 / cmd 9 honestly and only accept route writes on the
endpoint it currently advertises as active.

### What the matrix entity does NOT carry

- Card-internal config (timing, format, embed mode) — that lives in
  the DM of the card hosting the matrix.
- Physical inventory (slot, frame, controller) — that lives in the
  Frame hierarchy of ADR-0022.
- Network addressing (IP, port, transport) — that lives in the
  manifest's `endpoints` block.

## Consequences

- One vocabulary for matrices across every protocol.
- Probel manifests have `matrices` but typically no `frames`.
- Ember+ manifests can have both `frames` (if the device also exposes
  cards) and `matrices` (OID-anchored).
- Federation layer can address any crosspoint through
  `(device, matrix_id, level_id, dst, src)` regardless of protocol.

## Forbidden

- Modelling a matrix as a Card or as a Frame.
- Per-protocol matrix coordinate schemas leaking into core code — they
  stay in the plugin.
- Buffering an entire tally dump in memory before emitting events.
- Restating any of this in CLAUDE.md, per-connector CLAUDE.md, agent
  files, or memory — those point at this ADR per ADR-0015.

## Enforcement

- PR review checklist line: "any change to matrix entity, manifest
  matrices block, or behavior values → block until this ADR is
  updated".
- Memory `project_dhs_data_model.md` is a 3-line bookmark to this ADR
  (alongside the ADR-0022 pointer).

## Revisions

- 2026-05-14 — initial — yboujraf
- 2026-05-14 — added §"Dual-controller matrix": writes go to active only; reads symmetric on both; Probel cmd 8/9 is the native indicator — yboujraf
