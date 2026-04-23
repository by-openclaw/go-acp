# String (object type 5)

A UTF-8 text value with `maxLen=N` constraint expressed via pid 6
`string_max_length`.

## Spec

`acp2_protocol.pdf` §2, obj_type=5. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Object types" and
"Number types" (vtype 11 = string).

Key properties on the wire:
- pid 1 `object_type` = 5
- pid 6 `string_max_length` = u16 (slot 1 `UserLabel` is `maxLen=16`)
- pid 8 `value` — vtype 11, bytes + \0 + alignment padding

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 500 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 42.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=3)` = `UserLabel` on the fixture card.

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label UserLabel
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --id 3 --value Updated
```
