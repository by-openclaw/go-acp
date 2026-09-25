# Configure a Fusion HDMI/SDI channel to monitor a Neuron output

Goal: an operator sees a Neuron ST2110 sender on an HDMI monitor, via the
Fusion 6 (2R+6T, EmBOX6) behind MN SET. All over REST — no SNMP needed
(SNMP here is monitoring-only and disabled).

## Channel layout (program 2110-SDI-2R6T on this F6)

8 device channels (CH1..CH8) pair onto the 4 HDMI SFPs, 2 signals each:

| HDMI SFP | ch A | ch B | note |
|---|---|---|---|
| HDMI-1 | CH1 decode | CH2 decode | both decode (monitor) |
| HDMI-2 | CH3 decode | CH4 decode | both decode |
| HDMI-3 | CH5 encode | **CH6 decode** | 2nd channel decodes |
| HDMI-4 | CH7 encode | **CH8 decode** | 2nd channel decodes |

Decode = ST2110 → SDI/HDMI (what a monitor shows). Encode = SDI → ST2110.
On the mixed SFPs the **second channel is the decoder** — so to monitor a
Neuron output pick a decode channel: **CH1, CH2, CH3, CH4, CH6, or CH8**.
A decode `sdiOutput` has `is_sdi_input=false`, `input_signal_output_mode.
loss_of_input=freeze`, and `sdi_aud_chans_cfg` mapping 2 audio channels.

## The chain

```
Neuron sender (ST2110 mcast, RED+BLUE)
        │  e.g. VTX-01  239.131.3.134:20000 / 239.132.3.134:20000  src 10.6.40.50 / 10.7.40.50
        ▼
Fusion RECEIVER  (one of 36)  ── subscribes via its FLOW.network[]
        ▼
Fusion sdiOutput / HDMI SFP  (MN-Z-SFP-1T-HDMI-1.4, slots 0,1,3,5)
        ▼
HDMI monitor
```

## Two ways to connect (pick one)

**A. NMOS IS-05 (standard, preferred).** The Fusion exposes its own NMOS
node **on port 80**: IS-04 **v1.2** at `http://<device-ip>/x-nmos/node/v1.2`
and IS-05 **v1.0** at `http://<device-ip>/x-nmos/connection/v1.0`. Node
`emsfp-a2-10-0c st2110 node`, `href http://10.6.40.53:80/`, 8 devices (=
CH1..CH8), 36 receivers per channel: `VidRx 000 / AudRx 010,020,030,040 /
AncRx 050` (CH1), `VidRx 100…` (CH2)…

To monitor a Neuron output on an HDMI decode channel, IS-05-connect that
channel's `VidRx` (and `AudRx`) to the Neuron sender — one PATCH:
```
PATCH /x-nmos/connection/v1.0/single/receivers/{receiver-id}/staged
  { "sender_id": "<neuron sender uuid>",
    "master_enable": true,
    "activation": { "mode": "activate_immediate" },
    "transport_params": [ {RED leg}, {BLUE leg} ] }   # or push the sender's SDP via transport_file
```
`active`/`staged`/`constraints` are all live (200). Do it with
`dhs consumer nmos`, cerebrum-nb, or any IS-05 controller — no proprietary
field, no registry needed (peer-to-peer at the node).

**B. Riedel REST** (`flows[].network[]`) — the low-level equivalent, below.
Use it when a resource has no NMOS mapping (e.g. SDI audio channel routing).

### Discovery / mDNS

The NMOS **registry mDNS is ON** and Registry Mode = **Auto**, Control
Network = **Media** (VLAN 640). Status sits at **DISCOVERING** because no
registry advertises `_nmos-registration._tcp` on the media network
(Registry Address 0.0.0.0, uptime 0). This does **not** block monitoring —
IS-05 peer-to-peer at `10.6.40.53:80` works without a registry. To make the
device register (for controller auto-discovery): either run a registry on
VLAN 640, or set Registry Mode = Manual + registry IP. NB: the REST
`protocols.mdns_enable` is the device SAP/mDNS (essence announce), a
DIFFERENT knob from this NMOS registry mDNS. The separate NMOS at
10.6.40.3:3000 was powered off during this survey.

## Step 1 — pick a receiver + its flow

`GET /api/device` → `receivers[]` (thin `{id, device_id, flow_id}`). Take the
target receiver's `flow_id`, find that entry in `flows[]`. Each flow has a
`network[]` with two entries = RED leg [0] + BLUE leg [1].

## Step 2 — point the flow at the Neuron sender

Set, per leg, in `flows[].network[i]`:

| field | RED (leg 0) | BLUE (leg 1) |
|---|---|---|
| `dst_ip_addr` | 239.131.3.134 | 239.132.3.134 |
| `dst_udp_port` | 20000 | 20000 |
| `igmp_src_ip` | 10.6.40.50 (sender RED src, SSM) | 10.7.40.50 |
| `rtp_pt` | 96 | 96 |
| `enable` | 1 | 1 |

`igmp_src_ip` set = the receiver issues an IGMPv3 **source-specific** join,
which is what the Arista snooping fabric forwards (we proved this join
mechanism live on the Neuron loop test). Leave `dst_mac` to the device or
set the standard 01:00:5e mapping of the group.

## Step 3 — route the receiver to an HDMI output

`sdiOutputs[]` carry the output routing (`sdi_aud_chans_cfg`, `vpid`,
`input_signal_output_mode`, `is_sdi_input`, `color_bar`, `line_offset`).
Bind the chosen output to the receiver so the decoded video reaches the
HDMI SFP cage. The HDMI SFPs are the four `MN-Z-SFP-1T-HDMI-1.4` in slots
0,1,3,5 — one per monitor. Channels are `Device CH1..CH8`.

## Step 4 — apply

PUT the modified device object back through MN SET (or the NBAPI
`/rest/<array>/<idx>/emSFP/node/v1` once an array exists). Read back
`GET /api/device` and check the receiver's flow `switch_state` leaves
`idle` and the HDMI output shows the signal.

## Verify (optional, no extra tooling)

- Fusion receiver flow `network[].pkt_cnt` rises = packets arriving.
- On the fabric, the group appears in `show mac address-table multicast
  vlan 640` on the Fusion's port (same check that passed for the Neuron).
- NMOS: the Fusion's IS-05 receiver shows the subscription (via `nmos` /
  `diagNmos` in the device model).

## Notes / gaps

- MN SET is the only reachable control plane from mgmt; the module's own
  IPs (10.6.40.53 media, 172.16.16.2, 192.168.40.230 oob) are not routed
  off their segments.
- `PUT` shape and exact receiver→output binding field are not in a vendor
  OpenAPI; capture a working change from the MN SET UI (Rest page GET/PUT,
  manual §7) once and pin it as a fixture under `testdata/`.
- Two legs always (ST 2022-7). One leg = no redundancy and asymmetric
  join state on the two fabrics.
