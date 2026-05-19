# `QualifiedFunction` fixture — APP tag 20

Spec page 92 — Ember+ Documentation v2.50.

`QualifiedFunction` is the qualified-OID form of a Function element.
Its `arguments[]` and `result[]` tuples are encoded as
`TupleItemDescription` (APP 21) records, captured separately in the
sibling `tuple_item_description/` fixture from the same frame.

## Coverage

Captured 2026-05-19 from `bin/dhs.exe producer emberplus serve --tree
internal/emberplus/testdata/coverage-tree.json --port 9101` against a
local consumer walk. The crafted Function at
`dhs-coverage.fn.calculator` takes
`(a:integer, b:integer)` and returns `(sum:integer)`.

## Files

- `frames.jsonl` — single S101/EmBER frame carrying the
  `QualifiedFunction` declaration (same frame as
  `tuple_item_description/frames.jsonl`).
- `capture.pcap` — synthesised via the R12 #473 jsonl-to-pcap writer.
  Dissector shows `[APPLICATION 20] QualifiedFunction { OID = 1.2.1,
  Identifier = "calculator", Arguments[2], Result[1] }`.

## Replay

```powershell
dhs consumer emberplus validate `
    internal/emberplus/testdata/protocol_types/qualified_function/frames.jsonl `
    --lua
```
