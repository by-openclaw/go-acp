# rx 007 STATUS REQUEST (SW-P-02 §3.2.9)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 7 | §3.2.9 |

Status query. Used by VSM-style controllers as a continuous-poll
keepalive (per CLAUDE.md "AppKeepaliveSpacing" notes) since SW-P-02
§3.1 has no framing-layer keepalive command.

## Wire shape

```
SOM 0x07 <data...> <chk>
```

The exact `<data>` shape depends on the matrix vendor — query
type, optional dst hint. Matrix replies with cmd 9 STATUS RESPONSE-2
(§3.2.11).

## Fixtures (when captured)

- `frames.jsonl` — a representative poll cadence with paired tx 9
  responses.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
