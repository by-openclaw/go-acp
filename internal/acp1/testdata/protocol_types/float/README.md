# Float (object type 3)

IEEE-754 32-bit float parameter. Percentages, levels, gains — the
analog-looking dials.

## Spec

AXON-ACP_v1_4.pdf, p. 4.

```
FLOAT object (type=3, 10 properties):
  byte     object_type       = 3
  byte     num_properties    = 10
  byte     access
  float32  value              -- IEEE 754 big-endian
  float32  default_value
  float32  step_size
  float32  min_value
  float32  max_value
  string   label              (max 16 + \0)
  string   unit               (max 4  + \0)
```

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 464 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 68.
- Frozen tree: [`tshark.tree`](tshark.tree) — `SPF_Progress` status object (0.0..100.0 %).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group status --label SPF_Progress
./bin/acp set 10.6.239.113 --protocol acp1 --slot 1 --group control --label <float-param> --value -3.0
```
