# Matrix

A **Matrix** is a crosspoint grid. Targets (outputs) are rows; sources
(inputs) are columns; a connection selects one or more sources per target.
Matrices are the richest element type in Ember+: they carry structural
metadata (type, mode, counts), connection state (with disposition), plus
labels and per-crosspoint gain parameters that may live inline or at
pointed-to OIDs elsewhere in the tree.

## Classification axes

| Axis        | Values                     | Meaning                                                                                   |
|-------------|----------------------------|-------------------------------------------------------------------------------------------|
| `type`      | `oneToN`                   | Exactly **1 source per target**. Connecting a new source replaces the old one.            |
|             | `oneToOne`                 | Exactly 1 source per target, **and each source used at most once**. Reroutes steal.       |
|             | `nToN`                     | 0..`maximumConnectsPerTarget` sources per target. Explicit `connect`/`disconnect` ops.    |
| `mode`      | `linear`                   | Targets are `0..targetCount-1`, sources are `0..sourceCount-1`. No holes.                  |
|             | `nonLinear`                | Targets/sources are explicit lists — can be sparse / renumbered / reordered.               |
| dynamism    | static                     | Target/source/connection set fixed at walk time. Consumer caches.                         |
|             | dynamic                    | Provider sends `targets`/`sources`/`connections` updates at runtime. Consumer re-renders.  |

Any combination is valid: a `nToN` + `nonLinear` + dynamic matrix is the
hardest case but legal per spec §5.1.

## Field reference

| Key                         | Type              | Wire meaning (codec dev)                                                                                                          | UI hint (webui dev)                                                                |
|-----------------------------|-------------------|-----------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------|
| *common header*             |                   | See [node.md](node.md).                                                                                                           | Same.                                                                              |
| `type`                      | string            | `oneToN` / `oneToOne` / `nToN`. Drives connection constraints.                                                                    | Determines UI: radio vs exclusive grid vs multi-select grid.                       |
| `mode`                      | string            | `linear` / `nonLinear`.                                                                                                           | If nonLinear, render by `targets[].number` / `sources[].number`, not by position.  |
| `targetCount`               | integer           | Declared row count.                                                                                                               | Grid rows.                                                                         |
| `sourceCount`               | integer           | Declared column count.                                                                                                            | Grid columns.                                                                      |
| `maximumTotalConnects`      | integer \| null   | nToN only. Bus-wide connection budget.                                                                                            | Counter "X of Y total used".                                                       |
| `maximumConnectsPerTarget`  | integer \| null   | nToN only. Per-target budget.                                                                                                     | Per-row counter; disable further adds when hit.                                    |
| `parametersLocation`        | string \| null    | OID (dot-joined) of a Node whose children mirror the target/source/crosspoint structure with per-item parameters (e.g. gain).      | Not shown; consumer uses to pull gain.                                             |
| `gainParameterNumber`       | integer \| null   | Index of the "gain" child parameter within each connection's parameter set.                                                        | Not shown.                                                                         |
| `labels`                    | array             | `[{basePath, description}]`. Pointer form of labels; each `basePath` is an OID of a Node holding label children.                  | Tooltip "labels at <path>".                                                        |
| `targets`                   | array             | `[{number}]`. Declared target indices.                                                                                             | Row headers.                                                                       |
| `sources`                   | array             | `[{number}]`. Declared source indices.                                                                                             | Column headers.                                                                    |
| `connections`               | array             | `[{target, sources[], operation, disposition, locked}]`. Current crosspoint state.                                                | Filled cells of the grid.                                                          |
| `targetLabels`              | object \| null    | `{<description>: {<number>: "<label>"}}`. **Multi-level** — outer key comes from `labels[i].description` (e.g. `"Primary"`, `"Long"`). Emitted under `--labels=inline\|both`. | Row header text — pick the level suited to the UI (e.g. short in list view, long in tooltip); fall back to `target <number>`. |
| `sourceLabels`              | object \| null    | Same shape as `targetLabels` for sources. Multi-level.                                                                            | Column header text.                                                                |
| `targetParams`              | object \| null    | `{<number>: {<paramKey>: <value>}}`. Per-target resolved parameters (e.g. output trim). `--gain=inline\|both`.                    | Per-row side panel.                                                                |
| `sourceParams`              | object \| null    | Same for sources.                                                                                                                  | Per-column side panel.                                                             |
| `connectionParams`          | object \| null    | `{"<target>.<source>": {<paramKey>: <value>}}`. Per-crosspoint resolved parameters (e.g. crosspoint gain). `--gain=inline\|both`. | Cell tooltip / right-click panel.                                                  |

