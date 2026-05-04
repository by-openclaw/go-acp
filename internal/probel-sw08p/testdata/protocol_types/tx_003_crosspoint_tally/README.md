# tx 003 Crosspoint Tally (SW-P-08 §3.2.3)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| matrix → controller | 3 | §3.2.3 |

Spontaneous broadcast: "this dst is now connected to src on (matrix,
level)." Emitted to every connected session whenever the matrix
applies a route — single-shot connect (cmd 2), salvo commit (cmd 121,
per CLAUDE.md deviation note), or local panel.

## Wire shape

```
DLE STX 0x03 <matrix-level> <dst-hi> <dst-lo> <src-hi> <src-lo> <btc> <chk> DLE ETX
```

## Fixtures (when captured)

- `frames.jsonl` — a representative tally storm (e.g. the broadcast
  fan-out following a 64-dst salvo commit).
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
