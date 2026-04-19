# Long (object type 9)

32-bit signed integer parameter. Same layout as Integer, wider range.

## Spec

AXON-ACP_v1_4.pdf, p. 4.

```
LONG object (type=9, 10 properties):
  byte    object_type       = 9
  byte    num_properties    = 10
  byte    access
  int32   value
  int32   default_value
  int32   step_size
  int32   min_value
  int32   max_value
  string  label              (max 16 + \0)
  string  unit               (max 4  + \0)
```

All numeric fields are big-endian. `int32` interpretation is signed
two's-complement.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 468 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 400.
- Frozen tree: [`tshark.tree`](tshark.tree) — `#Audio_Delay` control object on slot 1.

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 1 --group control --label "#Audio_Delay"
```
