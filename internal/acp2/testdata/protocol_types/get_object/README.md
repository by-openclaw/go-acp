# Function: get_object (func=1)

Fetch every property of a single object. Reply body is a sequence of
property headers (pid + data/vtype + plen + value) with 4-byte
alignment between headers.

## Spec

`acp2_protocol.pdf` §3 "Functions", function=1. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "ACP2 message header"
and "Property header".

Wire layout:
- Request:  `type=0, mtid=N, func=1, pid=0, obj-id u32, idx u32` (12 bytes).
- Reply:    `type=1, mtid=N, func=1, pid=0, obj-id, idx, <properties...>`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 616 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 44 + 46
  (request + reply pair) — `get_object(slot=1, obj-id=4, idx=0)`,
  which resolves to `GainS32` (a `Number` object).
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

Every `walk` step fires `get_object` once per object in the tree:

```bash
./bin/dhs consumer acp2 walk 127.0.0.1 --port 2072 --slot 1
```

See [`../node/`](../node/), [`../string/`](../string/),
[`../number/`](../number/), [`../enum/`](../enum/),
[`../ipv4/`](../ipv4/) for per-object-type reply shapes.
