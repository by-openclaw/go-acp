# DHD (port 9000) — Ember+ stream capture fixture

A `watch --streams-only --capture` trace from the DHD provider on
`10.6.239.113:9000` (Series52, manifest `52-7440`). ADR-0020 Bucket 5 oracle,
focused on the **Stream** surface that neither the DHD console (9092 absent)
nor the TinyEmber router exposed.

## Provenance
- Host: `10.6.239.113:9000` (plain provider port).
- Captured: 2026-06-24 via `dhs consumer emberplus watch --slots all --streams-only --capture`.
- 530 frames (rx 271, tx 259) — includes the live StreamCollection/StreamEntry
  pushes for the 2 stream parameters plus the S101 keep-alive request/response
  traffic that flows during a streaming session.

## Why it exists
1. Real-device **Stream** decode coverage (StreamEntry value pushes).
2. Surfaced + pins a real validate bug: S101 keep-alives are `MsgEmBER` with
   command `0x01/0x02` (no distinct MsgKeepAlive on the wire); `validate` used
   to flag them as "unexpected command", failing every long/streaming capture.
   Fixed in `consumer/validate.go`; pinned by
   `keepalive_validate_test.go` + this fixture's decode test.

## Files
| file | what |
|------|------|
| `stream.s101.jsonl` | raw S101 frames: StreamEntry pushes + keep-alives |

## Used by
- `dhd_stream_capture_test.go` — replays the capture through the codec and
  asserts a clean decode (0 errors, 0 invariants) including keep-alives.
