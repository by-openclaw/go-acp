# rx 001 INTERROGATE (SW-P-02 §3.2.3)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 1 | §3.2.3 |

Asks the matrix "what's currently connected to `<dst>`?" The matrix
replies with cmd 4 CROSSPOINT CONNECTED.

## Wire shape

```
SOM 0x01 <multiplier> <dst> <chk>
```

`<multiplier>` packs 3 bits of DIV-128 for Dest + 3 for Src + 1
bad-source bit per §3.2.3. Range: dst 0-1023.

## Fixtures (when captured)

- `frames.jsonl` — interrogate frames per ADR-0021.
- `capture.pcapng` — original socket capture.
- `tshark.tree` — frozen dissector output.
