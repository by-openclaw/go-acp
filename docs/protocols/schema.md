# Canonical Element Schema

**Status:** locked 2026-04-18. Applies to ALL protocols (Ember+, ACP1, ACP2,
future Probel SW-P-02 / SW-P-08+, TSL UMD v3.1/v5).

The schema uses the **Ember+ per-type shape as the union**. Protocols with
fewer concepts simply set unused keys to `null` (the key is still present —
stable key set across protocols lets consumers aggregate without branching).

Per-type element docs (realistic samples, dev + UI hints, provider variations)
live in [`elements/`](elements/) — one file per type. This document is the
concise cross-cutting spec; the `elements/` files are where codec devs and
webui devs go first.

---

## 1. Element types

| Type         | Doc                                                  | Status       | Notes                                          |
|--------------|------------------------------------------------------|--------------|------------------------------------------------|
| Node         | [`elements/node.md`](elements/node.md)               | shipped      | Container; carries `children[]`.               |
| Parameter    | [`elements/parameter.md`](elements/parameter.md)     | shipped      | Leaf value; 7 value-types.                     |
| Matrix       | [`elements/matrix.md`](elements/matrix.md)           | shipped      | Crosspoint grid; oneToN / oneToOne / nToN / dynamic. |
| Function     | [`elements/function.md`](elements/function.md)       | shipped      | Callable; typed arguments + result tuple.      |
| Template     | [`elements/template.md`](elements/template.md)       | shipped      | QualifiedTemplate; shape library.              |
| Stream       | [`elements/stream.md`](elements/stream.md)           | shipped      | Parameter with `streamIdentifier`; merged into its `value`. |
| **Salvo**    | [`elements/salvo.md`](elements/salvo.md)             | **planned**  | First-class stateful salvo (staged members + commit tally). Lands with Probel SW-P-08+ plugin. |

`Stream` is not a standalone top-level element in the export. Ember+ sends
stream values in a `StreamCollection` frame (APPLICATION[6]); the export
pipeline merges each entry back onto its owning Parameter by
`streamIdentifier`. See §5.

---

## 2. Common header (every element)

Always present, always in this order at the top of every element object:

| Key           | Type           | Notes                                                 |
|---------------|----------------|-------------------------------------------------------|
| `number`      | integer        | Sibling index at this level (Ember+: last OID digit). |
| `identifier`  | string         | Stable machine name; unique among siblings.           |
| `path`        | string         | Dot-joined identifiers from root (`router.matrix.a`). |
| `oid`         | string         | Numeric OID, dot-joined (`"1.1.2"`). Unique per tree. |
| `description` | string \| null | Human label; may be null.                             |
| `isOnline`    | boolean        | Ember+ native; other protocols emit `true`.           |
| `access`      | string         | `"none"` \| `"read"` \| `"write"` \| `"readWrite"`.   |
| `children`    | array          | Child elements in order. Leaves emit `[]`, not null.  |

All pointers elsewhere in the schema (`templateReference`, `basePath`,
`parametersLocation`, …) resolve against `oid`, never `path`.

---

## 3. Per-type shapes

Additional keys appended after the common header. Types with fewer concepts
emit `null` for keys they don't use.

### 3.1 Node (extra keys)

| Key                 | Type           | Notes                                          |
|---------------------|----------------|------------------------------------------------|
| `templateReference` | string \| null | OID of a Template element. Null if no template.|
| `schemaIdentifiers` | string \| null | LF-joined list, per Ember+ spec §6.            |

### 3.2 Parameter (extra keys)