### Connection subfields

| Key           | Type      | Values                                               | Notes                                                                                 |
|---------------|-----------|------------------------------------------------------|---------------------------------------------------------------------------------------|
| `target`      | integer   | Target number.                                       |                                                                                       |
| `sources`     | int[]     | Source numbers.                                      | Length bound by `type` (see below).                                                   |
| `operation`   | string    | `absolute` / `connect` / `disconnect`                | Only meaningful on set. Reads return `absolute`.                                       |
| `disposition` | string    | `tally` / `modified` / `pending` / `locked`          | `tally`=stable, `modified`=updated after set, `pending`=in-flight, `locked`=blocked.  |
| `locked`      | boolean   |                                                      | Provider-set lock; consumer must not attempt set.                                     |

### Connection length constraints by matrix type

| `type`     | `sources[]` per target                                   |
|------------|----------------------------------------------------------|
| `oneToN`   | Exactly 1. Connect replaces previous.                    |
| `oneToOne` | Exactly 1, AND each source appears in at most 1 target.  |
| `nToN`     | `0 .. maximumConnectsPerTarget`.                         |

### Protocol-specific type restrictions

| Protocol         | Allowed matrix types                       | Notes                                                          |
|------------------|--------------------------------------------|----------------------------------------------------------------|
| Ember+           | `oneToN`, `oneToOne`, `nToN`               | Full spec support.                                             |
| Probel SW-P-02   | `oneToN`, `oneToOne`                       | No source summing; no dynamic matrices. Plugin enforces on emit.|
| Probel SW-P-08+  | `oneToN`, `oneToOne`                       | Same as SW-P-02. MIXER CONNECT (§3.2.23) carries gain, but still oneToN at the routing level. |
| ACP1 / ACP2      | N/A                                        | Neither protocol has matrix semantics; Matrix element unused.  |
| TSL UMD          | N/A                                        | Tally/label protocol; no routing.                              |

### Connection subobject — extensions

These keys extend the core Connection subobject when the source
protocol carries the concept. Emit `null` when absent:

| Key             | Type                        | Protocols | Notes                                                                                                   |
|-----------------|-----------------------------|-----------|---------------------------------------------------------------------------------------------------------|
| `protect`       | `{state, owner} \| null`    | Probel    | **Orthogonal to `locked`.** `locked` = blocked for everyone. `protect` = blocked for everyone EXCEPT the owner. `state ∈ "none" \| "probel" \| "probelOverride" \| "oem"`. `owner` = 8-char ASCII device name from PROTECT_DEVICE_NAME_RESPONSE. A crosspoint can be both `locked:true` and `protect.state:"probel"`. |
| `valid`         | `boolean` (default `true`)  | Probel    | `false` when provider reports a "bad source" bit (SW-P-02 §3.2.49 status bit 1; SW-P-08 §3.5.14). Plugin fires `probel_bad_source_connect` on first occurrence per connection. |

### Matrix element — extensions

These keys extend the top-level Matrix element when the source
protocol needs them:

| Key                        | Type                  | Protocols | Notes                                                                                          |
|----------------------------|-----------------------|-----------|------------------------------------------------------------------------------------------------|
| `matrixId`                 | `integer \| null`     | Probel    | Native Probel matrix address (§3.2.x). Emit one Matrix per (matrixId, level) pair.             |
| `level`                    | `integer \| null`     | Probel    | Native Probel level address. Multi-level providers emit N Matrices siblings under a Node.      |
| `supportedLabelVariants`   | `string[] \| null`    | Probel    | Char widths the controller reports via Name Identifier Flags (§3.2.x p.2304). `["4","8","12","16"]`. |

### Multi-level matrix pattern

