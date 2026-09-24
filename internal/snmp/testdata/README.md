# snmp testdata

What the connector is tested against without a device.

| Folder | What it is |
|---|---|
| `protocol_types/<pdu>/` | one capture per PDU kind the connector emits or reads — GetRequest, GetNextRequest, Response, SetRequest, a Response carrying an error-status, and the same read in v2c. Each with a README saying what it is. |
| `fixtures/dr5000-snmp.pcapng` | the golden scenario: one session of get + walk + set + refused set + a v2c read, 36 frames, as captured. |
| `integration-test/` | the committed DM + manifest (ADR-0025 #4). |

Everything here came off the **ATEME Kyrion DR5000** at `10.6.255.114`
(v1 on 161, firmware 1.3.1.1, serial 1410-00596) on 2026-09-24.

Re-capture after a firmware change — note the BPF filter: on this
fabric a cooked `-i any` capture does not match `udp port 161`, so
capture unfiltered and select at read time:

```bash
tshark -i any -w raw.pcap                       # while the verbs run
tshark -r raw.pcap -Y snmp -w dr5000-snmp.pcapng
```

Verify the dissector still decodes all of it, which is the check that
matters:

```bash
tshark -X lua_script:../wireshark/dhs_snmp.lua -r fixtures/dr5000-snmp.pcapng
```
