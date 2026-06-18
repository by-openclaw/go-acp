# SW-P-02 Test Fixtures

Replay fixtures + canonical exports for the SW-P-02 (Probel single-matrix
single-level) connector, per ADR-0025 deliverable 8 (replay fixtures).

This directory is the SW-P-02 sibling of `internal/acp1/testdata/` — same
layout, same role: committed, real, minimal artifacts that let the codec
and integration tests run without a live matrix.

## Layout

```
internal/probel-sw02p/testdata/
├── fixtures/
│   └── README.md            ← this file
├── exports/
│   └── matrix_tree.json     canonical JSON export of the matrix tree
│                            the loopback provider serves (8×8 1-to-N)
└── protocol_types/          per-wire-type capture fixtures (frames.jsonl
                             + capture.pcapng + tshark.tree); see its
                             own README.md
```

## `exports/matrix_tree.json`

Canonical-tree JSON export of the exact `canonical.Export` the
in-process provider serves in the loopback integration test
(`internal/probel-sw02p/integration/`). One `router` node containing a
single `oneToN` / `linear` matrix sized 8 destinations × 8 sources, with
one label base path.

It is produced by the repo's own canonical-JSON writer
(`export.WriteCanonicalJSON`) — **never hand-edited**. The integration
test `TestMatrixTreeExportFixture` asserts the committed file is
byte-identical to what the writer emits for the served tree, so the
fixture can never drift from the emulator. After an intentional tree
change, regenerate it:

```
DHS_REGEN_FIXTURES=1 go test -tags integration \
  ./internal/probel-sw02p/integration/ -run TestMatrixTreeExportFixture
```

JSON only by design — matrix CSV export/import is out of scope for this
connector.

## `protocol_types/`

Per-command wire-trace fixtures (one folder per command byte / verb),
consumed by `internal/probel-sw02p/codec/*_test.go` for byte-level
round-trips and by the Wireshark dissector cross-check. See
`protocol_types/README.md` for the folder convention and how to promote
a live `captures/probel-sw02p/<scenario>/frames.jsonl` into a committed
fixture.

## Pairing with `captures/`

Live captures live LOCAL ONLY at
`captures/probel-sw02p/<scenario>/frames.jsonl` (gitignored per
ADR-0021). Trim + drop the relevant frames under the matching
`protocol_types/<cmd>/` folder to promote them into a committed fixture.