Ember+ has no native "level" concept. When a Probel (or similar)
provider exposes a single physical matrix over N levels (video + audio
stems + AES + metadata), the canonical tree represents it as one
grouping Node containing N Matrix siblings — one per level — all
sharing the same `matrixId`, each with its own `level`.

```
Node "router-A" (matrixId=3)
├─ Matrix { matrixId: 3, level: 0, identifier: "video",   … }
├─ Matrix { matrixId: 3, level: 1, identifier: "audioL",  … }
├─ Matrix { matrixId: 3, level: 2, identifier: "audioR",  … }
├─ Matrix { matrixId: 3, level: 3, identifier: "aes",     … }
└─ Matrix { matrixId: 3, level: 4, identifier: "meta",    … }
```

SW-P-02 supports up to 28 levels (per §3.2.58/3.2.59, 4-byte bitmap).
SW-P-08+ extends further via extended commands. Typical field
deployment runs 1–8 levels.

> **Status:** `matrixId`, `level`, `supportedLabelVariants`,
> `Connection.protect`, and `Connection.valid` are documented here
> but not yet in `internal/export/canonical/*.go`. They are added
> when the Probel plugin phase starts. See
> `memory/project_probel_extensions.md`.

## Sample 1 — oneToN, linear, textbook basePath labels (5×5)

Standard video router. Labels point at a separate Node subtree at OID
`3.0.1` (targets) and `3.0.2` (sources). `--labels=inline` (default) resolves
them into `targetLabels`/`sourceLabels`.

```json
{
  "number": 0,
  "identifier": "matrix",
  "path": "router.matrix",
  "oid": "3.0.0",
  "description": "Video router",
  "isOnline": true,
  "access": "readWrite",

  "type": "oneToN",
  "mode": "linear",
  "targetCount": 5,
  "sourceCount": 5,
  "maximumTotalConnects": null,
  "maximumConnectsPerTarget": null,

  "parametersLocation": null,
  "gainParameterNumber": null,

  "labels": [
    { "basePath": "3.0.1", "description": "Targets" },
    { "basePath": "3.0.2", "description": "Sources" }
  ],

  "targets": [
    { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 }, { "number": 4 }
  ],
  "sources": [
    { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 }, { "number": 4 }
  ],

  "connections": [
    { "target": 0, "sources": [0], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 1, "sources": [1], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 2, "sources": [2], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 3, "sources": [0], "operation": "absolute", "disposition": "modified", "locked": false },
    { "target": 4, "sources": [4], "operation": "absolute", "disposition": "tally", "locked": false }
  ],

  "targetLabels": {
    "Primary": { "0": "MON 1", "1": "MON 2", "2": "MV",    "3": "REC",   "4": "PRV" },
    "Long":    { "0": "Monitor 1", "1": "Monitor 2", "2": "Multiview", "3": "Record", "4": "Preview" }
  },
  "sourceLabels": {
    "Primary": { "0": "CAM 1", "1": "CAM 2", "2": "CAM 3", "3": "CLIP1", "4": "CLIP2" }
  },

  "children": []
}
```

## Sample 2 — oneToOne (routing), in-matrix label subtree

Intercom / comms matrix where each source can only feed one target at a
time. Labels live **inside the matrix** as Node children, not at a separate
basePath. `--labels=inline` resolves the in-matrix children into the inline
maps.

```json
{
  "number": 1,
  "identifier": "intercom",
  "path": "comms.intercom",
  "oid": "4.0",
  "description": "Intercom assignments",
  "isOnline": true,
  "access": "readWrite",

  "type": "oneToOne",
  "mode": "linear",
  "targetCount": 4,
  "sourceCount": 4,
  "maximumTotalConnects": null,
  "maximumConnectsPerTarget": null,

  "parametersLocation": null,
  "gainParameterNumber": null,

  "labels": [],

  "targets": [ { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 } ],
  "sources": [ { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 } ],

  "connections": [
    { "target": 0, "sources": [2], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 1, "sources": [0], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 2, "sources": [1], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 3, "sources": [3], "operation": "absolute", "disposition": "tally", "locked": true  }
  ],

  "targetLabels": {
    "Primary": { "0": "Director", "1": "Producer", "2": "TD", "3": "Audio" }
  },
  "sourceLabels": {
    "Primary": { "0": "CAM-A",    "1": "CAM-B",    "2": "EVS", "3": "PGM" }
  },

  "children": []
}
```

