# Function: get_property (func=2)

Fetch one property of one object. Request carries the property id
(pid) to read and the idx (preset-index or 0 = ACTIVE INDEX on non-preset
objects). Reply carries a single property header.

## Spec

`acp2_protocol.pdf` §3 "Functions", function=2. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Property IDs".

Wire layout:
- Request:  `type=0, mtid=N, func=2, pid=<target>, obj-id, idx, property_header`
  (16 bytes — the trailing 4 bytes are the pid+data+plen header pinning
  the asked property).
- Reply:    `type=1, mtid=N, func=2, pid=<target>, obj-id, idx, property_header + value`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 556 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 217 + 219 —
  `get_property(slot=1, obj-id=4, pid=value)` on `GainS32`.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label GainS32
```

The CLI does a resolution `walk` first (building the local tree), then
issues exactly one `get_property` for the target leaf.
