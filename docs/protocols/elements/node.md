# Node

A **Node** is a container. It carries metadata and a list of children. Every
tree's root is a Node. Identity, matrix containers, function libraries, and
anything else that groups siblings are Nodes.

## Field reference

| Key                 | Type            | Wire meaning (codec dev)                                                           | UI hint (webui dev)                                          |
|---------------------|-----------------|------------------------------------------------------------------------------------|--------------------------------------------------------------|
| `number`            | integer         | Sibling index at this level; last digit of this node's OID.                        | Not shown to user; used as key for ordering.                 |
| `identifier`        | string          | Machine name, unique among siblings. ASCII, no spaces.                             | Tree label when `description` is null.                       |
| `path`              | string          | Dot-joined identifiers from root (`router.matrix.main`).                           | Breadcrumb trail.                                            |
| `oid`               | string          | Numeric OID (`"1.1"`). Authoritative address for every reference in the tree.      | Key used when addressing via REST API.                       |
| `description`       | string \| null  | Human-readable label. Optional.                                                    | Primary label shown in UI; fall back to `identifier`.        |
| `isOnline`          | boolean         | `true` = live, `false` = offline/disconnected subtree (see spec §4.1.1).           | Grey out subtree if false.                                   |
| `access`            | string          | `"none"` / `"read"` / `"write"` / `"readWrite"`. Ember+ maps the bitmask here.     | Always `read` for nodes — UI treats nodes as navigational.   |
| `children`          | array           | Ordered list of child elements. Leaves emit `[]` (never `null`).                   | Tree expansion.                                              |
| `templateReference` | string \| null  | OID of a Template element in `templates[]` (only under `--templates=pointer\|both`). | Tooltip: "this node follows template X".                    |
| `schemaIdentifiers` | string \| null  | LF-joined list of schema URIs (spec §6). Identifies conformance to a profile.      | Show in node properties panel.                               |

## Sample 1 — root Node with identity subtree

```json
{
  "number": 0,
  "identifier": "EmberPlus",
  "path": "EmberPlus",
  "oid": "0",
  "description": "Ember+ demo provider",
  "isOnline": true,
  "access": "read",
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": [
    {
      "number": 0,
      "identifier": "identity",
      "path": "EmberPlus.identity",
      "oid": "0.0",
      "description": "Device identity",
      "isOnline": true,
      "access": "read",
      "templateReference": null,
      "schemaIdentifiers": null,
      "children": [
        { "number": 0, "identifier": "product", "path": "EmberPlus.identity.product",
          "oid": "0.0.0", "description": "Product name",
          "isOnline": true, "access": "read",
          "type": "string", "value": "EmberPlus Server",
          "default": null, "minimum": null, "maximum": null, "step": null,
          "unit": null, "format": null, "factor": null, "formula": null,
          "enumeration": null, "enumMap": null,
          "streamIdentifier": null, "streamDescriptor": null,
          "templateReference": null, "schemaIdentifiers": null,
          "children": [] },
        { "number": 1, "identifier": "company", "path": "EmberPlus.identity.company",
          "oid": "0.0.1", "description": null,
          "isOnline": true, "access": "read",
          "type": "string", "value": "BY-RESEARCH SPRL",
          "default": null, "minimum": null, "maximum": null, "step": null,
          "unit": null, "format": null, "factor": null, "formula": null,
          "enumeration": null, "enumMap": null,
          "streamIdentifier": null, "streamDescriptor": null,
          "templateReference": null, "schemaIdentifiers": null,
          "children": [] },
        { "number": 2, "identifier": "version", "path": "EmberPlus.identity.version",
          "oid": "0.0.2", "description": null,
          "isOnline": true, "access": "read",
          "type": "string", "value": "1.0.0",
          "default": null, "minimum": null, "maximum": null, "step": null,
          "unit": null, "format": null, "factor": null, "formula": null,
          "enumeration": null, "enumMap": null,
          "streamIdentifier": null, "streamDescriptor": null,
          "templateReference": null, "schemaIdentifiers": null,
          "children": [] }
      ]
    }
  ]
}
```

## Sample 2 — grouping Node with schemaIdentifiers

A Node that publishes it conforms to a vendor schema profile.

```json
{
  "number": 1,
  "identifier": "audio",
  "path": "EmberPlus.audio",
  "oid": "0.1",
  "description": "Audio processing block",
  "isOnline": true,
  "access": "read",
  "templateReference": null,
  "schemaIdentifiers": "de.l-s-b.emberplus.schemas.audio.input\nde.l-s-b.emberplus.schemas.audio.gain",
  "children": []
}
```

## Sample 3 — Node with templateReference (pointer mode)

When `--templates=pointer` or `--templates=both`, a node can point at a
Template by OID instead of inflating fields. Here Node `inputs.ch1` reuses
the `genericInput` template at OID `9.0`.

```json
{
  "number": 0,
  "identifier": "ch1",
  "path": "EmberPlus.inputs.ch1",
  "oid": "2.0.0",
  "description": "Input channel 1",
  "isOnline": true,
  "access": "read",
  "templateReference": "9.0",
  "schemaIdentifiers": null,
  "children": []
}
```

Under `--templates=inline` the reference is resolved and the children from
the template are inflated into `children[]`; `templateReference` is then
omitted.

## Sample 4 — offline subtree

Indicates the subtree is currently unavailable (e.g. card removed, network
partition). Consumers continue to display the node but must not send
commands into it.

```json
{
  "number": 3,
  "identifier": "slotC",
  "path": "EmberPlus.slotC",
  "oid": "0.3",
  "description": "Card slot C (removed)",
  "isOnline": false,
  "access": "read",
  "templateReference": null,
  "schemaIdentifiers": null,
  "children": []
}
```

## Provider variations

| Pattern                         | Notes                                                                                     |
|---------------------------------|-------------------------------------------------------------------------------------------|
| Minimal provider                | Emits only `number` + `identifier` + `children`; description/schemaIdentifiers omitted. Consumer treats missing keys as `null`. |
| Deep identity tree              | Some providers (Lawo) use a nested identity tree with `hardware`, `firmware`, `license` sub-nodes. |
| Flat root                       | Other providers (small embedded devices) put parameters directly at the root without any intermediate Node. |
| Broken `isOnline`               | Some providers leave `isOnline` unset on transient loss and rely on the TCP disconnect instead — treat missing as `true`. |

## Consumer handling

- **Walk strategy**: `GetDirectory` on root, then recurse on each child
  container. `isOnline:false` subtrees are still walked (for structure) but
  parameter values in them stay `null`.
- **Template resolution** (`--templates=inline`): before emitting the node,
  look up `templateReference` in the collected `templates[]`; if found,
  merge the template's `children[]` into this node's `children[]`. Fires
  `template_inlined`; if the OID is missing, fires `template_unresolved` and
  leaves the reference intact.
- **schemaIdentifiers**: never parsed by the generic consumer; surfaced
  verbatim for schema-aware adapters (bus bridge, cross-protocol mapper).

## See also

- [`../schema.md`](../schema.md) — common header rules, `--templates` flag.
- [`parameter.md`](parameter.md) — what goes inside a typical Node.
- [`template.md`](template.md) — how the `templates[]` top-level array is structured.
