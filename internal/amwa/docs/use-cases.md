# NMOS use cases — proven configurations

Each row was run live on the VLAN600 plant (registry `10.6.250.101:8235`,
EVS Neuron, EVS Cerebrum 2.8.17) with wire receipts; dates 2026-08-28/29.
Details live in [`vlan600-migration.md`](vlan600-migration.md),
[`cerebrum-interop.md`](cerebrum-interop.md),
[`neuron-interop-matrix.md`](neuron-interop-matrix.md).

## Discovery of the registry (node side)

| mode | how | proof |
|---|---|---|
| **Manual** | device/UI pins `http://<registry>:8235` (Neuron Addr Override; dhs `--registry URL`) | Neuron + dhs node heartbeats on tape |
| **mDNS** (Mode A) | registry announces `_nmos-register/_nmos-query._tcp`; nodes browse | Cerebrum harvest; Neuron fallback registration; announce-off purge |
| **sd-DNS** (RFC 6763 §10) | zone on dnsmasq @ `.101` (`by-systems.arpa`); node `--unicast --resolver 10.6.250.101` | `registry discovered (unicast DNS-SD)` + registration |

`--no-mdns` on the registry gives a fully deterministic manual-only
plant (proven: both nodes re-registered after a registry restart with
no announces at all).

## Mirror registration (registry → registry)

```
dhs Node ↔ dhs Registry ↔ [ vendor Registry ↔ vendor controller ]
           (source of truth)      (their native catalogue)
```

`dhs registry nmos mirror --source http://10.6.250.101:8235 --target
http://<vendor>:8080` — toward the source the mirror is a standard
Controller (Query-WS grains); toward the target a standard Node
(Registration API + per-node heartbeats). Proven against Cerebrum's
hosted registry: 856/856 resources, full SDP + details rendered by the
Cerebrum controller from its own catalogue. Traps encoded in the
implementation: explicit `Content-Length: 0` heartbeats (their 411),
dependency-ordered POSTs (their parent validation), full resync on a
404 heartbeat.

## IS-05 driving (controller side)

- `consumer nmos connect --registry … --receiver R --sender S` —
  route through the catalogue (proven on the Neuron: VTX-03 → VRX-04,
  device-verified ACTIVE).
- `consumer nmos set --registry … --sender S --destination ip1,ip2
  --port p1,p2 --enable` — retune multicast + port per leg; SDP
  regenerates (proven: 239.30/32.1.1:5010, receiver re-fed).
- `consumer nmos watch --registry … --resource /senders` — live
  change stream (proven: 211-grain sync + the change grain from a
  `set`).
- Every mutating verb has `--dry-run` printing the exact PATCH first.
