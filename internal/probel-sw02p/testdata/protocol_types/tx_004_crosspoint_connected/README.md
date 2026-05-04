# tx 004 CROSSPOINT CONNECTED (SW-P-02 §3.2.6)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| matrix → controller | 4 | §3.2.6 |

Reply to cmd 1 INTERROGATE or cmd 2 CONNECT. Same wire shape as cmd 3
TALLY, addressed back to the single requesting session rather than
broadcast.

## Wire shape

```
SOM 0x04 <multiplier> <dst> <src> <chk>
```

## Fixtures (when captured)

- `frames.jsonl` — paired with the rx_001 / rx_002 request it
  answers. The codec round-trip test asserts the reply byte shape
  matches the spec.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
