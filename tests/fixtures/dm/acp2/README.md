# ACP2 DM fixtures

Per-card, per-firmware ACP2 captures.

## Expected shape

```
acp2/
└── <card-name>/              ← DDB08, DDB10, …
    ├── CHANGELOG.md          ← per-card firmware change log
    └── <firmware-version>/
        ├── meta.json
        ├── wire.jsonl        ← AN2 frames (header + ACP2 payload)
        └── tree.json
```

`<card-name>` matches the ACP2 identity block's card-name property verbatim.

`<firmware-version>` comes from the ACP2 identity block's firmware-version property.

## Known test devices

- `10.41.40.195` — ACP2 VM, 2 slots (memory: `project_test_device`)

## Status

Empty. Populated when `acp extract` (#36.b) ships.

Existing legacy captures live at [bin/devices/captures/acp2/10.41.40.195/](../../../../bin/devices/captures/acp2/10.41.40.195/) and [tests/fixtures/acp2/](../../acp2/) — those stay put.
