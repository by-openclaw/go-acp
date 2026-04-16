# ACP1 Protocol

## What is ACP1

ACP1 (Axon Control Protocol version 1) is a binary protocol for controlling
Synapse/Axon broadcast equipment. It operates over UDP or TCP and addresses
objects within rack-mounted cards by slot, group, and object ID.

## Wire format

```
Header: 7 bytes
  MTID(4) + PVER(1) + MTYPE(1) + MADDR(1)

MDATA: up to 134 bytes
  MCODE(1) + ObjGrp(1) + ObjId(1) + Value(up to 131)

Total: max 141 bytes per message
```

All values big-endian. Strings null-terminated ASCII.

## Transport modes

| Mode | Transport   | Port | Framing                          |
|------|-------------|------|----------------------------------|
| A    | UDP direct  | 2071 | None (one datagram = one message)|
| B    | TCP direct  | 2071 | MLEN(u32) prefix before header   |
| C    | AN2/TCP     | 2072 | AN2 frame wraps ACP1 payload     |

## CLI examples

```bash
# Discover devices on LAN
./bin/acp discover --protocol acp1

# Walk slot 0 (rack controller)
./bin/acp walk 192.168.1.5 --protocol acp1 --slot 0

# Walk all populated slots
./bin/acp walk 192.168.1.5 --protocol acp1 --all

# Get a value by label
./bin/acp get 192.168.1.5 --protocol acp1 --slot 1 --group control --label "Video Gain"

# Set a value
./bin/acp set 192.168.1.5 --protocol acp1 --slot 1 --group control --label "Video Gain" --value -3.0

# Watch for announcements
./bin/acp watch 192.168.1.5 --protocol acp1 --slot 1

# Export to JSON
./bin/acp export 192.168.1.5 --protocol acp1 --format json --output device.json
```

## Integration tests

```bash
export ACP1_TEST_HOST=192.168.1.5
make test-integration-acp1
```

Requires a real ACP1 device or emulator on the same VLAN.

## Known limitations

- TCP direct mode (Mode B) not yet tested against real hardware
- AN2 transport (Mode C) not yet implemented
- `discover` requires same broadcast domain (no routing)
- Methods 6-9 not defined by the spec (IDs 0-5 only)
- File-object firmware reprogramming out of scope

## Reference

- Spec: [docs/protocols/AXON-ACP_v1_4.pdf](../AXON-ACP_v1_4.pdf)
- Wireshark dissector: [assets/dissector_acpv1.lua](../../../assets/dissector_acpv1.lua)
- Full wire reference: [CLAUDE.md](../../../CLAUDE.md) (ACP1 section)

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
