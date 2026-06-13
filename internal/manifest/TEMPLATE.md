# Manifest + DM fixture template (the provider contract)

Every connector ships a **committed** fixture so the provider/emulator builds its
frame from the repo alone (no host dependency). Shape is fixed by
[`types.go`](types.go) + ADR-0022. Live at:

```
internal/<proto>/testdata/integration-test/
├── manifest/<device>.json          # device(controller) + frame + slots → DM refs
└── dm/<proto>/<Model@SwRev>.json   # one DM per card the slots expose
```

Run it: `dhs producer <proto> serve --manifest <…/manifest/<device>.json> --cache-dir <…/integration-test>`.

## manifest/<device>.json — template

```jsonc
{
  "device": {                       // the CONTROLLER (unit + its network face)
    "name": "<device-name>",
    "protocol": "<proto>",          // acp1 | acp2 | emberplus | probel-sw08p | ...
    "endpoints": [                  // 1 endpoint = single controller;
      { "ip": "0.0.0.0", "port": <port>, "transport": "tcp" }
      // 2 endpoints in this device = REDUNDANT CONTROLLER (ADR-0023)
    ]
  },
  "frames": [                       // one chassis per frame
    {
      "name": "<chassis-name>",
      "slots": [                    // each slot ATTACHES one DM = the card it exposes
        { "addr": <per-proto addr>, "dm": "<Model>@<SwRev>" }
      ]
    }
  ]
}
```

### `addr` is per-protocol (opaque key map)
| Protocol | `addr` shape | Notes |
|---|---|---|
| acp1 / acp2 | `{ "slot": N }` | slot 0 = rack controller card |
| emberplus | `{ "oid": "1.4.2" }` | grafts the DM subtree at that OID |
| probel-sw08p / sw02p | `{ "matrix": M, "level": L }` | matrix entity (ADR-0023) |

### `dm` reference
`"<Model>@<SwRev>"` → resolved against `<cache-dir>/dm/<proto>/<Model>@<SwRev>.json`.
For the committed fixture, `<cache-dir>` = `internal/<proto>/testdata/integration-test`.

## dm/<proto>/<Model@SwRev>.json — DM format

Flat shape (acp1/acp2 and any object-list protocol) — the producer reads this:
```jsonc
{
  "model": "<Model>",
  "sw_rev": "<SwRev>",
  "protocol": "<proto>",
  "objects": [
    { "slot": 0, "group": "...", "path": ["..."], "id": <n>, "label": "...",
      "kind": "string|number|enum|ipv4|node|...", "access": <bits>,
      "meta": { ... }, "min": ..., "max": ..., "step": ..., "default": ...,
      "enum_items": [...], "max_len": <n>, "value": { "kind": "...", ... } }
  ]
}
```
Canonical shape (emberplus) — `{ "model", "sw_rev", "protocol", "root": <canonical element>, "templates": [...] }`.

**Rules:** committed, flat format (never the old `device`+`slots[].objects[]` export
snapshot), sane size (no multi-MB blobs — trim to a representative card if a real DM
is huge), real device-collected values. `slot{x}.json` export blobs are deprecated.
Each connector's integration test must build the provider from THIS committed
manifest and assert `walk` returns the exposed objects — verified on a clean checkout.
