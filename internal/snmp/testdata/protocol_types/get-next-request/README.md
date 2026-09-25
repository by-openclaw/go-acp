# get-next-request

**GetNextRequest** — the walk primitive: v1 has no GETBULK, so a subtree costs one round trip per object.

- Captured from the ATEME Kyrion DR5000 at `10.6.255.114` (SNMP v1 on
  161, firmware 1.3.1.1) on 2026-09-24, driving the real `dhs consumer
  snmp` verbs from the control node.
- `wire.pcapng` is the frames themselves; read them with our dissector:

```bash
tshark -X lua_script:../../../wireshark/dhs_snmp.lua -r wire.pcapng
```
