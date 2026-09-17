# TinyEmber+ Router — real Ember+ capture fixture

Full `walk --capture` triple from a **TinyEmber+ Router** over Ember+
(S101/TCP). ADR-0020 Bucket 5 per-device decoder oracle. It complements the
DHD console fixture: the DHD has no Functions/Matrices-with-labels-write,
this router exposes **3 matrices + 3 functions** — the Function/RPC and
writable-matrix surface the DHD couldn't exercise.

## Provenance
- Device: Tiny Ember+ Router (identity `Tiny Ember+ Router@1.6.2`).
- Host: `10.6.239.113:9092` (lab, router/matrix port).
- Captured: 2026-06-24 via `dhs consumer emberplus walk … --port 9092 --capture`.
- Frames: 331 (rx 279, tx 52). Decodes clean — 0 errors, 0 invariants.

## Protocol surface (vs DHD)
- **Matrices (3):** `router.oneToN.matrix` (targetCount 200), `router.nToN.matrix`
  (4), `router.dynamic.matrix` (2000). Non-zero counts confirm the
  connection-only-delta-preserves-MatrixContents fix holds on a second device.
- **Functions (3):** `router.functions.add`, `router.functions.doNothing`,
  `router.functions.licensing.enterLicenseKey`.
- Templates / Streams: none on this instance (covered by the integration-test
  DM fixtures + the provider loopback stream test).

## Verified live during capture (2026-06-24)
- `invoke router.functions.add --args 3,5` → `result: [8]` (Function/RPC + InvocationResult).
- `matrix router.oneToN.matrix --target 5 --sources 3 --op absolute` → ok.
- `matrix router.nToN.matrix --target 1 --sources 2 --op connect` → ok.
- `matrix router.oneToN.matrix … --op connect` correctly REJECTED (oneToN cardinality, max 1 source).

## Files
| file | what |
|------|------|
| `raw.s101.jsonl` | raw S101 wire frames — decoder input |
| `glow.json` | decoded Glow tree (provider view) |
| `tree.json` | canonical object tree (consumer view) |

## Used by
- `tinyember_capture_test.go` — replays `raw.s101.jsonl` through the codec
  (clean decode + frame count) and asserts the tree carries the 3 matrices
  (non-zero targetCounts) + the functions.