| Key                  | Type                | Notes                                                   |
|----------------------|---------------------|---------------------------------------------------------|
| `type`               | string              | `integer` \| `real` \| `string` \| `boolean` \| `enum` \| `octets` \| `trigger` |
| `value`              | any \| null         | Current value. Type matches `type`.                     |
| `default`            | any \| null         | Default value.                                          |
| `minimum`            | any \| null         | Min (integer/real).                                     |
| `maximum`            | any \| null         | Max (integer/real).                                     |
| `step`               | any \| null         | Step size.                                              |
| `unit`               | string \| null      | e.g. `"dB"`, `"Hz"`.                                    |
| `format`             | string \| null      | printf-style, e.g. `"%.2f"`.                            |
| `factor`             | integer \| null     | Display factor.                                         |
| `formula`            | string \| null      | Ember+ formula expression.                              |
| `enumeration`        | string \| null      | LF-joined labels (Ember+ native form).                  |
| `enumMap`            | array \| null       | `[{key, value, masked?}]`. See §4.                      |
| `streamIdentifier`   | integer \| null     | If set, value is streamed.                              |
| `streamDescriptor`   | object \| null      | `{format, offset}` when stream uses CollectionAggregate.|
| `templateReference`  | string \| null      | OID of a Template.                                      |
| `schemaIdentifiers`  | string \| null      | LF-joined.                                              |

### 3.3 Matrix (extra keys)

| Key                         | Type              | Notes                                                      |
|-----------------------------|-------------------|------------------------------------------------------------|
| `type`                      | string            | `oneToN` \| `oneToOne` \| `nToN`                           |
| `mode`                      | string            | `linear` \| `nonLinear`                                    |
| `targetCount`               | integer           | Declared count.                                            |
| `sourceCount`               | integer           | Declared count.                                            |
| `maximumTotalConnects`      | integer \| null   | nToN only.                                                 |
| `maximumConnectsPerTarget`  | integer \| null   | nToN only.                                                 |
| `parametersLocation`        | string \| null    | OID of gain parameter root. See §4 `--gain`.               |
| `gainParameterNumber`       | integer \| null   | Child parameter number inside each connection node.        |
| `labels`                    | array             | `[{basePath, description}]`. Pointer form; see §4.         |
| `targets`                   | array             | `[{number}]`. Declared target indices.                     |
| `sources`                   | array             | `[{number}]`. Declared source indices.                     |
| `connections`               | array             | `[{target, sources[], operation, disposition, locked}]`.   |
| `targetLabels`              | object \| null    | `{<description>: {<number>: "<label>"}}`. Multi-level — outer key = `labels[i].description`. Only when `--labels=inline\|both`. |
| `sourceLabels`              | object \| null    | Same shape as `targetLabels`. Multi-level.                 |
| `targetParams`              | object \| null    | `{<number>: {<paramKey>: <value>}}`. Single-level. `--gain=inline\|both`.|
| `sourceParams`              | object \| null    | Same shape as `targetParams`.                              |
| `connectionParams`          | object \| null    | `{"<target>.<source>": {<paramKey>: <value>}}`. Composite key because the wire subtree is two-deep (connections/target/source/param). |

**Connection disposition** (per spec §5.1 p.42): `tally` | `modified` |
`pending` | `locked`. **Operation** on set: `absolute` | `connect` |
`disconnect`.

**Matrix-type constraints** (per spec §5.1):

| Matrix type | connection.sources[] length constraint                 |
|-------------|--------------------------------------------------------|
| `oneToN`    | exactly 1 source per target (replaces on connect).     |
| `oneToOne`  | exactly 1 source, and source is used at most once.     |
| `nToN`      | `0 .. maximumConnectsPerTarget` sources per target.    |

### 3.4 Function (extra keys)

| Key         | Type    | Notes                                                |
|-------------|---------|------------------------------------------------------|
| `arguments` | array   | `[{name, type}]`. Types same vocabulary as Parameter.|
| `result`    | array   | `[{name, type}]`. Zero-length tuple = void.          |

Invocation and invocation result are wire-only — not part of the export.

### 3.5 Template (top-level `templates[]`)

Templates, if present, sit at the top of the export as a sibling of
`root.children[]`:

```
{
  "root": { "identifier": "…", "children": [ … ] },
  "templates": [
    {
      "number": 1,
      "oid": "0.1",
      "identifier": "genericInput",
      "description": "Standard input schema",
      "template": { <full element shape with its own children[]> }
    }
  ]
}
```

`templates[]` is **omitted** when `--templates=inline` (the default) —
references are inflated onto the referring elements and the array is dropped.

---

## 4. Three mode flags

Consumers and providers differ in what they emit. The export CLI supports
three independent flags, each with values `inline | pointer | both` and
default `inline`.

