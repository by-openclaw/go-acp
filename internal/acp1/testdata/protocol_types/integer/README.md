# Integer (object type 1)

A 16-bit signed integer parameter. One of the two most common parameter
types on a Synapse rack (alongside Enumerated).

## Spec

AXON-ACP_v1_4.pdf, p. 4.

```
INTEGER object (type=1, 10 properties):
  byte   object_type       = 1
  byte   num_properties    = 10
  byte   access             -- bit0=r, bit1=w, bit2=setDef
  int16  value              -- getValue / setValue return/take this
  int16  default_value
  int16  step_size
  int16  min_value
  int16  max_value
  string label              (max 16 + \0)
  string unit               (max 4  + \0)
```

Numeric fields are MSB-first (big-endian). Strings are null-terminated.

The fixture is a `getObject` reply — shows the full 10-property layout.
`getValue` replies would be 2 bytes total (the `int16 value` only).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 456 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 64.
- Frozen tree: [`tshark.tree`](tshark.tree) — `Temp_Left` status object.

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group status --label Temp_Left
./bin/acp set 10.6.239.113 --protocol acp1 --slot 1 --group control --label <int-param> --value 42
```
