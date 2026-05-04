# rx 002 Crosspoint Connect (SW-P-08 §3.2.2)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 2 | §3.2.2 |

Tells the matrix to connect `<src>` to `<dst>` on `(matrix, level)`.
The matrix replies with cmd 4 Crosspoint Connected once the route is
live, AND broadcasts cmd 3 Crosspoint Tally to every connected session
(per CLAUDE.md "Salvo commit emits cmd 04 Connected" deviation note;
cmd 3 broadcast applies to non-salvo single-shot connects too).

## Wire shape

```
DLE STX 0x02 <matrix-level> <dst-hi> <dst-lo> <src-hi> <src-lo> <btc> <chk> DLE ETX
```

Extended form (cmd 130) has separate matrix + level bytes and 14-bit
dst / src.

## Fixtures (when captured)

- `frames.jsonl` — paired (cmd 002 request, cmd 004 reply, cmd 003
  broadcast) sequence so the codec round-trip hits all three.
- `capture.pcapng` + `tshark.tree` — dissector cross-check artefacts.
