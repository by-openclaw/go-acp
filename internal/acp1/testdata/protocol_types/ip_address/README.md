# IP Address (object type 2)

A 32-bit IPv4 address parameter. Same wire layout as Integer / Long /
Float / Byte, only the numeric field width differs.

## Spec

AXON-ACP_v1_4.pdf, p. 4.

```
IP ADDRESS object (type=2, 10 properties):
  byte    object_type       = 2
  byte    num_properties    = 10
  byte    access
  uint32  value              -- IPv4 address (MSB-first: 0x0A06EF71 = 10.6.239.113)
  uint32  default_value
  uint32  step_size
  uint32  min_value
  uint32  max_value
  string  label              (max 16 + \0)
  string  unit               (max 4  + \0)
```

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 456 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 26.
- Frozen tree: [`tshark.tree`](tshark.tree) — `mIP` control object.

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group control --label mIP
./bin/acp set 10.6.239.113 --protocol acp1 --slot 0 --group control --label mIP --value 192.168.1.5
```
