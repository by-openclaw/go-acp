# Error: invalid_obj_id (stat=1)

The provider rejects a request that names an obj-id that doesn't exist
on the target slot. Slot 99 (not installed) or obj-id outside the
walked tree both trip this path.

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=1. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Error stat codes".

Wire layout:
- Request:  the original request that caused the error.
- Error reply: `type=3, mtid=N, stat=1, pid=0` (4-byte ACP2 header, no body).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 540 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 558 + 560 —
  `get_object(slot=99, obj-id=0)` probe fired by the `diag` verb,
  followed by the provider's `stat=1` error reply.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 diag 127.0.0.1 --port 2072 --slot 99
```

The diag command prints every probe's result; this fixture's frame pair
corresponds to the "get_object spec (AN2 data, 12 bytes)" probe on the
absent slot.
