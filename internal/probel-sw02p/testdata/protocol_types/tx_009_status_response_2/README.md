# tx 009 STATUS RESPONSE - 2 (SW-P-02 §3.2.11)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| matrix → controller | 9 | §3.2.11 |

Reply to cmd 7 STATUS REQUEST. Carries the matrix's current
operational status (active controller, fault flags, etc.).

## Wire shape

```
SOM 0x09 <status-bytes...> <chk>
```

## Fixtures (when captured)

- `frames.jsonl` — paired with the rx 007 it answers.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
