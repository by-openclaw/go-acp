# DHD console — real Ember+ capture fixture

Full `walk --capture` triple from a **real DHD audio console** over Ember+
(S101/TCP). This is the Ember+ entry of the **ADR-0020 Bucket 5** per-device
decoder-oracle library (amendment 2026-06-24): one full real-device capture
per protocol per device, committed to enrich the codec regression with
ground-truth bytes. `.gitignore` re-includes
`internal/**/testdata/fixtures/**/*.jsonl`; `.gitattributes` marks the dir
`-text` (byte-stable). It is the Tier‑1 decoder oracle (real device bytes
decode through our codec) and the regression fixture behind the matrix
walk‑storm / watch‑flood fixes.

## Provenance
- Device: DHD audio console (`Device.Identity.Product` / `Firmwareversion` in `tree.json`).
- Host: `10.107.2.210:9000` (lab).
- Captured: 2026-06-23, via `dhs consumer emberplus walk … --capture`.
- Frames: 12663 (rx 12558, tx 105). Decodes clean — 0 errors, 0 invariants.

## Files
| file | what |
|------|------|
| `raw.s101.jsonl` | raw S101 wire frames (`{ts,dir,hex,len}` per line) — the decoder input |
| `glow.json` | decoded Glow tree (provider view) |
| `tree.json` | canonical object tree (consumer view) — real DHD DM, incl. `Device.Routing` matrix |

## Notable structure
- `Device.Routing` — large matrix whose multi‑wave tally + re‑broadcast exposed
  the watch flood and the zero‑route GetDirectory spin (see
  `internal/emberplus/consumer` matrix tests).
- `Device.Identity.Product` / `Device.Identity.Firmwareversion` — identity probe inputs.

## Used by
- `dhd_capture_test.go` — replays `raw.s101.jsonl` through the codec (offline
  `Validate`) and asserts a clean decode + the matrix/identity are present.
