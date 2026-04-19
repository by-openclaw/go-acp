# String (object type 5)

Null-terminated ASCII string parameter. Used for labels, descriptions,
firmware revision, serial number, MIB text.

## Spec

AXON-ACP_v1_4.pdf, p. 5.

```
STRING object (type=5, 6 properties):
  byte    object_type       = 5
  byte    num_properties    = 6
  byte    access
  string  value              [MaxLen + \0]
  byte    max_len
  string  label              (max 16 + \0)
```

`max_len` is the configured maximum string length (so a client knows how
many bytes it may send in a `setValue`). The actual `value` is
null-terminated and may be shorter.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 448 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 8.
- Frozen tree: [`tshark.tree`](tshark.tree) — `Card name` identity object (max 8 chars).

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group identity --label "Card name"
./bin/acp set 10.6.239.113 --protocol acp1 --slot 0 --group identity --label "User label" --value "MyRack"
```
