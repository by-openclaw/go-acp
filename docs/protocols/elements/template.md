# Template

A **Template** is a reusable element shape. One Template defines a
sub-tree (Node / Parameter / Matrix / Function with all keys populated);
any number of Nodes in the main tree can point at it via
`templateReference` to reuse that shape without re-declaring every field.
The Ember+ spec defines Template and QualifiedTemplate elements; in the
canonical export, Templates live at the top level in a `templates[]` array
alongside `root`.

Templates are an **export-time** concern. The three-valued flag
`--templates=inline|separate|both` (default `inline`) controls whether the
consumer inflates references onto referring elements, leaves them as
pointers, or emits both forms.

## Field reference — QualifiedTemplate entry

Each entry in `templates[]`:

| Key           | Type           | Wire meaning (codec dev)                                                                              | UI hint (webui dev)                                                    |
|---------------|----------------|-------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------|
| `number`      | integer        | Sibling index at the templates-collection level; last digit of `oid`.                                 | Not shown; order only.                                                 |
| `oid`         | string         | Dot-joined numeric OID of this template. Unique across the entire tree.                               | Used when `templateReference` on some Node points here.                |
| `identifier`  | string         | Machine name of the template, e.g. `genericInput`.                                                    | Tooltip / admin panel label.                                           |
| `description` | string \| null | Human description of what the template models.                                                        | Admin panel description.                                               |
| `template`    | object         | The full element shape the template models — Node / Parameter / Matrix / Function — with its children.| Not rendered directly; source for inflation.                           |

The embedded `template` uses the normal element shape for its declared type.
Recursively, it can contain further `templateReference` keys referring to
other templates.

## Three modes

| Mode        | `templates[]` in output | `templateReference` on referring element | Referring element's `children[]`             | Consumer behaviour                                                                                                  |
|-------------|-------------------------|------------------------------------------|----------------------------------------------|---------------------------------------------------------------------------------------------------------------------|
| `inline`    | **omitted**             | **omitted**                              | inflated with the template's children        | Resolves every `templateReference`; emits only the final inflated tree. Fires `template_inlined` per resolution.    |
| `separate`  | present                 | present                                  | left empty (or as declared)                  | No inflation. Webui/post-processor does the inflation on demand. Fires nothing.                                     |
| `both`      | present                 | present                                  | inflated                                      | Does the work AND preserves the pointer form. Auditable. Fires `template_inlined` per resolution.                   |

If a `templateReference` points at an OID that isn't present in
`templates[]`, the consumer fires `template_unresolved` and leaves the
reference intact (no inflation possible).

## Sample — `separate` mode

Two Nodes in the main tree reuse the same `genericInput` shape.

```json
{
  "root": {
    "number": 0,
    "identifier": "EmberPlus",
    "path": "EmberPlus",
    "oid": "0",
    "description": "Demo",
    "isOnline": true,
    "access": "read",
    "templateReference": null,
    "schemaIdentifiers": null,
    "children": [
      {
        "number": 0,
        "identifier": "inputs",
        "path": "EmberPlus.inputs",
        "oid": "0.0",
        "description": "Input channels",
        "isOnline": true,
        "access": "read",
        "templateReference": null,
        "schemaIdentifiers": null,
        "children": [
          {
            "number": 0,
            "identifier": "ch1",
            "path": "EmberPlus.inputs.ch1",
            "oid": "0.0.0",
            "description": "Channel 1",
            "isOnline": true,
            "access": "read",
            "templateReference": "9.0",
            "schemaIdentifiers": null,
            "children": []
          },
          {
            "number": 1,
            "identifier": "ch2",
            "path": "EmberPlus.inputs.ch2",
            "oid": "0.0.1",
            "description": "Channel 2",
            "isOnline": true,
            "access": "read",
            "templateReference": "9.0",
            "schemaIdentifiers": null,
            "children": []
          }
        ]
      }
    ]
  },
  "templates": [
    {
      "number": 0,
      "oid": "9.0",
      "identifier": "genericInput",
      "description": "Standard audio input shape",
      "template": {
        "number": 0,
        "identifier": "input",
        "path": "input",
        "oid": "9.0",
        "description": "Input template",
        "isOnline": true,
        "access": "read",
        "templateReference": null,
        "schemaIdentifiers": null,
        "children": [
          {
            "number": 0,
            "identifier": "gain",
            "path": "input.gain",
            "oid": "9.0.0",
            "description": "Input gain",
            "isOnline": true,
            "access": "readWrite",
            "type": "real",
            "value": null,
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
          },
          {
            "number": 1,
            "identifier": "mute",
            "path": "input.mute",
            "oid": "9.0.1",
            "description": "Channel mute",
            "isOnline": true,
            "access": "readWrite",
            "type": "boolean",
            "value": null,
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
        ]
      }
    }
  ]
}
```