| Flag          | Covers                          | `inline` (default)                                                  | `pointer`                                                       | `both`                              |
|---------------|---------------------------------|---------------------------------------------------------------------|-----------------------------------------------------------------|-------------------------------------|
| `--templates` | `templateReference`             | Inflate fields onto element; drop `templates[]`; drop `templateReference`. | Keep `templates[]` and `templateReference`; no inflation.        | Both: inflate AND keep `templates[]`. |
| `--labels`    | Matrix labels                   | Resolve `basePath` → `targetLabels{}` / `sourceLabels{}`.          | `labels[{basePath, description}]` only; omit inline maps.        | Both forms present.                 |
| `--gain`      | Matrix gain / connection params | Resolve `parametersLocation` + `gainParameterNumber` → `targetParams{}` / `sourceParams{}` / `connectionParams{}`. | `parametersLocation` + `gainParameterNumber` only.              | Both forms present.                 |

### Provider patterns this supports

| Provider style | Label pattern                                              | Gain pattern                                             | Flag choice                  |
|----------------|------------------------------------------------------------|----------------------------------------------------------|------------------------------|
| Textbook       | `basePath` points at a label node subtree.                 | `parametersLocation` points at a gain-param subtree.     | `inline` resolves both.      |
| In-matrix      | Labels as Parameter children under the matrix itself.      | Gain params as Parameter children under the matrix.      | `inline` resolves both.      |
| Split          | Both: `basePath` AND an in-matrix label subtree duplicate. | `parametersLocation` AND in-matrix gain params.          | `both` to preserve evidence. |
| Pointer-only   | Consumer doesn't want to chase basePath — UI handles it.   | Same.                                                    | `pointer`.                   |

Pointers are always emitted as dot-joined numeric OID strings
(e.g. `"3.0.3000"`) — never bare integers or slash-joined paths.

### 4.5 enumMap (universal)

Every enum Parameter carries BOTH:

- `enumeration`: LF-joined label list (Ember+ native).
- `enumMap`: `[{key: string, value: integer, masked?: boolean}]`.

`enumMap` is the portable form across all protocols. Derivation:

| Protocol | Source                             | Sequential? |
|----------|------------------------------------|-------------|
| ACP1     | `item_list` (comma-delimited)      | Yes — `value = position`. |
| ACP2     | pid 15 options (72-byte records)   | Can be non-sequential.    |
| Ember+   | Native `enumMap`, or derived from `enumeration` (position = value). | Mixed. |

Masked items (smh pattern: leading `~` in label): strip the `~`, emit
`{key, value, masked: true}`. Consumers render masked entries as
non-selectable.

---

## 5. Stream merge

Ember+ carries streamed values in APPLICATION[6] `StreamCollection` frames,
each entry `{streamIdentifier, value}`. The export pipeline:

1. Walks the tree and records `streamIdentifier` on each owning Parameter.
2. On each `StreamCollection` frame, writes `entry.value` onto the Parameter
   whose `streamIdentifier` matches.
3. Emits **no separate `streams[]` section** — the merged value lives on the
   Parameter's `value` field; `streamIdentifier` and `streamDescriptor` stay
   as metadata.

This matches Ember+ spec §3.1 behaviour and keeps the canonical shape
protocol-agnostic.

---

## 6. Compliance events

Every non-trivial resolution during export writes an event to the connection's
compliance profile. Events help debug providers that bend the spec:

Events are grouped by source. Full authoritative list in
`internal/protocol/emberplus/compliance/profile.go`.

### Resolver (matrix-scoped, templates, gain, labels)

| Event                                       | When it fires                                                      |
|---------------------------------------------|--------------------------------------------------------------------|
| `template_absorbed`                         | `--templates=inline`: template content inflated into referring element. |
| `template_reference_unresolved`             | `templateReference` points at an OID not present in `templates[]` (or cross-type mismatch). |
| `labels_absorbed`                           | `--labels=inline`: at least one label level absorbed; label Nodes removed from tree. |
| `matrix_label_basepath_unresolved`          | `labels[i].basePath` does not resolve to a walked Node.            |
| `matrix_label_none`                         | Matrix ships no `labels[]` or empty array. Informational.          |
| `matrix_label_description_empty`            | `labels[i].description` blank — resolver keys by basePath instead. |
| `matrix_label_level_mismatch`               | Two label levels expose different target / source counts.          |
| `gain_absorbed`                             | `--gain=inline`: parametersLocation subtree absorbed.              |
| `matrix_parameters_location_unresolved`     | `parametersLocation` does not resolve to a walked Node.            |

