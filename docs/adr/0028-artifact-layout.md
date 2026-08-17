# ADR-0028 — Artifact layout: one deterministic home per artifact, keyed by IP

- **Status**: proposed (owner-agreed design, 2026-08-17 session)
- **Extends**: ADR-0020 (capture + fixture layout), ADR-0022 (card data model)
- **Issue**: #703

## Context

DMs, manifests, export CSVs, tree renders, wire captures and diagnostic
logs each grew their own ad-hoc destinations ("a souk"): exports landed
wherever `--out` pointed, manifests were keyed by renameable device
names, and nothing prevented two devices from writing the same file.
The owner requires: no mismatch, no accidental override, no re-reading
of this decision in a few weeks.

## Decision

### 1. Four buckets by lifecycle

```
.cache/                                  MACHINE — regenerable, gitignored
├── dm/<proto>/<Model@SwRev>.json        schema of a card TYPE (shared; never per-device)
├── manifest/<proto>/<ip>.json           inventory anchor, ONE per device
└── logs/<proto>/<ip>/<verb>.log         disposable diagnostics (--log default)

snapshots/                               OPERATOR — state, source-of-truth candidates
└── <proto>/<ip>/                        one folder per device; all its facets inside
    ├── params.csv                       Tree/DM facet (export --device)
    ├── xpoint.csv src.csv dst.csv level.csv        Matrix facets
    ├── lock.csv cat-src.csv cat-dst.csv             control-plane-exclusive facets
    └── tree.json | tree.txt             tree render — a VIEW of state, same folder

captures/                                EVIDENCE — wire truth, gitignored (holds LOGIN → secret)
└── <proto>/<ip>/<verb>-<scope>-<utcstamp>.jsonl

internal/<proto>/testdata/integration-test/   COMMITTED — curated copies serving as oracles
```

### 2. Keys

- **Folder + manifest key = IP.** Unique and static in the plant;
  names are renameable and duplicable. The RM sentinel `0.0.0.0` is a
  valid key. A rename changes one metadata field, never a path.
- **FQDN/hostname**: recorded as an optional manifest metadata field
  now (devops direction); NOT a key. If DNS becomes authoritative
  later, migration is a re-key script, not a schema change.
- **Redundant controllers**: ONE manifest keyed by the primary IP;
  every endpoint listed inside in registration order; secondary IPs
  get no file of their own. Lookup by secondary IP = endpoint scan
  across manifests.
- **DM stays type-keyed and proto-filtered**: `dm/<proto>/<Model@SwRev>.json`.
  Same filename across protos (e.g. `dm/acp2/CONVERT IP@6.7.4.json` vs
  `dm/cerebrum-nb/CONVERT IP@6.7.4.json`) is the dual-oracle match
  pair — deliberately diffable, never merged. All units of one
  card+firmware share one DM per proto; endpoints/sub-devices are the
  manifest's job (this is why a second unit overwriting the DM is
  correct, not a collision).

### 3. Facet-capability matrix (a missing facet file is a protocol fact)

| Connector | Folder class | Facets |
|---|---|---|
| cerebrum-nb RM (`0.0.0.0`) | control plane | xpoint, src, dst, level, **lock, cat-src, cat-dst** |
| cerebrum-nb physical router | matrix | xpoint, src, dst, level (labels r/o) |
| cerebrum-nb device | Tree/DM | params |
| probel-sw08p | matrix | xpoint, src, dst, level, **lock** (in-protocol protect) |
| probel-sw02p | matrix | xpoint (single matrix/level) |
| emberplus matrix | matrix | xpoint, matrix-properties, labels — **no lock (glow has none)** |
| acp1 / acp2 / emberplus params / osc | Tree/DM | params |
| tsl | push | event log (no snapshot) |
| amwa (parked) | bridge | registry views |

Lock/protect on cerebrum is Route-Master business (owner rule: the RM
IS the control plane); the wire tolerating DEST_LOCK reads on routers
(live 2026-08-16) does not create a facet.

### 4. Defaults and overrides

- `--out` / `--out-dir` / `--in-dir` **omitted or empty → the
  deterministic home above**, derived from the addressed target.
  Never the current directory.
- Explicit `--out`/`--out-dir`/`--in-dir` remain as overrides for
  dedicated trees (staging, ops repos).
- `import` defaults to reading exactly where `export` writes.

### 5. Partial scope (already contractual; restated once)

- File-level: a facet file absent from the folder is out of scope.
- Row-level: a file holding a subset of rows converges only those;
  absent rows are never touched. Splitting a facet file into parts
  and importing each independently is supported by construction.
- Per-protocol authority rules (e.g. ember nToN file-authoritative-
  per-target) keep their documented semantics.

### 6. Efficiency: schema once, state on demand

- **DM = schema** (capture once per Model@SwRev): `extract` runs the
  identity probe first (2–3 obtains); if the DM file exists, it stops
  and reports the cache hit — zero walk. Full walk only for an unseen
  Model@SwRev or `--refresh`.
- **Snapshot = state**: `export` always walks — its job is
  point-in-time truth.
- `get`/`set`/`watch` never walk.

### 7. Capture self-description (evidence-grade)

Line one of every `--capture` JSONL is a meta record: exact CLI
invocation (credentials redacted), binary version + SHA256, protocol,
target, verb, UTC start. Filename: `<verb>-<scope>-<utcstamp>.jsonl`.
The same header applies to `--log` files.

### 8. Manifest roles (all three, not just emulation)

1. Consumer fast path — resolve host → sub-devices → DM without
   re-walking.
2. Fleet inventory — the Ansible-readable answer to "what is at this
   IP" (endpoints incl. redundancy, name, fqdn, DM refs).
3. Producer emulation, where a provider exists. cerebrum-nb stays
   consumer-only (docs/provider.md); its decoder/consumer regression
   is covered by capture replay (`validate`) + the fake-WS test
   server, not a provider.

## Verification (three layers, oracle-per-tier)

1. **CI, no wire**: unit tests pin the path composer — for each
   proto × verb × artifact, the computed default path equals the
   table above. Layout drift fails the build.
2. **Ansible verify play per connector (tier 2/3)**: capture + export
   + extract against the vendor emulator / real device; assert file
   locations, capture meta record, `import --check` = would_change 0
   right after export, and the extract DM cache hit.
3. This ADR's tables are the single acceptance checklist both layers
   implement.

## Consequences / migration

- Manifest writers (all connectors) re-key from device-name slug to
  `<proto>/<ip>.json`; name moves inside as metadata. Legacy
  name-keyed manifests are read once and rewritten.
- Export/import/tree/capture/log verbs gain the default-home rule
  (explicit flags unchanged).
- `extract` gains the DM-exists skip + `--refresh`.
- Implementation order: manifest re-key → default homes → DM-skip.
  One unit each per ADR-0013/0014.
