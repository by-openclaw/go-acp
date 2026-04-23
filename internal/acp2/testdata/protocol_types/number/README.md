# Number (object type 3)

A typed numeric value. `number_type` (pid 5) selects one of the 9 wire
encodings — s8/s16/s32/s64/u8/u16/u32/u64/float. This fixture shows an
s32 (`format="s32"`) with unit `dB`, range `-60..20`, step `1`.

## Spec

`acp2_protocol.pdf` §2, obj_type=3. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Object types" and
"Number types".

Key properties on the wire:
- pid 1 `object_type` = 3
- pid 5 `number_type` = 2 (s32)
- pid 8 `value` — vtype 2, s32 stored as signed big-endian 4 bytes
- pid 9 `default_value`, pid 10 `min_value`, pid 11 `max_value`,
  pid 12 `step_size`, pid 13 `unit`

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 520 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 46.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=4)` = `GainS32`.

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label GainS32
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --label GainS32 --value 3
```
