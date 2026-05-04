# rx 002 CONNECT (SW-P-02 §3.2.4)

| Direction | Cmd byte | Spec |
| --- | ---: | --- |
| controller → matrix | 2 | §3.2.4 |

Tells the matrix to connect `<src>` to `<dst>`. The matrix replies
with cmd 4 CROSSPOINT CONNECTED once the route is live and broadcasts
cmd 3 TALLY to every connected session.

Note the deviation captured in `internal/probel-sw02p/CLAUDE.md`
"Protect blocks connect — state-echo on rx 02 / rx 66": when the
target dst is protected and the requester is not the protect owner,
the matrix silently drops the request OR echoes the EXISTING route
back via tx 04 (per §3.2.60 silence + the §3.2.6 echo deviation).

## Wire shape

```
SOM 0x02 <multiplier> <dst> <src> <chk>
```

## Fixtures (when captured)

- `frames.jsonl` — paired (cmd 002 request, cmd 004 reply, cmd 003
  broadcast) sequence so the codec round-trip hits all three.
- `capture.pcapng` + `tshark.tree` — dissector cross-check artefacts.
- A second fixture set under a `protect_blocked/` subfolder pinning
  the deviation in `provider/cmd_rx002_connect.go`.
