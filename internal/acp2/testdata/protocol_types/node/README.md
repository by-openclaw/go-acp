# Node (object type 0)

A container object — holds children via pid 14 (`children`). Root objects
and every intermediate folder in the ACP2 tree are nodes.

## Spec

`acp2_protocol.pdf` §2 "Object types", table row obj_type=0. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Object types".

Key properties on the wire:
- pid 1 `object_type` = 0
- pid 2 `label` — UTF-8 0-terminated
- pid 3 `access` — 1=r / 2=w / 3=rw
- pid 14 `children` — u32[] child obj-ids

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 496 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 34.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=1)` = `ROOT_NODE_V2` on the fixture card.

## CLI equivalent

```bash
./bin/dhs consumer acp2 walk 127.0.0.1 --port 2072 --slot 1
./bin/dhs consumer acp2 get  127.0.0.1 --port 2072 --slot 1 --label ROOT_NODE_V2
```