## Sample 3 — nToN, linear, pointer-only labels and gain

Audio summing matrix. 8 outputs can each receive up to 4 inputs. Gain
parameters live under `2.0.3000` — one gain child per connection. Labels
are at pointed-to OIDs. User invoked with `--labels=pointer --gain=pointer`.

```json
{
  "number": 0,
  "identifier": "summing",
  "path": "audio.summing",
  "oid": "2.0",
  "description": "Audio summing bus",
  "isOnline": true,
  "access": "readWrite",

  "type": "nToN",
  "mode": "linear",
  "targetCount": 8,
  "sourceCount": 16,
  "maximumTotalConnects": 128,
  "maximumConnectsPerTarget": 4,

  "parametersLocation": "2.0.3000",
  "gainParameterNumber": 1,

  "labels": [
    { "basePath": "2.0.4000", "description": "Bus outputs" },
    { "basePath": "2.0.4001", "description": "Bus inputs"  }
  ],

  "targets": [
    { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 },
    { "number": 4 }, { "number": 5 }, { "number": 6 }, { "number": 7 }
  ],
  "sources": [
    { "number":  0 }, { "number":  1 }, { "number":  2 }, { "number":  3 },
    { "number":  4 }, { "number":  5 }, { "number":  6 }, { "number":  7 },
    { "number":  8 }, { "number":  9 }, { "number": 10 }, { "number": 11 },
    { "number": 12 }, { "number": 13 }, { "number": 14 }, { "number": 15 }
  ],

  "connections": [
    { "target": 0, "sources": [0, 2, 4, 6],   "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 1, "sources": [1, 3, 5, 7],   "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 2, "sources": [0, 1],         "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 3, "sources": [],             "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 4, "sources": [8, 9, 10, 11], "operation": "absolute", "disposition": "pending", "locked": false }
  ],

  "targetLabels": null,
  "sourceLabels": null,
  "targetParams": null,
  "sourceParams": null,
  "connectionParams": null,

  "children": []
}
```

Under `--gain=inline --labels=inline` the same matrix would carry populated
`targetLabels`, `sourceLabels`, `targetParams`, `sourceParams`,
`connectionParams` — see Sample 5.

## Sample 4 — nToN, nonLinear (sparse numbering)

Only physical slots 0, 2, 4, 7 are populated. The provider emits explicit
target/source numbers instead of `0..N-1`.

```json
{
  "number": 0,
  "identifier": "routing",
  "path": "frame.routing",
  "oid": "5.0",
  "description": "Frame routing (populated slots only)",
  "isOnline": true,
  "access": "readWrite",

  "type": "nToN",
  "mode": "nonLinear",
  "targetCount": 4,
  "sourceCount": 4,
  "maximumTotalConnects": 16,
  "maximumConnectsPerTarget": 2,

  "parametersLocation": null,
  "gainParameterNumber": null,
  "labels": [],

  "targets": [ { "number": 0 }, { "number": 2 }, { "number": 4 }, { "number": 7 } ],
  "sources": [ { "number": 0 }, { "number": 2 }, { "number": 4 }, { "number": 7 } ],

  "connections": [
    { "target": 0, "sources": [0],    "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 2, "sources": [2, 4], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 4, "sources": [],     "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 7, "sources": [7],    "operation": "absolute", "disposition": "tally", "locked": true  }
  ],

  "targetLabels": {
    "Primary": { "0": "Slot A", "2": "Slot C", "4": "Slot E", "7": "Slot H" }
  },
  "sourceLabels": {
    "Primary": { "0": "In A",   "2": "In C",   "4": "In E",   "7": "In H"   }
  },

  "children": []
}
```

The webui must iterate `targets[]` / `sources[]` by `number` and not assume
positions 0..N-1.

## Sample 5 — nToN with inline gain (crosspoint level control)

Same summing bus as Sample 3, now exported with
`--gain=inline --labels=inline`. `connectionParams` keys use
`"<target>.<source>"`.

