# EVS Neuron provisioning — dhs only

How to take an EVS Neuron (REST `/api/v1`, connector `ccm`) from a fresh
firmware load or reset to RED/BLUE ST 2110 senders routed from its inputs,
**with dhs alone** — no EVS GUI step.

Status: **✅ verified** on the lab Neuron, CONVERT Hybrid 7.0.3
(10.6.255.102), 2026-09-28/29; **⚠ unverified** otherwise. Needs dhs
with #1186 (numbers written as numbers, read-back answers).

## 1. Access — management port, not in-band

| Path | Serves | Status |
|---|---|---|
| Management 10.6.255.102 (VLAN 600, pfSense DHCP, gateway 10.6.255.254) | REST `:443`, GUI + stream preview, NMOS `:3000` | ✅ |
| In-band RED 10.6.40.50 / BLUE 10.7.40.50 (via the RED VRF leak) | **NMOS `:3000` only** — `:80/:443` time out on the device | ✅ (question open with EVS) |

So **provisioning goes through the management address**. In-band is the
NMOS control path only.

## 2. Find the objects

One export lists every sender, receiver, channel and MAC with its uuid:

```bash
dhs consumer ccm export 10.6.255.102 --format csv --out neuron.csv   # ~8 s, ~41 600 rows
grep 'io\.ip\.senders\.video\..*\.name,' neuron.csv                 # uuid ↔ "Output Video Stream n"
```

## 3. Media links — do this first after any firmware change

A firmware load **resets the media MACs** to `fec: Off` + DHCP. The switch
runs 100G with Reed-Solomon FEC, so the links stay **down** and nothing
else works until FEC matches. ✅

```bash
dhs consumer ccm set 10.6.255.102 --path misc.macs.<mac1-uuid>.fec --value ReedSolomon   # RED  (Control Port Mac 1, QSFP0)
dhs consumer ccm set 10.6.255.102 --path misc.macs.<mac2-uuid>.fec --value ReedSolomon   # BLUE (Control Port Mac 2, QSFP1)
```

Then the switch DHCP leases **10.6.40.50** (RED) and **10.7.40.50** (BLUE).
Check `misc.macs.<uuid>.status` → `link: true`, `dhcpStatus: Leased`.
Keep `inBandControl: true`.

## 4. Senders — addresses from the plant plan

A firmware load also resets **every sender to `0.0.0.0:12700`**. ✅ Leg 0 is
RED (source 10.6.40.50), leg 1 is BLUE (10.7.40.50).

| Essence | Senders | RED leg 0 | BLUE leg 1 | Port |
|---|---|---|---|---|
| Video | `io.ip.senders.video` 1–8 | 239.1.0.n | 239.65.0.n | 20000 |
| Audio | `io.ip.senders.audio` 1–16 | 239.4.0.n | 239.68.0.n | 30000 |
| ANC | `io.ip.senders.data` 1–8 | 239.8.0.n | 239.72.0.n | 40000 |

```bash
S=io.ip.senders.video.<uuid of "Output Video Stream 1">
dhs consumer ccm set 10.6.255.102 --path $S.legs.0.ip   --value 239.1.0.1
dhs consumer ccm set 10.6.255.102 --path $S.legs.0.port --value 20000
dhs consumer ccm set 10.6.255.102 --path $S.legs.1.ip   --value 239.65.0.1
dhs consumer ccm set 10.6.255.102 --path $S.legs.1.port --value 20000
dhs consumer ccm set 10.6.255.102 --path $S.enable      --value true
```

A write applies at once — the GUI's **Take** is not needed over REST. ✅
The SDP then carries `source-filter … 10.6.40.50` / `10.7.40.50`.
NMOS label and group hint (`$S.nmos.label`, `$S.nmos.groupHint`) come
from the site sheet (xlsx) — ⚠ sheet pending.

## 5. Routing — firmware defaults are not a plant

Defaults after a firmware load ✅: every IP video output takes **CH00**,
and only CH00 has a real input (SDI00); CH01–CH15 take the **pattern
generator** (PAT00).

| Matrix | Path | Example |
|---|---|---|
| Input → processing channel | `matrix.video.path.main.CH<nn>` | `--value SDI02` (SDI Input 3) or `PAT00` |
| Channel → IP output | `matrix.video.output.current.IP<nn>` | `--value CH01` |

```bash
dhs consumer ccm set 10.6.255.102 --path matrix.video.path.main.CH01     --value SDI02
dhs consumer ccm set 10.6.255.102 --path matrix.video.output.current.IP01 --value CH01
```

For many crosspoints use the matrix file-set (one GET + one PUT per
matrix) — see [`../CLAUDE.md`](../CLAUDE.md) "Reading the routing: the canonical matrix file-set". Which SDI inputs carry signal:
`io.sdi.<uuid>.videoFormat` (`NA` = no signal).

## 6. Video processing — deinterlace

Deinterlacing needs **all three** on the channel (HQ deinterlacer exists
on channels 1–2 of each path only) ✅:

```bash
C=processing.video.channels.<channel uuid>
dhs consumer ccm set 10.6.255.102 --path $C.hqDeinterlacer        --value true
dhs consumer ccm set 10.6.255.102 --path $C.scaling.conversionMode --value "Up/Down/Cross"
dhs consumer ccm set 10.6.255.102 --path $C.videoFormatOutput      --value 1080p50
```

`hqDeinterlacer` alone leaves the picture interlaced. Colour follows the
source when `hdrConversion.colorSpaceMode` is `Auto` (a Rec.2020-flagged
source publishes `colorimetry=BT2020`).

## 7. Verify

| Check | Where | Expect |
|---|---|---|
| Sender on air | `io.ip.senders.video.<uuid>.status` | `sending: true` on both legs |
| Bandwidth | switch `show interfaces Ethernet10/1,22/1 counters rates` | ≈ 1.95 Gb/s per 1080p50 stream per plane |
| Receivers downstream | FusioN syslog `{job="syslog", msg_id="decap"}` | "Packets are now received … primary/secondary" |

Receivers are connected from the receiving side over IS-05:
`dhs consumer nmos connect --node <receiver node> --receiver <uuid>
--sender <neuron sender uuid> --sender-node http://10.6.255.102:3000`
(see the FusioN guide, `internal/MNSet/docs/provisioning.md` §6).

## 8. After a firmware change

1. Fetch `https://<ip>/api/v1/docs/api.yml` and diff it against
   `internal/ccm/codec/testdata/` (BRIDGE 7.0.3 → CONVERT Hybrid 7.0.3:
   identical, 111 operations).
2. Redo §3 → §6: the load resets FEC, DHCP mode, senders and routing.
