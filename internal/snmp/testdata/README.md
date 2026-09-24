# snmp testdata

What the connector is tested against without a device.

| Folder | What it is |
|---|---|
| `protocol_types/<pdu>/` | one capture per PDU kind the connector emits or reads — GetRequest, GetNextRequest, Response, SetRequest, a Response carrying an error-status, and the same read in v2c. Each with a README saying what it is. |
| `fixtures/dr5000-snmp.pcapng` | the golden scenario: one session of get + walk + set + refused set + a v2c read, 36 frames, as captured. |
| `integration-test/` | the committed DM + manifest (ADR-0025 #4). |

Everything here came off the **ATEME Kyrion DR5000** at `10.6.255.114`
(v2c and v1 on 161, firmware 1.3.1.1, serial 1410-00596) on 2026-09-24.

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

## The DM + manifest fixture

`integration-test/` is ADR-0025 deliverable 4: the device model this
agent published, and the manifest that attaches it to one slot.

```bash
SNMP_TEST_HOST=10.6.255.114 SNMP_COMMUNITY=public SNMP_WRITE_FIXTURE=1 \
  go test -tags integration ./internal/snmp/integration/ -run WriteTheCommittedDM -v
```

Two seconds, and it is complete. The obvious route — one whole-device
walk, copied out of `.cache/dm/snmp/` — does **not** work here, and
finding out why is most of what this fixture is:

- the whole model is **94 181 objects and 48 MB**, and 97 % of that is
  two transport-stream tables (4096 rows × 13 columns of programme
  streams, plus a 42 705-entry DVB subtitle table);
- and the walk **does not finish**: after about forty minutes the agent
  stops answering, the walk says "the model is not complete" and keeps
  what it got — which stopped partway through those tables, so
  `Software`, `Hardware`, `Network`, `Status.Input` and everything else
  that sorts after them was simply missing.

So the generator walks branch by branch, capping each group at 32
objects: every branch present, every table's shape present, no table's
bulk. 4 247 objects read, 2 775 kept, 1.3 MB — which is what
`internal/manifest/TEMPLATE.md` asks for ("no multi-MB blobs — trim to
a representative card if a real DM is huge").

`internal/snmp/consumer/fixture_test.go` is what it is for: it holds the
model, the compiled MIB tables and the shipped alarm template to each
other, from the repo alone. It earned that immediately — a shipped rule
pointed at `Video.DecodedValid`, where the MIB puts that object under
`Video.Decoded.Valid`, so it had been matching nothing.
