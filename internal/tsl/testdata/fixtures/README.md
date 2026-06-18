# TSL UMD Test Fixtures

Replay fixtures for the TSL UMD (v3.1 / v4.0 / v5.0) connector, per
ADR-0025 deliverable 8 (replay fixtures).

This directory is the TSL sibling of `internal/probel-sw02p/testdata/` —
same role: committed, real, minimal artifacts that let the codec and
integration tests run without a live tally source. Because TSL is a
tally/UMD **PUSH** protocol (not a matrix), there is no canonical tree to
export; the natural replay fixture is the **exact on-wire frame for each
version**, so the analog of probel's `exports/matrix_tree.json` is
`fixtures/golden_frames.jsonl`.

## Layout

```
internal/tsl/testdata/
├── fixtures/
│   ├── README.md            ← this file
│   └── golden_frames.jsonl  codec-encoded golden wire bytes, one per
│                            (version, transport) — the drift-guard target
└── protocol_types/          per-wire-type capture fixtures (frames.jsonl
                             + capture.pcapng + tshark.tree); see its
                             own README.md
```

## `fixtures/golden_frames.jsonl`

One JSON object per line:

| Field | Meaning |
| --- | --- |
| `version` | `v3.1` / `v4.0` / `v5.0` |
| `transport` | `udp` / `tcp-dle-stx` (v5.0 DLE/STX-wrapped TCP) |
| `description` | human scenario label |
| `hex` | lower-case hex of the wire bytes |

The committed samples:

| Version | Transport | Scenario |
| --- | --- | --- |
| v3.1 | UDP | 18-byte frame, addr 7, tally1+tally4, full brightness, `"PGM LIVE"` |
| v4.0 | UDP | v3.1 block + CHKSUM + VBC + 2-byte XDATA (both displays, all colours) |
| v5.0 | UDP | single-DMSG ASCII, screen 1, `"CAM 1"` |
| v5.0 | UDP | multi-DMSG UTF-16LE (`CAMÉRA` / `日本語` / `HOLA`) |
| v5.0 | TCP | DLE/STX wrapper + 0xFE byte-stuffing (SCREEN=0xFEFE) |

### NEVER hand-edited — generated from the real codec

The `hex` bytes are produced by the repo's own codec encode path
(`codec.V31Frame.Encode` / `codec.V40Frame.Encode` /
`codec.V50Packet.Encode`, the last wrapped via `codec.EncodeDLEFrame`).
The struct literals in
`internal/tsl/integration/fixture_test.go` (`buildGoldenFrames`) are the
source of truth for the *semantics*; the bytes are **derived, never
fabricated**.

Two guards in that file pin the fixture:

- `TestGoldenFrameFixtures` — drift-guard. Asserts the committed file is
  byte-identical to what the codec re-emits for the canonical sample set
  (the TSL sibling of probel's `TestMatrixTreeExportFixture`).
- `TestGoldenFramesRoundTrip` — decodes every committed frame back
  through `DecodeV31` / `DecodeV40` / `DLEStreamDecoder` + `DecodeV50`
  and asserts a clean parse with no deviation notes — proving the bytes
  are spec-conformant, not hand-fabricated.

Both carry `//go:build integration`, so a plain `go test ./...` never
runs them. After an intentional wire change, regenerate:

```
DHS_REGEN_FIXTURES=1 go test -tags integration \
  ./internal/tsl/integration/ -run TestGoldenFrameFixtures
```

then commit the updated `golden_frames.jsonl`.

## `protocol_types/`

Per-wire-type capture fixtures (one folder per version + frame-type),
consumed by `internal/tsl/codec/*_test.go` for byte-level round-trips and
by the Wireshark dissector cross-check. See `protocol_types/README.md` for
the folder convention and how to promote a live
`captures/tsl-vXX/<scenario>/frames.jsonl` into a committed fixture.

## Pairing with `captures/`

Live captures live LOCAL ONLY at `captures/tsl-vXX/<scenario>/
frames.jsonl` (gitignored, per ADR-0021). Trim + drop the relevant
frames under the matching `protocol_types/<version>/` folder to promote
them into a committed fixture. The golden-frame fixture here is the
codec-generated baseline; live captures complement it with real
vendor-emitted bytes (Miranda / Lawo VSM).
