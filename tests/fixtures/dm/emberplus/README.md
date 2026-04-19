# Ember+ DM fixtures

Per-provider, per-version Ember+ captures.

## Expected shape

```
emberplus/
└── <provider>/               ← TinyEmberPlus, Lawo, DHD, Calrec, …
    ├── CHANGELOG.md          ← per-provider version change log
    └── <version>/            ← provider release identifier
        ├── meta.json
        ├── wire.jsonl        ← S101-framed BER blobs
        └── tree.json
```

`<provider>` — vendor / product name as it appears in the Ember+ root Identity node (preserve casing).

`<version>` — provider release or build identifier. If the provider reports no version, use its listening port number.

## Known test devices

- `127.0.0.1:9092` — TinyEmberPlus (working, memory: `project_emberplus_devices`)
- `127.0.0.1:9000` — DHD-shaped tree
- `127.0.0.1:9090` — alternate

## Status

Empty. Populated when `acp extract` (#36.b) ships.

Existing legacy captures live at [tests/fixtures/emberplus/9000/](../../emberplus/9000/), [9090/](../../emberplus/9090/), [9092/](../../emberplus/9092/) — those stay put.
