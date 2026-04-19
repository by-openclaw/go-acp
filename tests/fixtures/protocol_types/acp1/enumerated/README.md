# Enumerated (object type 4)

A single-byte index into an inline item list. The most common type on
Synapse rack controllers (modes, toggles, state selections).

## Spec

AXON-ACP_v1_4.pdf, p. 5.

```
ENUMERATED object (type=4, 8 properties):
  byte    object_type       = 4
  byte    num_properties    = 8
  byte    access
  byte    value              -- index into item_list
  byte    num_items
  byte    default_value
  string  label              (max 16 + \0)
  string  item_list          -- comma-delimited, null-terminated
                             -- e.g. "Manual,DHCP\0"
```

The item_list format is **comma-delimited, terminated by `\0`**. A walker
parses it by `strings.Split(item_list, ",")`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 452 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 24.
- Frozen tree: [`tshark.tree`](tshark.tree) — `IP_Conf` control object (`Manual,DHCP`).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group control --label IP_Conf
./bin/acp set 10.6.239.113 --protocol acp1 --slot 0 --group control --label IP_Conf --value DHCP
```
