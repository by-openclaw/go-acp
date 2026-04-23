# Error: invalid_pid (stat=3)

The provider rejects a get_property request whose target property id is
not defined on the addressed object (e.g. pid 99 — outside the 1..20
range the spec defines).

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=3. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Property IDs" for the
full pid catalogue.

Wire layout:
- Request:  `type=0, mtid=N, func=2 get_property, pid=99, obj-id=4, idx=0`.
- Error reply: `type=3, mtid=N, stat=3, pid=0`.

Provider-side check: `internal/acp2/provider/handlers.go` in
`handleGetProperty` — after the obj/idx checks, if no property in
`buildProperties(e)` has `PID == msg.PID`, returns `ErrInvalidPID`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 544 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 678 + 680 —
  `get_property(slot=1, obj=4 GainS32, pid=99)` fired via `--pid 99`.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label GainS32 --pid 99
```

The `--pid` flag defaults to 0 (pid=8 `value`) so normal reads never trip
this path.
