# Runbook — put a Neuron output on a FusioN HDMI monitor

Lab: Neuron 10.6.255.102 (VTX-01 = 239.131.3.134:20000 RED /
239.132.3.134 BLUE), FusioN6 10.6.40.53, Arista FABRIC-1 10.6.224.21.

## 1. Find the module

```
dhs consumer mnset discover --range 10.6.40.50-99
```

No answer → the module is on another VLAN or off (10.6.40.3 was powered
off on 2026-09-20). `inventory` against MN SET (10.6.250.105) lists what
it knows, online or not:

```
MNSET_PASS=… dhs consumer mnset inventory 10.6.250.105 --user admin
```

## 2. Know which channel drives which HDMI cage

| HDMI cage (SFP) | channels | decode side |
|---|---|---|
| 1 (sfp 0) | CH1 / CH2 | CH2 |
| 2 (sfp 1) | CH3 / CH4 | CH4 |
| 3 (sfp 3) | CH5 / CH6 | CH6 |
| 4 (sfp 5) | CH7 / CH8 | CH8 |

Odd channels are encoders and ignore a receiver assignment ("channel
odd ignored", confirmed in Cerebrum). Each even channel has a video
receiver (`VidRx`), two audio receivers and an anc receiver; their flow
indices are read from the export:

```
dhs consumer mnset export 10.6.40.53 --format csv --out .cache/exports/fusion-53.csv
grep -n 'devices\.[0-9]*\.label\|receivers\.[0-9]*\.flow_id\|receivers\.[0-9]*\.label' .cache/exports/fusion-53.csv
```

## 3. Point the receiver at the Neuron sender

`<uuid>` is the receiver's `flow_id` from step 2. Both legs, RED and BLUE, always (ST 2022-7):

```
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.0.dst_ip_addr'  --value 239.131.3.134
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.0.dst_udp_port' --value 20000
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.0.enable'       --value 1
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.1.dst_ip_addr'  --value 239.132.3.134
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.1.dst_udp_port' --value 20000
dhs consumer mnset set 10.6.40.53 --path 'flows.<uuid>.network.1.enable'       --value 1
```

Alternative, when the plant has an NMOS registry: IS-05 PATCH through
`dhs consumer nmos connect` (Fusion node API is on **port 80**, Neuron on
3000) — the module then fills the same `network[]` records itself.

## 4. Prove the stream is flowing

On the module:

```
dhs consumer mnset get 10.6.40.53 --path 'flows.<uuid>.network.0.pkt_cnt'   # must climb
dhs consumer mnset walk 10.6.40.53 | grep self.diag.flow                    # per-flow diagnostics
```

On the fabric (read-only, key on `admin`):

```
ssh -i ~/.ssh/fabric_arista admin@10.6.224.21 'enable
show ip igmp snooping groups vlan 640 | include 239.131.3.134'
```

The module's port (Et20/1 for 10.6.40.53) must appear under the group; the
interface counters rise by the stream's rate (a 1080p50 VTX ≈ 2.1 Gbps).

## 5. Take it down

Set both legs to `0.0.0.0` (or `enable` 0). The IGMP leave follows within
the querier interval; `pkt_cnt` stops climbing. That transition is the
"no signal" event the module's syslog reports (`self/syslog`, event
`no_signal`), which is what the later SNMP-trap layer will carry.

## Troubleshooting

| Symptom | Check |
|---|---|
| `mnset connect 10.6.40.53:80: … connection refused` | wrong VLAN / module off; `discover` the range |
| `module answered 400 key 'x' not found` | the field name is not what the firmware expects: `walk` and copy the path |
| `set` succeeds, `get` shows the old value | the module reverted it (licence / program type); read `self.license`, `diag.flow` |
| picture on HDMI but no audio | audio receivers (2 per channel) not pointed: ports 30000 on the Neuron |
| Arista shows no group | `enable` still 0, or the leg IP is on the wrong VLAN (RED 10.6.40/640, BLUE 10.7.40/740) |
