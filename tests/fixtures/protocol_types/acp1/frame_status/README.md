# Frame Status (object type 6)

Rack-controller-only object (slot 0). Reports the card-presence / power /
boot state of every slot in the rack, in one compact byte array.

## Spec

AXON-ACP_v1_4.pdf, p. 6. Read-only.

```
FRAME STATUS object (type=6, 4 properties):
  byte    object_type       = 6
  byte    num_properties    = 4
  byte    access             = 1 (read-only)
  byte[]  num_slots + slot_status_array
          byte[0] = num_slots
          byte[1..N] = per-slot status:
                      0 = no card
                      1 = powering up
                      2 = present
                      3 = error
                      4 = removed
                      5 = boot
```

The fixture is a **`getValue` reply** — provider returns only the value-bytes
portion (num_slots + status array), not the full `getObject` header. This
is the wire shape of a Frame Status announcement as well (MTID=0, MCODE=0,
Group=6, ID=0).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 460 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 2.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group frame --id 0
# → num_slots=31 + per-slot status byte array
```
