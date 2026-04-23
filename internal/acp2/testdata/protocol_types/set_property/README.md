# Function: set_property (func=3)

Write one property on one object. Reply confirms by echoing the new
(post-write) property value. Fires an `announce` (type=2) broadcast to
every session that has called `EnableProtocolEvents([2])`.

## Spec

`acp2_protocol.pdf` §3 "Functions", function=3. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Property IDs" and
"Announces (type=2)".

Wire layout:
- Request:  `type=0, mtid=N, func=3, pid=<target>, obj-id, idx, property_header + value`.
- Reply:    `type=1, mtid=N, func=3, pid=<target>, obj-id, idx, property_header + confirmed value`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 560 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 280 + 282 —
  `set_property(slot=1, obj-id=4, pid=value, value(s32))` on `GainS32`.
- Frozen tree: [`tshark.tree`](tshark.tree).

See [`../announce/`](../announce/) for the paired type=2 broadcast fired
by the provider after this write.

## CLI equivalent

```bash
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --label GainS32 --value 3
```

Setting a read-only property yields stat=4 `no_access` instead —
see [`../error_no_access/`](../error_no_access/).
