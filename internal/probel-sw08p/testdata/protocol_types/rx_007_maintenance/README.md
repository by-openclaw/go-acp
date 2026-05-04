# rx 007 Maintenance (SW-P-08 §3.2.7)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 7 | §3.2.7 |

Maintenance / liveness probe. The matrix's reply (or the framing-
level DLE ACK alone, depending on vendor) is enough to confirm the
session is alive.

## Wire shape

```
DLE STX 0x07 <data...> <btc> <chk> DLE ETX
```

The `<data>` payload is vendor-specific (test pattern, version
query, ping nonce). Spec leaves it open.

## Fixtures (when captured)

- `frames.jsonl` — at least one maintenance probe + the framing
  ACK that follows so the codec round-trip exercises the
  command + the §2 ACK path.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
