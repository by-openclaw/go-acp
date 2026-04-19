# Stream

Streams are not a standalone top-level element. In the canonical export a
"stream" is a **Parameter with a non-null `streamIdentifier`** whose value
is updated via periodic Ember+ `StreamCollection` frames rather than
individual `setValue` announcements. The merged value lives on the
Parameter's `value` field — the export emits **no separate `streams[]`
section**.

Streams are how Ember+ carries high-rate data like VU meters, loudness,
spectrum analysers, RF levels — values that update 10–100 Hz, where sending
one Glow message per update would saturate the link.

## Wire model

```
StreamCollection (APPLICATION[6])
  ├─ StreamEntry (APPLICATION[5]) { streamIdentifier: 4101, value: -18.3 }
  ├─ StreamEntry (APPLICATION[5]) { streamIdentifier: 4102, value: -24.1 }
  ├─ StreamEntry (APPLICATION[5]) { streamIdentifier: 4103, value: -12.7 }
  └─ ...
```

One `StreamCollection` frame carries multiple `StreamEntry` records, each
keyed by a `streamIdentifier` that was previously advertised on a
Parameter. The consumer indexes Parameters by `streamIdentifier` at walk
time and on each frame writes `entry.value` onto the matching Parameter.

### Two encodings

| Mode                     | How the value is packed                                                                                     | Parameter metadata                                                  |
|--------------------------|-------------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------|
| Individual               | `StreamEntry.value` is a single typed BER value (integer / real / octet string).                            | `streamIdentifier` set; `streamDescriptor` **null**.                |
| CollectionAggregate      | `StreamEntry.value` is one **binary blob** carrying packed values for multiple Parameters at fixed offsets. | `streamIdentifier` shared; `streamDescriptor:{format, offset}` set. |

CollectionAggregate is how a provider ships a whole meter bridge (16 VU
meters) in one StreamEntry rather than 16 separate entries.

## `streamDescriptor` — CollectionAggregate decoding

`streamDescriptor: { format, offset }`

| Key       | Type    | Meaning                                                                                                |
|-----------|---------|--------------------------------------------------------------------------------------------------------|
| `format`  | integer | One of 14 values from Ember+ `StreamFormat` — see below.                                               |
| `offset`  | integer | Byte offset into the StreamEntry's binary value where THIS Parameter's sample begins.                  |

### StreamFormat values (Ember+ spec §5.3)

| ID  | Format            | Size (bytes) | Endianness |
|-----|-------------------|--------------|------------|
| 0   | unsignedInt8      | 1            | n/a        |
| 1   | unsignedInt16BE   | 2            | big        |
| 2   | unsignedInt16LE   | 2            | little     |
| 3   | unsignedInt32BE   | 4            | big        |
| 4   | unsignedInt32LE   | 4            | little     |
| 5   | unsignedInt64BE   | 8            | big        |
| 6   | unsignedInt64LE   | 8            | little     |
| 7   | signedInt8        | 1            | n/a        |
| 8   | signedInt16BE     | 2            | big        |
| 9   | signedInt16LE     | 2            | little     |
| 10  | signedInt32BE     | 4            | big        |
| 11  | signedInt32LE     | 4            | little     |
| 12  | ieeeFloat32BE     | 4            | big        |
| 13  | ieeeFloat32LE     | 4            | little     |
| 14  | ieeeFloat64BE     | 8            | big        |
| 15  | ieeeFloat64LE     | 8            | little     |

(Naming varies slightly in the wild; consumer accepts any of the above.)

## Sample 1 — individual-encoded meter Parameter

A single VU meter. No `streamDescriptor`.

