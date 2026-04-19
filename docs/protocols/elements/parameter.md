# Parameter

A **Parameter** is a leaf value. Ember+ defines seven value types; ACP1 and
ACP2 map to this same union, emitting `null` for keys they don't support.
Parameters are what a UI actually renders controls for: inputs, sliders,
dropdowns, text fields, toggles, buttons.

## Value types

| `type`      | Concrete value | Examples                                               |
|-------------|----------------|--------------------------------------------------------|
| `integer`   | int64          | Gain dB×10, sample counts, indices.                    |
| `real`      | float64        | Frequency Hz, ratio, normalised level.                 |
| `string`    | UTF-8 string   | Labels, free-form description fields.                  |
| `boolean`   | bool           | On/off toggles.                                        |
| `enum`      | int (index into `enumMap`) | Mode selectors, input sources, EQ curves.  |
| `octets`    | base64 bytes   | Binary blobs, opaque vendor data.                      |
| `trigger`   | n/a            | Momentary action — set only, no persistent value.      |

## Field reference

| Key                 | Type            | Wire meaning (codec dev)                                                                                                                                | UI hint (webui dev)                                                            |
|---------------------|-----------------|---------------------------------------------------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------------|
| *common header*     |                 | See [node.md](node.md).                                                                                                                                 | Same.                                                                          |
| `type`              | string          | One of the seven above. Drives BER tag selection on encode.                                                                                             | Drives widget choice: number input vs dropdown vs checkbox vs text vs button. |
| `value`             | any \| null     | Current value. Typed per `type`. `null` when never confirmed from provider.                                                                             | Bind to widget's displayed value.                                              |
| `default`           | any \| null     | Default value; `setDefValue` restores this.                                                                                                             | "Reset" button behaviour.                                                      |
| `minimum`           | any \| null     | Lower bound (integer/real).                                                                                                                             | Slider/number input `min` attribute.                                           |
| `maximum`           | any \| null     | Upper bound (integer/real).                                                                                                                             | Slider/number input `max` attribute.                                           |
| `step`              | any \| null     | Increment granularity.                                                                                                                                  | Slider/number input `step` attribute.                                          |
| `unit`              | string \| null  | Display-only suffix ("dB", "Hz", "%").                                                                                                                  | Append after numeric value.                                                    |
| `format`            | string \| null  | printf-style format string ("%.2f"). Ember+ native.                                                                                                     | Format `value` for display.                                                    |
| `factor`            | integer \| null | Divide wire value by factor to get display value. E.g. factor=10 + integer gain=-36 displays as -3.6 dB.                                                | Apply before rendering: `displayed = value / factor`.                          |
| `formula`           | string \| null  | Two-line expression: wire→display on line 1, display→wire on line 2. See Ember+ Formulas PDF. Evaluated by consumer if supplied.                        | If present, use instead of `factor` for display.                               |
| `enumeration`       | string \| null  | LF-joined label list (Ember+ native form). Position = value.                                                                                            | Obsolete for UI — use `enumMap` instead.                                       |
| `enumMap`           | array \| null   | `[{key, value, masked?}]`. The portable form, always present on enums regardless of protocol.                                                           | Dropdown `<option>` list. Skip `masked:true` entries from selectable options.  |
| `streamIdentifier`  | integer \| null | If non-null, provider streams this value via StreamCollection frames. See [stream.md](stream.md).                                                       | Show live indicator; disable manual set.                                       |
| `streamDescriptor`  | object \| null  | `{format, offset}` when provider uses CollectionAggregate stream (multiple parameters share one binary payload).                                         | Not shown; consumed by stream decoder.                                         |
| `templateReference` | string \| null  | OID of a Template whose shape this Parameter follows.                                                                                                   | Tooltip "follows template X".                                                  |
| `schemaIdentifiers` | string \| null  | LF-joined schema URIs.                                                                                                                                  | Shown in properties panel.                                                     |

## Sample 1 — integer with range + unit

A classic bounded integer, e.g. preset slot selector.

