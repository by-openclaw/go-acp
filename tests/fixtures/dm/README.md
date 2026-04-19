# Data Model fixture library

Per-device, per-firmware captures used by offline replay tests and by `acp diff` for cross-version regression tracking.

## One rule

**Generated, never hand-edited.** If a file here is wrong, fix the generator (`acp extract` #36.b, `acp diff` #36.c, or the plugin decoder) and regenerate.

## Layout

```
dm/
└── <proto>/<card-or-provider>/<version>/
    ├── meta.json      identity + fingerprint + source tool
    ├── wire.jsonl     raw frames (replay source)
    └── tree.json      canonical export (replay destination)

dm/<proto>/<card-or-provider>/CHANGELOG.md     generated on new version add
```

Full spec + schema: [docs/fixtures-dm.md](../../../docs/fixtures-dm.md).

## Status

Empty today. Populated as `acp extract` (#36.b) and `acp diff` (#36.c) ship and engineers capture real devices.

Legacy fixtures at `tests/fixtures/{acp1,acp2,emberplus}/<non-dm-paths>` stay in place — tests keep using them.
