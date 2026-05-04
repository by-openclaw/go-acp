# tx 004 Crosspoint Connected (SW-P-08 §3.2.4)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| matrix → controller | 4 | §3.2.4 |

Reply to cmd 1 Crosspoint Interrogate or cmd 2 Crosspoint Connect.
Same wire shape as cmd 3 Crosspoint Tally, but addressed back to the
single requesting session rather than broadcast.

## Wire shape

```
DLE STX 0x04 <matrix-level> <dst-hi> <dst-lo> <src-hi> <src-lo> <btc> <chk> DLE ETX
```

## Fixtures (when captured)

- `frames.jsonl` — paired with the `rx_001_*` or `rx_002_*` request
  it answers. The codec round-trip test asserts the reply byte
  shape matches the spec.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
