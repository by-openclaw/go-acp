# Product fixture library

Per-product, per-firmware captures used by offline replay tests and by `acp diff` for cross-version regression tracking.

## One rule

**Generated, never hand-edited.** If a file here is wrong, fix the generator (`acp extract` #36.b, `acp diff` #36.c, or the plugin decoder) and regenerate.

## Layout

```
products/
└── <manufacturer>/<product>/<protocol>/
    ├── CHANGELOG.md        generated on each new version add
    └── <version>/
        ├── meta.json       identity + fingerprint + capture_tool build info
        ├── wire.jsonl      raw frames (replay source)
        └── tree.json       canonical export (replay destination)
```

A product folder can hold multiple `<protocol>` subfolders — one physical card sometimes exposes several interfaces (e.g. an Axon card speaking both ACP2 and Ember+). Each protocol has its own version lineage and its own CHANGELOG.

Full spec + schema: [docs/fixtures-products.md](../../../docs/fixtures-products.md).

## Status

Empty today. Populated as `acp extract` (#36.b) and `acp diff` (#36.c) ship and engineers capture real devices.

Legacy fixtures at `tests/fixtures/{acp1,acp2,emberplus}/<non-products-paths>` stay in place — tests keep using them.
