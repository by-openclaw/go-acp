# Ember+ integration-test source-of-truth

Version-controlled DM cards + manifest assembled into the spec-clean
Ember+ provider used by `internal/emberplus/integration/` Go tests, the
operator runbook at [internal/emberplus/docs/runbook.md](../../docs/runbook.md),
and the capture fixtures under [internal/emberplus/testdata/protocol_types/](../protocol_types/).

Per ADR-0025 deliverable #6, the DM files live **inside the protocol
folder** so they ship as part of the lift-to-own-repo unit. The
runtime `.cache/dm/emberplus/` directory at the repo root is a
gitignored derived cache; nothing here depends on it.

## Layout

```
internal/emberplus/testdata/integration-test/
├── README.md                      ← this file
├── manifest/
│   └── emberplus-integration.json ← Device → Frame → Slot manifest (ADR-0022)
└── dm/
    └── emberplus/
        ├── identity-strict@1.0.0.json    ← product / company / version / DTD
        ├── oneToOne-strict@1.0.0.json    ← 16×16, Pri+Sec labels, source-exclusive
        ├── oneToN-strict@1.0.0.json      ← 16×16, Pri+Sec labels, locked target
        ├── nToN-strict@1.0.0.json        ← 16×16, Pri+Sec, multi-src per tgt
        ├── dynamic-strict@1.0.0.json     ← 128×128 declared, 16×16 sparse
        ├── functions-strict@1.0.0.json   ← setLock/listLocks/storeSalvo/...
        └── glow-types-strict@1.0.0.json  ← every ParameterType + streams id=0|>0
```

## Serve from this layout

```powershell
dhs producer emberplus serve `
    --manifest internal/emberplus/testdata/integration-test/manifest/emberplus-integration.json `
    --cache-dir internal/emberplus/testdata/integration-test `
    --port 9100
```

`--cache-dir` is the root of the cache tree; `manifest.BuildExport` resolves
each slot's `dm` field against `<cache-dir>/dm/emberplus/<dm>.json`. The
default `.cache` value points at the gitignored runtime cache; pointing
it here serves the source-of-truth fixtures directly.

## Regenerate

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\emberplus\gen-emberplus-demo-dms.ps1
```

The script writes to this directory directly. Any drift between the
script's output and the checked-in files is a regression — diff to
catch it.

## Refs

- [ADR-0022](../../../../docs/adr/0022-card-data-model.md) — Card data model (Device / Frame / Slot / DM)
- [ADR-0025](../../../../docs/adr/0025-per-connector-definition-of-done.md) — Per-connector definition of done; deliverable #6 (replay fixtures under testdata/)
- [Operator runbook](../../docs/runbook.md) (in progress, tracked by #460)
- [Capture fixtures](../protocol_types/) (in progress)
