# `StreamDescription` fixture — APP tag 12

Spec page 86 — Ember+ Documentation v2.50 §4 ParameterContents[16].

`StreamDescription` is the wire form of a Parameter's `streamDescriptor`
field. It declares the binary layout of the parameter's wire-stream
payload (format constant + offset within the StreamEntry packet). The
dissector pulls it out so the operator can see how a metering point's
value is packed onto the wire without reading the device's manual.

## Coverage

Captured 2026-05-19 from `bin/dhs.exe producer emberplus serve --tree
internal/emberplus/testdata/coverage-tree.json --port 9101` against a
local consumer walk. The crafted Parameter at
`dhs-coverage.streams.vu_a` declares
`{"streamIdentifier":1001,"streamDescriptor":{"format":13,"offset":0}}`
(format 13 = IEEE Float 32-bit little-endian per
`canonical.StreamIEEEFloat32LE`).

## Files

- `frames.jsonl` — single S101/EmBER frame carrying the
  `QualifiedParameter` that includes the `StreamDescription` record.
- `capture.pcap` — synthesised via the R12 #473 jsonl-to-pcap writer
  (`tools/jsonl-to-pcap`); Wireshark opens it directly and shows
  `[APPLICATION 12] StreamDescription { format = ieeeFloat32LE,
  offset = 0 }` under the surrounding Parameter.

## Replay

```powershell
dhs consumer emberplus validate `
    internal/emberplus/testdata/protocol_types/stream_description/frames.jsonl `
    --lua
```

Loads the dissector (from the installed user-plugin copy or via
`-X lua_script:` when not installed) and prints the tshark -V
dissection.
