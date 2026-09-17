# Lawo PowerCore — real Ember+ matrix capture fixture

`raw.s101.jsonl` from a **Lawo PowerCore** (DM identity `PowerCore Rev3
(710_13)@8.2.93`). ADR-0020 Bucket 5 decoder oracle, kept specifically to
pin **matrix label resolution from a real, large device matrix**.

## Why it exists
PowerCore's `System Matrix` is **1024 × 1024** and its contents (identifier,
targetCount/sourceCount, the Labels basePath descriptor) arrive in the
directory stream, separate from the connection-bearing matrix element. This
capture proves the consumer merges those contents onto the matrix and
`enrichMatrixLabels` resolves all 1024 target labels — the same path the DHD
console exercises at 147. Two independent vendors (DHD + Lawo) confirm the
behavior is general, not device-specific.

## Provenance
- Device: Lawo PowerCore (Rev3 710_13, fw 8.2.93).
- Captured: 2026-06-24 via `dhs consumer emberplus walk … --capture` (remote).
- 5820 frames. Matrix at OID `1.3.1`, identifier "System Matrix".

## Used by
- `matrix_capture_resolve_test.go` — replays this + the DHD capture through the
  full tree-builder and asserts the matrix resolves `targetCount` + all
  `targetLabels` (PowerCore 1024, DHD 147). Regression guard against a dropped
  contents merge.