### Enum / field handling

| Event                    | When it fires                                                          |
|--------------------------|------------------------------------------------------------------------|
| `enum_double_source`     | Parameter has both `enumeration` and native `enumMap`; counts differ. |
| `enum_masked_item`       | Enum option carries smh `~` mask prefix; stripped and flagged.         |
| `enum_map_derived`       | Canonical `enumMap` synthesised from legacy LF-joined `enumeration`.   |
| `field_inferred`         | Canonical field synthesised from protocol-specific source (e.g. `type` inferred from `value` CHOICE). |

### Streams

| Event                                  | When it fires                                                    |
|----------------------------------------|------------------------------------------------------------------|
| `stream_id_collision_no_descriptor`    | Two Parameters share a `streamIdentifier` with at least one missing `streamDescriptor`. Spec §7 forbids — provider bug. |

### Wire-level tolerance (decoder fall-backs)

| Event                               | When it fires                                                     |
|-------------------------------------|-------------------------------------------------------------------|
| `non_qualified_element`             | Node / Parameter / Matrix / Function delivered without RelOID path. |
| `multi_frame_reassembly`            | S101 FlagFirst/FlagLast chain observed.                            |
| `invocation_success_default`        | InvocationResult omitted `success` field (spec p.92).              |
| `connection_operation_default`      | Connection omitted `operation` (default `absolute`, p.89).         |
| `connection_disposition_default`    | Connection omitted `disposition` (default `tally`, p.89).          |
| `contents_set_omitted`              | Contents arrived without UNIVERSAL SET envelope (p.85).            |
| `tuple_direct_ctx`                  | Tuple as bare CTX[0] items (no enclosing SEQUENCE).                |
| `element_collection_bare`           | ElementCollection inlined without APP[4] wrapper.                  |
| `unknown_tag_skipped`               | Vendor-private APP / CTX tag encountered.                          |

### Future / cross-protocol

| Event                      | When it fires                                                      |
|----------------------------|--------------------------------------------------------------------|
| `proto_cross_empty`        | Cross-protocol mapper emitted null for a key the target requires.  |
| `auto_pointer_downgrade`   | (future) inline form exceeded size threshold; pointer emitted.     |
| `field_lossy_down`         | Wire value narrower than canonical field accepts.                  |

### Protocol-family events

Plugin-specific deviations absorbed on the fly. Source memories list the
spec references for each.

| Event                              | Protocol         | Fires when |
|------------------------------------|------------------|------------|
| `probel_level_scoped_matrix`       | Probel SW-P-02/08+ | One Matrix emitted per (matrixId, level) pair. Informational. |
| `probel_protect_state_demoted`     | Probel           | 4-state `protect.state` collapsed to boolean `locked` on cross-protocol bridge. |
| `probel_protect_owner_missing`     | Probel           | PROTECT_DEVICE_NAME_REQUEST returned no owner string. |
| `probel_tie_line_multi_level`      | Probel           | Source-level bitmap has >1 bit set; `sources[]` can't express it fully. |
| `probel_bad_source_connect`        | Probel           | Crosspoint reports `valid:false` (bad-source bit). |
| `probel_extended_used`             | Probel           | Extended command path taken (address > basic range). |
| `probel_label_variant_missing`     | Probel           | Requested label width absent from Name Identifier Flags. |
| `probel_salvo_capacity_exceeded`   | Probel SW-P-08+  | Staged connection count > 128 per salvo group. |
| `probel_salvo_cleared_no_data`     | Probel SW-P-08+  | GO returned status=02 on empty salvo. |
| `probel_labels_absent`             | Probel SW-P-02   | Protocol cannot supply labels; consumer must source out-of-band. |
| `tsl_reserved_bit_set`             | TSL UMD          | v3.1 CTRL bit 6 or bits 8–14 of v5.0 CONTROL are non-zero (reserved). |
| `tsl_version_mismatch`             | TSL UMD v4.0     | VBC minor version != 0; unknown XDATA layout. |
| `tsl_checksum_fail`                | TSL UMD v4.0     | CHKSUM doesn't match 2's-complement mod 128. |
| `tsl_control_data_undefined`       | TSL UMD          | v4.0 CTRL.6=1 or v5.0 CONTROL bit 15 set. |
| `tsl_unknown_display_index`        | TSL UMD          | DMSG arrives for an INDEX not yet modelled. |
| `tsl_broadcast_received`           | TSL UMD v5.0     | SCREEN=0xFFFF or INDEX=0xFFFF broadcast. |
| `tsl_charset_transcode`            | TSL UMD v5.0     | UTF-16LE label transcoded to canonical UTF-8. |
| `tsl_label_length_mismatch`        | TSL UMD v3.1     | Packet arrives with != 16 data bytes. |