```json
{
  "number": 0,
  "identifier": "summing",
  "path": "audio.summing",
  "oid": "2.0",
  "description": "Audio summing bus",
  "isOnline": true,
  "access": "readWrite",

  "type": "nToN",
  "mode": "linear",
  "targetCount": 4,
  "sourceCount": 4,
  "maximumTotalConnects": 16,
  "maximumConnectsPerTarget": 4,

  "parametersLocation": "2.0.3000",
  "gainParameterNumber": 1,

  "labels": [],

  "targets": [ { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 } ],
  "sources": [ { "number": 0 }, { "number": 1 }, { "number": 2 }, { "number": 3 } ],

  "connections": [
    { "target": 0, "sources": [0, 1], "operation": "absolute", "disposition": "tally", "locked": false },
    { "target": 1, "sources": [2],    "operation": "absolute", "disposition": "tally", "locked": false }
  ],

  "targetLabels": {
    "Primary": { "0": "Bus L", "1": "Bus R", "2": "Bus C", "3": "Bus Sub" }
  },
  "sourceLabels": {
    "Primary": { "0": "Ch 1",  "1": "Ch 2",  "2": "Ch 3",  "3": "Ch 4"    }
  },

  "targetParams": {
    "0": { "trim": 0.0 },
    "1": { "trim": 0.0 },
    "2": { "trim": -3.0 },
    "3": { "trim": 0.0 }
  },
  "sourceParams": {
    "0": { "pad": false }, "1": { "pad": false }, "2": { "pad": true }, "3": { "pad": false }
  },
  "connectionParams": {
    "0.0": { "gain": -6.0 },
    "0.1": { "gain": -6.0 },
    "1.2": { "gain":  0.0 }
  },

  "children": []
}
```

## Sample 6 — split / duplicate label pattern

The provider advertises `basePath` **and** duplicates labels as Parameter
children under the matrix itself. Under `--labels=both` both forms are kept
so the consumer can verify agreement; the inline map is the authoritative
source for display.

```json
{
  "number": 0,
  "identifier": "matrix",
  "path": "router.matrix",
  "oid": "3.0.0",
  "description": "Duplicate-label router",
  "isOnline": true,
  "access": "readWrite",

  "type": "oneToN",
  "mode": "linear",
  "targetCount": 2,
  "sourceCount": 2,
  "maximumTotalConnects": null,
  "maximumConnectsPerTarget": null,
  "parametersLocation": null,
  "gainParameterNumber": null,

  "labels": [
    { "basePath": "3.0.1", "description": "Targets" },
    { "basePath": "3.0.2", "description": "Sources" }
  ],

  "targets": [ { "number": 0 }, { "number": 1 } ],
  "sources": [ { "number": 0 }, { "number": 1 } ],
  "connections": [
    { "target": 0, "sources": [0], "operation": "absolute", "disposition": "tally", "locked": false }
  ],

  "targetLabels": {
    "Targets": { "0": "OUT 1", "1": "OUT 2" }
  },
  "sourceLabels": {
    "Sources": { "0": "IN 1",  "1": "IN 2" }
  },

  "children": []
}
```

Consumer notes: fires `label_duplicate` when both are present and agree;
fires `label_missing` if `basePath` is set but the pointed-to Node has no
label children.

## Sample 7 — dynamic matrix (runtime targets/sources/connections updates)

Conceptual shape at walk time — the provider may later send updates that
add rows, remove rows, change connection dispositions (`pending` →
`modified` → `tally`), or lock crosspoints on demand. The export snapshot
captures a single instant; the live consumer re-renders on every update.

