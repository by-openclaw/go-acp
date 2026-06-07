# `export` / `import` — full reference

Linked from the main runbook §10. This file is the canonical reference for the export/import round-trip on Ember+, including the full scope of [#461](https://github.com/by-openclaw/go-acp/issues/461) (R4).

## Today on `main` (PARTIAL)

Round-trip works for a subset of Glow types; gaps tracked in [#461](https://github.com/by-openclaw/go-acp/issues/461). The verb signatures match the rest of the catalogue (`--port`, `--out`, etc.).

### `--format` matrix

| `--format` | Extension | Typical use |
|---|---|---|
| `json` | `.json` | machine-readable; lossless re-import; default |
| `yaml` | `.yaml` / `.yml` | human-edit-friendly; same schema as JSON |
| `csv` | `.csv` | spreadsheet-friendly tabular view; flattens nested structure (matrix labels become indexed rows) |

### Happy

```powershell
# Full tree export — every supported Glow type
.\bin\dhs.exe consumer emberplus export 127.0.0.1 --port 9100 --format json --out demo.json
.\bin\dhs.exe consumer emberplus export 127.0.0.1 --port 9100 --format yaml --out demo.yaml
.\bin\dhs.exe consumer emberplus export 127.0.0.1 --port 9100 --format csv  --out demo.csv

# Re-import on a fresh producer
.\bin\dhs.exe consumer emberplus import demo.yaml --port 9100
```

### Partial export (current shape — header preserved)

A partial export — e.g. only the `identity` subtree — keeps the document header (root identifier, dtdVersion, capture timestamp) so the importer can locate the slot:

```yaml
# demo-identity-only.yaml
device:
  identifier: dhs-emberplus-integration
  dtdVersion: 2.60
  capturedAt: 2026-05-17T13:22:00Z
elements:
  - oid: "1.0"
    path: "identity"
    type: node
    children:
      - oid: "1.0.1"
        path: "identity.product"
        type: parameter
        value: "dhs-emberplus-integration"
      # ... only identity subtree, no matrices / functions / types
```

Header rules:
- `device.identifier` MUST match the importer's target — else fail with `validation: device-identifier-mismatch`
- `device.dtdVersion` warns on mismatch but does not block (post #459 isOnline + post #466 reroot make this safe in most cases)
- `device.capturedAt` is informational only

## R4 [#461](https://github.com/by-openclaw/go-acp/issues/461) — full round-trip scope

Round-trip on the integration demo:

1. `export 127.0.0.1 --port 9100 --format yaml --out /tmp/dump.yaml` against the running demo
2. Stop demo, start a fresh producer pointed at an empty manifest
3. `import /tmp/dump.yaml --port 9100`
4. `walk` → assert byte-identical canonical tree (or canonical-equivalent after deterministic normalisation)

Coverage required across all Glow types:

| Type | Notes |
|---|---|
| Node | + `isOnline` + `schemaIdentifiers` + `templateReference` (DTD 2.30+) |
| Parameter | every `ParameterType` — Integer, Real, Boolean, String, Octets, Enum + `factor` + `step` + `format` + `default` + `min/max` + `enumMap` |
| Matrix | every type (oneToOne / oneToN / nToN / dynamic) with labels (Pri + Sec), source/target/connection params (gain layers), connections, locked targets |
| Function | tuple shape (args + results) |
| Stream Parameter | `streamIdentifier` id=0 + id>0 |
| Template / QualifiedTemplate | per ADR-0024 federation |

## `--dry-run` import + per-type tally + `--scope` (extends R4 — see [#461 comment](https://github.com/by-openclaw/go-acp/issues/461#issuecomment-4470752301))

### Dry-run import

`dhs consumer emberplus import <file> --dry-run` parses the dump, resolves every node/parameter/matrix/function path against the live tree, and emits the diff — but sends zero wire traffic. Output format matches `--ensure dryrun` from [#475](https://github.com/by-openclaw/go-acp/issues/475) (R14) so operators see one consistent diff format across `set` / `matrix` / `invoke` / `import`.

### Per-Glow-type pass/fail/skip classification

```
type=Node              count=42  ok=42  fail=0  skip=0
type=Parameter         count=189 ok=187 fail=2  skip=0
  fail: oneToN.target.0.gain (parse: factor missing)
  fail: identity.dtdVersion (parse: not in DTD 2.60 enum)
type=Matrix            count=4   ok=4   fail=0  skip=0
type=Function          count=6   ok=6   fail=0  skip=0
type=StreamParameter   count=3   ok=3   fail=0  skip=0
type=Template          count=0   ok=0   fail=0  skip=0
```

- `skip` covers types the producer/consumer doesn't yet support (e.g. Template before federation lands)
- `fail` is the bug surface — silent drops are unacceptable

### `--scope <path>` for sample exports

```powershell
.\bin\dhs.exe consumer emberplus export ... --scope identity         --out identity.yaml
.\bin\dhs.exe consumer emberplus export ... --scope mtx.oneToN       --out matrix-oneToN.yaml
.\bin\dhs.exe consumer emberplus export ... --scope params.layers    --out params-layers.yaml
.\bin\dhs.exe consumer emberplus export ... --scope func.builtin.recallSalvo --out func-recallSalvo.yaml
```

Use cases:
- Build per-type test fixtures → one file per Glow type under `internal/emberplus/testdata/protocol_types/`
- Operator handoff — "send me your identity tree" without dumping 65535² of matrix data
- CI regression fixtures — small, type-focused dumps that exercise one code path

`--scope` composes with `--format` and `--out`.

## Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| target file unwritable | `export ... --out /no/perm/dump.yaml` | `export: open /no/perm/dump.yaml: permission denied` | 1 |
| input file missing | `import does-not-exist.yaml` | `import: open does-not-exist.yaml: no such file` | 1 |
| device identifier mismatch | `import other-device.yaml --port 9100` | `validation: device-identifier-mismatch: expected "dhs-emberplus-integration", got "other-device"` | 2 |
| dry-run on existing path that doesn't resolve | `import dump.yaml --dry-run` (with stale path) | per-type tally with `fail:` rows; exit 0 (dry-run never mutates) | 0 |
| connection refused on import | `import ... --port 9999` | s101 framing error | 1 |
| `--scope` with unknown subtree | `export ... --scope bogus` | `validation: --scope: object not found at path "bogus"` | 2 |

## Refs

- [ADR-0022 — Card data model](../../../../docs/adr/0022-card-data-model.md)
- [ADR-0024 — Federation mirror vs virtual frame](../../../../docs/adr/0024-federation-mirror-and-virtual-frame.md)
- [ADR-0025 — Per-connector definition of done](../../../../docs/adr/0025-per-connector-definition-of-done.md)
- [#461](https://github.com/by-openclaw/go-acp/issues/461) — round-trip + dry-run + scope
- [#475](https://github.com/by-openclaw/go-acp/issues/475) — R14 `--ensure` (shared diff format)
- [#442](https://github.com/by-openclaw/go-acp/issues/442) — multi-sheet XLSX matrix export (separate)