```json
{
  "number": 0,
  "identifier": "preset",
  "path": "audio.ch1.preset",
  "oid": "1.0.0",
  "description": "Active preset slot",
  "isOnline": true,
  "access": "readWrite",
  "type": "integer",
  "value": 3,
  "default": 1,
  "minimum": 1,
  "maximum": 32,
  "step": 1,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 2 — real gain in dB with formula

The wire carries an integer in tenths of a dB (-960 .. 120). The consumer
applies the formula to display -96.0 .. 12.0 dB.

```json
{
  "number": 1,
  "identifier": "gain",
  "path": "audio.ch1.gain",
  "oid": "1.0.1",
  "description": "Channel gain",
  "isOnline": true,
  "access": "readWrite",
  "type": "real",
  "value": -6.0,
  "default": 0.0,
  "minimum": -96.0,
  "maximum": 12.0,
  "step": 0.1,
  "unit": "dB",
  "format": "%.1f",
  "factor": 10,
  "formula": "it/10\nit*10",
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 3 — enum with enumMap, including masked items

Source selector. The provider advertises 8 slots, two of which are reserved
(masked) — the UI must show them greyed or hide them entirely.

```json
{
  "number": 2,
  "identifier": "source",
  "path": "audio.ch1.source",
  "oid": "1.0.2",
  "description": "Input routing",
  "isOnline": true,
  "access": "readWrite",
  "type": "enum",
  "value": 1,
  "default": 0,
  "minimum": null,
  "maximum": null,
  "step": null,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": "Off\nMic 1\nMic 2\nLine\n~Reserved1\n~Reserved2\nAES\nDante",
  "enumMap": [
    { "key": "Off",   "value": 0 },
    { "key": "Mic 1", "value": 1 },
    { "key": "Mic 2", "value": 2 },
    { "key": "Line",  "value": 3 },
    { "key": "Reserved1", "value": 4, "masked": true },
    { "key": "Reserved2", "value": 5, "masked": true },
    { "key": "AES",   "value": 6 },
    { "key": "Dante", "value": 7 }
  ],
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 4 — string with length limit

Writable label/name field. `maximum` here is reused to cap UTF-8 length.

```json
{
  "number": 3,
  "identifier": "label",
  "path": "audio.ch1.label",
  "oid": "1.0.3",
  "description": "User label",
  "isOnline": true,
  "access": "readWrite",
  "type": "string",
  "value": "Studio A Mic",
  "default": "Input",
  "minimum": null,
  "maximum": 32,
  "step": null,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 5 — boolean toggle

```json
{
  "number": 4,
  "identifier": "mute",
  "path": "audio.ch1.mute",
  "oid": "1.0.4",
  "description": "Channel mute",
  "isOnline": true,
  "access": "readWrite",
  "type": "boolean",
  "value": false,
  "default": false,
  "minimum": null,
  "maximum": null,
  "step": null,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 6 — octets (binary blob)

Vendor-specific opaque data. Base64 on the JSON side, raw bytes on the wire.

```json
{
  "number": 5,
  "identifier": "vendorBlob",
  "path": "system.vendorBlob",
  "oid": "9.0.5",
  "description": "Opaque vendor data",
  "isOnline": true,
  "access": "read",
  "type": "octets",
  "value": "AQIDBAUGBwgJCg==",
  "default": null,
  "minimum": null,
  "maximum": 256,
  "step": null,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 7 — trigger (momentary action)

No persistent value — any setValue on a trigger fires the action once.
UI renders as a button. `value` is always `null` on read.

```json
{
  "number": 6,
  "identifier": "reboot",
  "path": "system.reboot",
  "oid": "9.0.6",
  "description": "Reboot device",
  "isOnline": true,
  "access": "write",
  "type": "trigger",
  "value": null,
  "default": null,
  "minimum": null,
  "maximum": null,
  "step": null,
  "unit": null,
  "format": null,
  "factor": null,
  "formula": null,
  "enumeration": null,
  "enumMap": null,
  "streamIdentifier": null,
  "streamDescriptor": null,
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Sample 8 — streamed meter (VU level)

Provider sends periodic StreamCollection frames at ~30 Hz. The Parameter
declares `streamIdentifier`; the latest merged value lives in `value`.

```json
{
  "number": 7,
  "identifier": "meter",
  "path": "audio.ch1.meter",
  "oid": "1.0.7",
  "description": "Peak level",
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
  "streamDescriptor": { "format": 0, "offset": 0 },
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

`streamDescriptor.format` is one of 14 values from Ember+ `StreamFormat`
(signed/unsigned int 8/16/32/64 BE+LE, float 32/64 BE+LE). See
[`stream.md`](stream.md).

## Provider variations

| Pattern                                  | Notes                                                                                   |
|------------------------------------------|-----------------------------------------------------------------------------------------|
| Integer with `factor`, no `formula`      | Common. Consumer applies `value / factor` to display.                                   |
| Integer with `formula`, no `factor`      | Rich provider (Lawo Nova, Riedel). Consumer evaluates expression per Ember+ Formulas PDF. |
| Enum native `enumMap`                    | Provider announces `enumMap` directly (extended Ember+).                                |
| Enum derived from `enumeration` only     | Bulk of providers. Consumer derives `enumMap` by splitting on `\n`, value = position.   |
| Masked enum items                        | smh convention: label prefixed with `~`. Consumer strips `~`, sets `masked:true`.       |
| Enum with non-sequential values (ACP2)   | Consumer trusts the wire `options` list — values may skip.                              |
| String with no `maximum`                 | Consumer accepts any length; UI may truncate at ~256 for display safety.                |
| Missing `default`                        | Very common on meters/read-only params. `null` leaves "reset" disabled in UI.            |
| Trigger typed as `integer` with range `0..0` | Anti-pattern. Consumer detects and coerces to `type:"trigger"`; fires `field_inferred`.|

## Consumer handling

- **Value confirmation**: `value` starts `null` from cache; becomes live
  after a `getValue`, a `getObject` walk, or a subscribed announcement.
  Stream-backed parameters are live as soon as the first StreamCollection
  frame merges.
- **enumMap derivation**: when the provider sends only `enumeration`, the
  consumer splits on `\n` and sets `value = position, key = label`. If the
  label starts with `~`, strip it and set `masked: true`.
- **factor vs formula**: if `formula` is present, it wins over `factor`.
  Log `field_inferred` if the consumer falls back to plain integer display
  because neither parsed cleanly.
- **access=write-only**: consumer must not schedule a `getValue` on those —
  wastes a round-trip and provider returns an error.
- **Compliance events**: `enum_masked_item`, `enum_double_source`,
  `enum_duplicate_label`, `field_lossy_down`, `field_inferred`.

## See also

- [`../schema.md`](../schema.md) — common header, 3 mode flags, stream merge, compliance events.
- [`stream.md`](stream.md) — streamed Parameter mechanics.
- [`node.md`](node.md) — what parents a Parameter typically sits under.
- Ember+ Formulas PDF — formula evaluation rules.
