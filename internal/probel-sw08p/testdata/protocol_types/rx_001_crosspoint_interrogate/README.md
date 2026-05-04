# rx 001 Crosspoint Interrogate (SW-P-08 §3.2.1)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 1 | §3.2.1 |

Asks the matrix "what's currently connected to (matrix, level, dst)?"
The matrix replies with cmd 4 Crosspoint Connected.

## Wire shape

```
DLE STX 0x01 <matrix-level> <dst-hi> <dst-lo> <btc> <chk> DLE ETX
```

`<matrix-level>` packs 4-bit matrix into the high nibble and 4-bit
level into the low nibble (narrow form). Extended form (cmd 129)
carries them as separate bytes.

## Fixtures (when captured)

- `frames.jsonl` — one or more interrogate frames per ADR-0021.
- `capture.pcapng` — original socket capture for the Wireshark Lua
  dissector cross-check.
- `tshark.tree` — frozen dissector output, regression diff target.
