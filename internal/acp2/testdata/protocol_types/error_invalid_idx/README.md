# Error: invalid_idx (stat=2)

The provider rejects a get_property request carrying `idx != 0` on a
non-preset object (only preset children declare valid idx values via
pid 7 `preset_depth`).

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=2. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Error stat codes" and
"Preset depth".

Wire layout:
- Request:  `type=0, mtid=N, func=2 get_property, pid=8, obj-id=4 (GainS32), idx=99`.
- Error reply: `type=3, mtid=N, stat=2, pid=0`.

Provider-side check: `internal/acp2/provider/handlers.go` in
`handleGetProperty` — if `msg.Idx != 0 && e.objType != ObjTypePreset`,
returns `ErrInvalidIdx`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 544 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 611 + 613 —
  `get_property(slot=1, obj=4 GainS32, idx=99)` fired via `--idx 99`.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label GainS32 --idx 99
```

The `--idx` flag defaults to 0 (ACTIVE INDEX) so normal reads never trip
this path.
