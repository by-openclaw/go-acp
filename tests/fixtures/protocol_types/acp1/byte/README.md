# Byte (object type 10)

Unsigned 8-bit parameter. Same layout as Integer, 0..255 range.

## Spec

AXON-ACP_v1_4.pdf, p. 4.

```
BYTE object (type=10, 10 properties):
  byte    object_type       = 10
  byte    num_properties    = 10
  byte    access
  uint8   value
  uint8   default_value
  uint8   step_size
  uint8   min_value
  uint8   max_value
  string  label              (max 16 + \0)
  string  unit               (max 4  + \0)
```

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 452 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 38.
- Frozen tree: [`tshark.tree`](tshark.tree) — `NetwPrefix` control object (network bits, 0..32).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group control --label NetwPrefix
./bin/acp set 10.6.239.113 --protocol acp1 --slot 0 --group control --label NetwPrefix --value 24
```
