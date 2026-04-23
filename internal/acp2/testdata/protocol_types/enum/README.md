# Enum (object type 2)

An enumeration value — wire encoding is u32 index into the `options`
table (pid 15). This fixture shows a 2-option enum (`Off`, `On`).

## Spec

`acp2_protocol.pdf` §2, obj_type=2 and §5.1 "Enum options layout" (pid
15, 72 bytes per option). See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Object types".

Key properties on the wire:
- pid 1 `object_type` = 2
- pid 8 `value` — vtype 9 (preset/enum), u32 index, 4 bytes
- pid 15 `options` — 72 bytes per option: 64-byte label + 4-byte value +
  4 reserved. With 2 options this fixture shows `plen=148` (4 header +
  2×72 = 148 bytes body).
- pid 9 `default_value` — omitted for plain enums (see provider
  `encoder.go` comment — pid 9 is depth-indexed and only valid for
  preset children carrying pid 7).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 624 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 50.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=5)` = `Mode`.

## Related open work

[#79 (provider-acp2): Enum pid 15 options layout — validate against Cerebrum](https://github.com/by-rune/acp/issues/79).

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label Mode
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --label Mode --value 1
```