```json
{
  "number": 0,
  "identifier": "dyn",
  "path": "virt.dyn",
  "oid": "6.0",
  "description": "Dynamic routing (sessions come and go)",
  "isOnline": true,
  "access": "readWrite",

  "type": "nToN",
  "mode": "nonLinear",
  "targetCount": 3,
  "sourceCount": 3,
  "maximumTotalConnects": 64,
  "maximumConnectsPerTarget": 8,

  "parametersLocation": null,
  "gainParameterNumber": null,
  "labels": [],

  "targets": [ { "number": 1024 }, { "number": 1025 }, { "number": 1026 } ],
  "sources": [ { "number": 2001 }, { "number": 2002 }, { "number": 2003 } ],

  "connections": [
    { "target": 1024, "sources": [2001, 2003], "operation": "absolute", "disposition": "tally",    "locked": false },
    { "target": 1025, "sources": [2002],       "operation": "absolute", "disposition": "modified", "locked": false },
    { "target": 1026, "sources": [],           "operation": "absolute", "disposition": "pending",  "locked": false }
  ],

  "targetLabels": {
    "Primary": { "1024": "Session-A", "1025": "Session-B", "1026": "Session-C" }
  },
  "sourceLabels": {
    "Primary": { "2001": "Talent-1",  "2002": "Talent-2",  "2003": "Guest-VOIP" }
  },

  "children": []
}
```

Consumer handling of a runtime update:

| Provider sends                 | Consumer action                                                                       |
|--------------------------------|---------------------------------------------------------------------------------------|
| New `targets[]` entry          | Add row; emit update event upward.                                                    |
| Removed `targets[]` entry      | Remove row and any connections referencing it.                                        |
| Connection `disposition:pending`| Show crosspoint as in-flight (e.g. yellow); do not treat as final.                   |
| Connection `disposition:modified`| Treat as latest value until next `tally` arrives.                                    |
| Connection `disposition:tally` | Stable state; clear any in-flight UI.                                                 |
| Connection `disposition:locked`| Render as read-only; reject user attempts to change.                                  |

## Provider variations

| Pattern                                         | Notes                                                                                           |
|-------------------------------------------------|-------------------------------------------------------------------------------------------------|
| Textbook `basePath` + inline resolution         | Default path. `inline` mode fills `targetLabels`/`sourceLabels`.                                |
| In-matrix label subtree only                    | Some providers omit `basePath`; labels are Node children under the matrix itself.               |
| Duplicate (`basePath` + in-matrix)              | Defensive provider design. `both` mode keeps both forms; `label_duplicate` fires.               |
| Pointer-only label                              | Consumer leaves labels unresolved; UI fetches on demand.                                        |
| nToN with `maximumConnectsPerTarget` unset      | Non-compliant; consumer treats as unbounded but fires `field_inferred`.                         |
| Mixed `oneToN` + `disposition:locked` crosspoints | Allowed — individual crosspoints can be locked regardless of matrix type.                     |
| `operation` always `absolute` on reads          | Correct — `connect`/`disconnect` only valid on client-to-provider sets.                         |
| Labels with empty `description`                 | Consumer uses `number` as fallback label.                                                        |

## Consumer handling

- **Walk**: `GetDirectory` on the matrix implicitly subscribes to matrix
  connection changes (spec §5.1 p.42). Provider streams disposition updates
  as the crosspoints change. No separate Subscribe needed.
- **Set**: consumer sends a matrix-addressed Connection with
  `operation=connect|disconnect|absolute` depending on matrix `type`.
  `oneToN` set always uses `absolute`; `nToN` set uses `connect`/`disconnect`.
- **Label resolution** (`--labels=inline|both`): after walking labels at
  `basePath` (or inside the matrix), build `targetLabels`/`sourceLabels`.
  If both forms exist and agree → `label_duplicate`. If one is missing →
  `label_basepath_only` / `label_inline_only`. If `basePath` set but no
  labels found at that OID → `label_missing`.
- **Gain resolution** (`--gain=inline|both`): walk `parametersLocation` to
  find per-target / per-source / per-crosspoint parameter nodes. Map each
  to `targetParams`/`sourceParams`/`connectionParams`. Missing subtree →
  `gain_missing`.
- **Dynamic updates**: every further provider message updating a matrix
  patches the in-memory tree; the consumer fires an event upward.
- **Compliance events (matrix-specific)**: `label_*`, `gain_*`,
  `proto_cross_empty` (when bridging to a protocol that can't carry
  crosspoint gain).

## See also

- [`../schema.md`](../schema.md) — 3 mode flags, compliance events.
- [`parameter.md`](parameter.md) — gain/trim/pad are Parameters.
- [`node.md`](node.md) — matrix is a peer of Node in the tree.
