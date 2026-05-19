# `QualifiedTemplate` fixture — APP tag 25

Spec page 93 — Ember+ Documentation v2.50 §5 "The DTD".

`QualifiedTemplate` is the root-level form of a Template: it carries
a full OID path and lives at the root of the served tree as part of
the `RootElementCollection`. Concrete elements (Nodes, Parameters,
Matrices) reference it via their `templateReference` field.

## Coverage

Captured 2026-05-19 from `bin/dhs.exe producer emberplus serve --tree
internal/emberplus/testdata/coverage-tree.json --port 9101` against a
local consumer walk. The crafted template declares
`Export.Templates[0] = { OID:"0.1", Identifier:"channelStrip" }`
wrapping a Node with a single readWrite Integer Parameter.

## Files

- `frames.jsonl` — single S101/EmBER frame carrying the
  `QualifiedTemplate` at root.
- `capture.pcap` — synthesised via the R12 #473 jsonl-to-pcap writer.
  Dissector shows `[APPLICATION 25] QualifiedTemplate { ... }` with
  the description string visible verbatim.

## Replay

```powershell
dhs consumer emberplus validate `
    internal/emberplus/testdata/protocol_types/qualified_template/frames.jsonl `
    --lua
```

Verified 2026-05-19 via tshark 4.6.5: `[APPLICATION 25]
QualifiedTemplate { ... UTF8String = "Reusable channel strip template
for fixture capture (#62)" ... }`.