## Sample — same tree in `inline` mode

`templates[]` array is dropped; each referring Node's `children[]` is
inflated from the template; `templateReference` is stripped.

```json
{
  "root": {
    "number": 0,
    "identifier": "EmberPlus",
    "path": "EmberPlus",
    "oid": "0",
    "description": "Demo",
    "isOnline": true,
    "access": "read",
    "templateReference": null,
    "schemaIdentifiers": null,
    "children": [
      {
        "number": 0,
        "identifier": "inputs",
        "path": "EmberPlus.inputs",
        "oid": "0.0",
        "description": "Input channels",
        "isOnline": true,
        "access": "read",
        "templateReference": null,
        "schemaIdentifiers": null,
        "children": [
          {
            "number": 0,
            "identifier": "ch1",
            "path": "EmberPlus.inputs.ch1",
            "oid": "0.0.0",
            "description": "Channel 1",
            "isOnline": true,
            "access": "read",
            "templateReference": null,
            "schemaIdentifiers": null,
            "children": [
              {
                "number": 0,
                "identifier": "gain",
                "path": "EmberPlus.inputs.ch1.gain",
                "oid": "0.0.0.0",
                "description": "Input gain",
                "isOnline": true,
                "access": "readWrite",
                "type": "real",
                "value": null,
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
              },
              {
                "number": 1,
                "identifier": "mute",
                "path": "EmberPlus.inputs.ch1.mute",
                "oid": "0.0.0.1",
                "description": "Channel mute",
                "isOnline": true,
                "access": "readWrite",
                "type": "boolean",
                "value": null,
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
            ]
          },
          { "...": "ch2 with same inflated children, new oids" }
        ]
      }
    ]
  }
}
```

Key rules during inflation:

- OIDs and paths of inflated children are **rewritten** so they descend
  from the referring Node, not from the template's OID. E.g. template
  `9.0.0.gain` becomes `0.0.0.0.gain` under `ch1`.
- Everything else (`type`, `minimum`, `maximum`, `default`, …) is copied
  verbatim from the template.
- `value` is NOT carried from the template — the inflated child starts
  with `null` until the consumer confirms it. Each concrete instance has
  its own live state.
- `isOnline` is inherited from the referring Node; inflated children take
  the Node's value.

## Sample — `both` mode

Combines the two: `templates[]` is present AND every referring Node is
inflated. Used when downstream consumers may need either form, or for
debugging provider compliance. No additional ruleset — produces the
inlined tree from the inline sample plus the `templates[]` array from the
separate sample.

## Provider variations

| Pattern                                   | Notes                                                                                          |
|-------------------------------------------|------------------------------------------------------------------------------------------------|
| No templates declared                     | Majority of providers. Consumer emits `templates[]:[]` or omits the key entirely.               |
| QualifiedTemplate only                    | Common. Uses absolute OID form.                                                                 |
| Plain Template (not qualified)            | Allowed; consumer normalises to QualifiedTemplate shape.                                        |
| Circular `templateReference`              | Malformed; consumer detects cycles during inflation and fires `template_unresolved`.            |
| Template of type Matrix                   | Rare but legal — whole matrix structure (type, counts, placeholder targets) reused.             |
| Template containing nested templateReference | Legal; consumer resolves iteratively (fixed-point) in `inline` mode.                         |

## Consumer handling

- **Discovery**: templates typically announced by the provider at walk time
  as top-level QualifiedTemplate elements alongside the root Node.
- **Inflation** (`inline`/`both`): build a template table keyed by OID; do
  a second pass over the tree resolving `templateReference`. Bail out of a
  cycle on second encounter of the same OID, fire `template_unresolved`,
  leave reference intact.
- **OID rewriting**: when inflating, the template's internal OIDs are
  remapped so each inflated child's OID descends from the referring Node.
  The consumer must update the `path` field to match.
- **`value` reset**: inflated instances start with `value:null`. They are
  populated via the normal getValue / announcement flow against their
  concrete OIDs.
- **Compliance events**: `template_inlined`, `template_unresolved`.

## See also

- [`../schema.md`](../schema.md) — `--templates` flag values.
- [`node.md`](node.md) — `templateReference` field on Nodes.
- [`parameter.md`](parameter.md) — `templateReference` field on Parameters.
