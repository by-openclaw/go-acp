# Alarm (object type 7)

An alarm/event source. Carries priority, tag, label, and the two
event strings (on / off) used by external monitors (SNMP trap text,
dashboard flashers).

## Spec

AXON-ACP_v1_4.pdf, p. 7.

```
ALARM object (type=7, 8 properties):
  byte    object_type       = 7
  byte    num_properties    = 8
  byte    access
  byte    priority           -- 0 = disabled
  byte    tag                -- fixed value assigned by Axon
  string  label              (max 16 + \0)
  string  event_on_msg       (max 32 + \0)
  string  event_off_msg      (max 32 + \0)  -- immediately after event_on
```

The `event_off_msg` is placed **immediately** after `event_on_msg` with
no extra separator — the dissector splits them on the first `\0`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 448 bytes.
- Extracted from: `bin/acp1_walk_slot0_slot1.pcapng` frame 114.
- Frozen tree: [`tshark.tree`](tshark.tree) — `Announcements` alarm on slot 0.

## CLI equivalent

```bash
./bin/acp get 10.6.239.113 --protocol acp1 --slot 0 --group alarm --id 0
```
