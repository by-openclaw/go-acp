# IPv4 (object type 4)

A 4-byte IPv4 address. Serialised as 4 × u8 in network order (not
packed into a u32).

## Spec

`acp2_protocol.pdf` §2, obj_type=4. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Object types" and "Wire
sizes of property values" (ipv4 → plen=8).

Key properties on the wire:
- pid 1 `object_type` = 4
- pid 8 `value` — vtype 10 (ipv4), 4-byte address in network byte order
- The fixture card exposes `Gateway` = 192.168.1.1 as read-only.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 476 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 54.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=6)` = `Gateway`.

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label Gateway
```

Setting this path yields stat=4 `no_access` — see
[`../error_no_access/`](../error_no_access/).