```json
{
  "number": 0,
  "identifier": "meter",
  "path": "audio.ch1.meter",
  "oid": "1.0.0.7",
  "description": "Channel 1 peak level",
  "isOnline": true,
  "access": "read",
  "type": "real",
  "value": -18.3,
  "default": null,
  "minimum": -96.0,
  "maximum": 0.0,
  "step": null,
  "unit": "dBFS",
  "format": "%.1f",
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": 4101,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

Wire flow every ~30 ms:

```
StreamCollection {
  StreamEntry { streamIdentifier: 4101, value: -18.3 (real) }
  StreamEntry { streamIdentifier: 4102, value: -22.7 (real) }
  ...
}
```

Consumer looks up `4101 → Parameter at 1.0.0.7`, writes `-18.3` onto its
`value`.

## Sample 2 — CollectionAggregate for a 4-meter bus bridge

Four meters share one `streamIdentifier`; each has its own offset into the
packed binary payload.

```json
[
  {
    "number": 0,
    "identifier": "meterL",
    "path": "bus.meters.L",
    "oid": "1.2.0.0",
    "description": "Bus L peak",
    "isOnline": true,
    "access": "read",
    "type": "real",
    "value": -18.3,
    "default": null, "minimum": -96.0, "maximum": 0.0, "step": null,
    "unit": "dBFS", "format": "%.1f", "factor": null, "formula": null,
    "enumeration": null, "enumMap": null,
    "streamIdentifier": 9001,
    "streamDescriptor": { "format": 12, "offset": 0 },
    "templateReference": null, "schemaIdentifiers": null,
    "children": []
  },
  {
    "number": 1,
    "identifier": "meterR",
    "path": "bus.meters.R",
    "oid": "1.2.0.1",
    "description": "Bus R peak",
    "isOnline": true,
    "access": "read",
    "type": "real",
    "value": -19.7,
    "default": null, "minimum": -96.0, "maximum": 0.0, "step": null,
    "unit": "dBFS", "format": "%.1f", "factor": null, "formula": null,
    "enumeration": null, "enumMap": null,
    "streamIdentifier": 9001,
    "streamDescriptor": { "format": 12, "offset": 4 },
    "templateReference": null, "schemaIdentifiers": null,
    "children": []
  },
  {
    "number": 2,
    "identifier": "meterC",
    "path": "bus.meters.C",
    "oid": "1.2.0.2",
    "description": "Bus C peak",
    "isOnline": true,
    "access": "read",
    "type": "real",
    "value": -24.1,
    "default": null, "minimum": -96.0, "maximum": 0.0, "step": null,
    "unit": "dBFS", "format": "%.1f", "factor": null, "formula": null,
    "enumeration": null, "enumMap": null,
    "streamIdentifier": 9001,
    "streamDescriptor": { "format": 12, "offset": 8 },
    "templateReference": null, "schemaIdentifiers": null,
    "children": []
  },
  {
    "number": 3,
    "identifier": "meterSub",
    "path": "bus.meters.Sub",
    "oid": "1.2.0.3",
    "description": "Bus Sub peak",
    "isOnline": true,
    "access": "read",
    "type": "real",
    "value": -30.0,
    "default": null, "minimum": -96.0, "maximum": 0.0, "step": null,
    "unit": "dBFS", "format": "%.1f", "factor": null, "formula": null,
    "enumeration": null, "enumMap": null,
    "streamIdentifier": 9001,
    "streamDescriptor": { "format": 12, "offset": 12 },
    "templateReference": null, "schemaIdentifiers": null,
    "children": []
  }
]
```

Wire flow — one StreamEntry carries all four values packed at offsets
0/4/8/12 (format=12 = ieeeFloat32BE, 4 bytes each):

```
StreamCollection {
  StreamEntry {
    streamIdentifier: 9001,
    value: <16-byte blob: [L_float32BE][R_float32BE][C_float32BE][Sub_float32BE]>
  }
}
```

Consumer slices by `(format, offset)` for each Parameter sharing id `9001`.

## Merge semantics

On each `StreamCollection` frame:

| Entry format            | Consumer action                                                                                               |
|-------------------------|---------------------------------------------------------------------------------------------------------------|
| Individual value        | Find Parameter by `streamIdentifier`; write `entry.value` onto its `value`; emit `update` event upward.       |
| CollectionAggregate blob| For each Parameter sharing that `streamIdentifier`: slice blob at `streamDescriptor.offset` by `format`, decode, assign to Parameter `value`, emit `update` event upward. |

`value` on a stream-backed Parameter is **always** the latest merged
sample. The export's `value` field is a snapshot at the moment of export.

## Provider variations

| Pattern                                  | Notes                                                                                         |
|------------------------------------------|-----------------------------------------------------------------------------------------------|
| Individual StreamEntry per Parameter     | Simple providers. Slightly chattier wire.                                                     |
| CollectionAggregate blob                 | Efficient for dense meter grids. Requires consumers to handle `streamDescriptor`.              |
| Mixed encoding                           | Legal. Parameter-by-parameter: some with `streamDescriptor`, others without.                  |
| Provider declares `streamIdentifier` but never emits a frame | Treat value as `null`/stale; don't block.                                  |
| Frame with unknown `streamIdentifier`    | Consumer ignores silently; optionally log once.                                                |
| Frame arriving before walk completes     | Consumer buffers the last one; applies when matching Parameter appears.                        |

## Consumer handling

- **Indexing**: at walk time, build
  `streamIdentifier → [Parameter, streamDescriptor?]` map. Re-build on each
  tree structure change.
- **Merging**: handle `StreamCollection` frames in the read loop; update
  Parameter `value` in-place under the session lock.
- **Subscription**: no explicit subscribe is required for streams — the
  provider pushes StreamCollection frames as soon as the Parameter is
  discovered (on walk / GetDirectory). Consumer may need to send
  `Subscribe` on very old providers — treat as optional.
- **Rate control**: no rate limiting at the protocol level — the consumer
  processes whatever arrives. For UI, throttle visual refresh to ~30 Hz
  regardless of wire rate.
- **Compliance events**: `field_lossy_down` fires if the wire format is
  wider than the Parameter can represent (rare — both sides usually use
  float32 or int16).

## See also

- [`../schema.md`](../schema.md) — stream merge rule (§5).
- [`parameter.md`](parameter.md) — `streamIdentifier`, `streamDescriptor` field refs.
- Ember+ spec v2.50, §5.3 StreamCollection / StreamEntry / StreamDescription.
