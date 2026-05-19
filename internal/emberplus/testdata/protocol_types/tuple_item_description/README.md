# `TupleItemDescription` fixture — APP tag 21

Spec page 92 — Ember+ Documentation v2.50.

`TupleItemDescription` declares one element of a Function's
`arguments[]` or `result[]` tuple. Each record carries a `name` +
`type` pair so a controller can render the function signature
without seeing the device's documentation.

## Coverage

Captured 2026-05-19 from `bin/dhs.exe producer emberplus serve --tree
internal/emberplus/testdata/coverage-tree.json --port 9101` against a
local consumer walk. The crafted Function at
`dhs-coverage.fn.calculator` declares 3 `TupleItemDescription` records:
`a:integer`, `b:integer`, `sum:integer`.

## Files

- `frames.jsonl` — single S101/EmBER frame containing the
  `QualifiedFunction` + 3 `TupleItemDescription` records (same frame
  as the sibling `qualified_function/frames.jsonl`).
- `capture.pcap` — synthesised via the R12 #473 jsonl-to-pcap writer.
  Dissector shows each `[APPLICATION 21] TupleItemDescription {
  Name, Type }` nested under the parent Function.

## Replay

```powershell
dhs consumer emberplus validate `
    internal/emberplus/testdata/protocol_types/tuple_item_description/frames.jsonl `
    --lua
```
