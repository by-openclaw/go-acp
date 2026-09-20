# MNSet connector docs

Riedel MuoN eMSFP / FusioN modules, controlled over **their own REST API**
(`http://<module>/emsfp/node/v1/…`). MN SET, Riedel's management server,
is **not** in the control path — it is only asked for its device list
(`inventory`). Consumer only; there is no producer (issue #1110).

| Doc | Purpose |
|---|---|
| [`consumer.md`](consumer.md) | CLI walkthrough: discover, walk, export, get, set, the path grammar |
| [`runbook.md`](runbook.md) | operate it: put a Neuron output on an HDMI monitor, verify on the fabric, undo |
| [`scope.md`](scope.md) | the accepted scope (REST now; SNMP / NBAPI / syslog later) |
| [`api-endpoints.md`](api-endpoints.md) | the 30 node resources + the 112 MN SET app operations, as discovered |
| [`hdmi-monitor-config.md`](hdmi-monitor-config.md) | channel ↔ HDMI cage map and the receiver records that matter |
| [`connector-plan.md`](connector-plan.md) | the design as pinned before coding |
| [`../CLAUDE.md`](../CLAUDE.md) | atomic API context: ports, auth, object model, what NOT to do |

Wireshark: [`../wireshark/dhs_mnset.lua`](../wireshark/dhs_mnset.lua)
(post-dissector over http; filter `dhs_mnset`).

Fixtures: [`../testdata/`](../testdata/) —
`fusion6-0x68cd783f-export.csv` (the connector's own export of the lab
FusioN6, `dhs consumer mnset export … --format csv`, 2026-09-20),
`device.json` (the same module as MN SET's `/api/device` lists it),
`mnset-dm.csv` (that MN SET record flattened), `fusion-nmos-node.json`
(its IS-04 node), `endpoints-raw.txt` / `device-rest-tabs.txt` (the MN SET
operation and Rest-page catalogues as discovered).
