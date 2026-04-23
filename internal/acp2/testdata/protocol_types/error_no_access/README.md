# Error: no_access (stat=4)

The provider rejects a `set_property` against a read-only object (the
`access` property reports `r` = 1, not `rw`). Our fixture card exposes
`Gateway` as read-only IPv4 to exercise this path.

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=4. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Error stat codes".

Wire layout:
- Request:  `set_property(slot=1, obj-id=6, pid=value, value(ipv4))`.
- Error reply: `type=3, mtid=N, stat=4, pid=0` (4-byte ACP2 header).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 548 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 453 + 455 —
  `set_property(Gateway, value=10.0.0.1)` request followed by the
  provider's `stat=4` error reply.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --label Gateway --value 10.0.0.1
```

The CLI prints `error: acp2: stat=4` and does not update its local
cache.