Events are counted, not unique-keyed — useful for "how many template
resolutions happened?" rather than "what was the last one?"

---

## 7. Capture pipeline

`--capture <dir>` on any consumer plugin writes a raw frame file plus
the decoded tree, named after the wire framing of the plugin in use:

| File                        | Written for  | Content                                                          |
|-----------------------------|--------------|------------------------------------------------------------------|
| `raw.acp1.jsonl`            | ACP1         | Every ACP1 frame tx+rx (UDP datagrams or TCP/AN2), one JSON line.|
| `raw.an2.jsonl`             | ACP2         | Every AN2 frame tx+rx (including its ACP2 payload).              |
| `raw.s101.jsonl`            | Ember+       | Every S101 frame tx+rx, one JSON line each: `{ts, dir, hex}`.    |
| `glow.json`                 | Ember+ only  | Decoded Glow tree snapshot after initial walk completes.         |
| `tree.json`                 | All 3        | Canonical-shape export (post-resolution) using current mode flags.|

Replay unit tests under `tests/unit/{acp1,acp2,emberplus}/` consume these fixtures:

| Test                   | What it verifies                                        |
|------------------------|---------------------------------------------------------|
| `s101_replay`          | Re-framing `raw.s101.jsonl` yields identical byte stream (Ember+).|
| `ber_roundtrip`        | BER decode → re-encode of each frame matches byte-exact (Ember+).|
| `glow_decode`          | `raw.s101.jsonl` → Glow tree == `glow.json` (Ember+).           |
| `export_shape`         | Glow tree → canonical export == `tree.json` (Ember+).           |
| `encoder_compliance`   | Encoder emits byte-exact frames for known operations.           |
| `an2_replay` (ACP2)    | Re-framing `raw.an2.jsonl` yields identical byte stream.        |

Fixtures come from two live providers:

| Port | Device                | Role                     |
|------|-----------------------|--------------------------|
| 9092 | TinyEmberPlus Router  | Reference textbook shape.|
| 9000 | Dufour / smh emulator | Real-world deviations.   |

---

## 8. Planned extensions

Schema additions already documented in `elements/*.md` but not yet in
`internal/export/canonical/*.go`. They land with the plugin that needs
them; the conformance test (step 2c) covers what's documented AND
implemented — additions land in tandem.

| Addition                                   | Where documented                               | Plugin that lands it        |
|--------------------------------------------|------------------------------------------------|-----------------------------|
| `Matrix.matrixId` / `Matrix.level`         | [`elements/matrix.md`](elements/matrix.md) §Matrix element — extensions | Probel SW-P-02 consumer |
| `Matrix.supportedLabelVariants`            | [`elements/matrix.md`](elements/matrix.md) §Matrix element — extensions | Probel SW-P-08+ consumer |
| `Connection.protect` (4-state + owner)     | [`elements/matrix.md`](elements/matrix.md) §Connection subobject — extensions | Probel SW-P-02 consumer |
| `Connection.valid` (bad-source bit)        | [`elements/matrix.md`](elements/matrix.md) §Connection subobject — extensions | Probel SW-P-02 consumer |
| `Salvo` element (first-class)              | [`elements/salvo.md`](elements/salvo.md)       | Probel SW-P-08+ consumer    |
| `Parameter.preset` (DHS preset/value split)| (pending doc)                                  | Ember+ provider (part of reactive model) |

All keys stay additive — no breaking change to existing Ember+ representation.
