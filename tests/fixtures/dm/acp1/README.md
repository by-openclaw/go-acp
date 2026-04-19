# ACP1 DM fixtures

Per-card, per-firmware ACP1 captures.

## Expected shape

```
acp1/
└── <card-name>/              ← CDV08v06, RRS18, SFR18, …
    ├── CHANGELOG.md          ← per-card firmware change log
    └── <firmware-version>/
        ├── meta.json
        ├── wire.jsonl        ← UDP datagrams (Synapse) or TCP/AN2 ACP1 frames
        └── tree.json
```

`<card-name>` matches the ACP1 identity block's `Card name` field verbatim (no spaces — replace with hyphens).

`<firmware-version>` comes from the ACP1 identity block's `Card SW rev` field.

## Known test devices

- `10.6.239.113` — Synapse Simulator, 4 cards present across 31 slots (memory: `project_test_device`)

## Status

Empty. Populated when `acp extract` (#36.b) ships.
