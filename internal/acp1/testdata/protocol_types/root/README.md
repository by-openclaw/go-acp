# Root (object type 0)

The one-and-only root object on every slot. Exposes the per-group object
counts so a walker knows how many Identity / Control / Status / Alarm / File
objects to enumerate.

## Spec

AXON-ACP_v1_4.pdf, p. 3.

```
ROOT object (type=0, 9 properties):
  byte  object_type       = 0
  byte  num_properties    = 9
  byte  access
  byte  boot_mode          -- getValue returns this single byte
  byte  num_identity
  byte  num_control
  byte  num_status
  byte  num_alarm
  byte  num_file
```

- `getValue` returns the 1-byte `boot_mode`.
- `getObject` (method 5) returns **all 9 property bytes** in sequence.

The fixture is the provider's `getObject` reply for root — so the reader
sees the full 9-byte property sequence (type=0, n_props=9, access,
boot_mode, n_ident, n_ctrl, n_stat, n_alarm, n_file).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 436 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 6.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Walk visits root first on every slot
./bin/acp walk 10.6.239.113 --protocol acp1 --slot 0
```
