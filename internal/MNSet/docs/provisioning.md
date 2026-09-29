# FusioN6 provisioning — in-band, dhs only

How to take a Riedel FusioN6 (emSFP node API) from a fresh or reset
module to a working RED/BLUE ST 2110 endpoint **with dhs alone**. MN SET
is not used for anything: every step below talks to the module's own
REST API (`/emsfp/node/v1`) or its NMOS IS-05 API.

Status marks: **✅ verified** on FusioN6 fw `0x68cd783f` (lab, 2026-09-28/29),
**⚠ unverified** until a factory reset has been observed.

## 1. What must exist before the module is plugged in

The module is managed **in-band only**: control rides on the media ports,
and the switch is what makes it reachable.

| Piece | Setting | Where |
|---|---|---|
| Switch port, RED (e1) | `client_red` profile, **`speed forced 25gfull`, `no error-correction encoding`** (the module runs FEC `none`) | `ansible/sites/<site>/ports/<switch>.yml` |
| Switch port, BLUE (e2) | same on `client_blue` | same |
| Media DHCP | RED 10.6.40.0/24 and BLUE 10.7.40.0/24, range .50–.99, gateway .254 | role `arista_media` |
| In-band path | RED VRF leak ↔ MGMT_CTRL 10.6.240.0/20 (route-maps `RM-RED-TO-MGMT` / `RM-MGMT-TO-RED`) | role `arista_vrf` |
| Control host | a host in 10.6.240.0/20 (today `dhs-debian`, 10.6.250.101) with dhs ≥ the release carrying #1185/#1186 | — |

A desk outside 10.6.240.0/20 cannot reach the module: that is the leak
doing its job, not a fault.

## 2. How the module becomes reachable

| Setting on the module | Value | Meaning | Status |
|---|---|---|---|
| `self.system.flex_port_mode` | `RT` | management (OOB) port off; control over the media ports | ✅ read |
| `self.system.access_control.media.device_management` | `true` | REST allowed over the media ports | ✅ read |
| `self.interfaces.e1/e2.dhcp` | `true` | each media port asks the switch for an address | ✅ read |
| Static fallback e1 / e2 / oob | 192.168.39.230/24 · 172.16.16.2/24 · 192.168.40.230/24 | used when DHCP is off | ✅ read, ⚠ factory value |
| Manual's factory control IP | `10.<MAC byte 4>.<byte 5>.<byte 6>` (MAC `40:a3:6b:a2:10:0c` → 10.162.16.12) | MN SET manual §3, written for MuoN SFP | ⚠ not seen on a FusioN6 |

Link up → DHCP lease on each plane → the module answers on both media
addresses. **Do not change `flex_port_mode`, `access_control`,
`self.ipconfig` or `self.interfaces` remotely**: a wrong value removes the
only path to the module, and the OOB port is not cabled.

## 2b. The objects to provision (CSV)

[`provisioning/fusion6-objects.csv`](provisioning/fusion6-objects.csv) —
the FusioN6 DM cut to what stands a module up (MGMT · RED/BLUE network +
PTP · channels · NMOS): one row per object pattern with kind, access,
values/range, an example, note and source.
[`provisioning/fusion6-modules.csv`](provisioning/fusion6-modules.csv) —
one row per module × concrete object (channel, rx/tx, essence, RED/BLUE
leg decoded), the current value and an empty `target_value`. Every
FusioN6 shares the same DM; regenerate for any set of modules:

```bash
dhs consumer mnset export <ip> --format csv --out <serial>.csv      # per module
python provisioning/objects.py ../consumer/dm/fusion6.json *.csv    # both CSVs
```

NMOS registration (`self.diag.nmos.*`: `registry_mode`, `registry_address`,
`control_network`, `mdns_mode`, DNS) is writable with `dhs … set` ✅
(same-value write confirmed, malformed address refused, 2026-09-29).

## 3. Find the modules

```bash
dhs consumer mnset discover --range 10.6.40.50-99 --range 10.7.40.50-99
```

A module answers on **both** planes (one row per media address). The
**serial** is the identity: de-duplicate on it, and use the RED address as
the control address. ✅

## 4. Read what the module is — never assume

Every module states its own model, sizes and licences. Read both before
planning anything for it.

```bash
dhs consumer mnset get <ip> --path self.information.type           # "22 - ST2110 UHD Transceiver"
dhs consumer mnset get <ip> --path self.information.encap_count    # senders (2)
dhs consumer mnset get <ip> --path self.information.decap_count    # receivers (6)
dhs consumer mnset get <ip> --path self.information.output_media.1 # "sdi" (index 0 = "st2110"; HDMI units differ)
dhs consumer mnset get <ip> --path self.license.feature.black_burst # licensed | unlicensed
```

Licensed features (`self.license.feature`): `clean_switch`, `frame_sync`,
`black_burst`, `uhd_support`. Writing a feature the module is not licensed
for answers **HTTP 402** ✅ (seen on `frame_sync`). Check first; never
provision a feature the module does not have.

## 5. Base settings

| Step | Command | Status |
|---|---|---|
| Syslog to the collector | `set <ip> --path self.syslog.config.server --value 10.6.250.101`, `…config.port --value 1514`, `…config.enable --value true` | ✅ |
| Event classes | `set <ip> --path self.syslog.monitoring.<class>.<event> --value true` for `common.*`, `encap.*`, `decap.*` | ✅ |
| 2022-7 class | `set <ip> --path self.system.smpte_network.2022-7.class --value d` (a / b / d = Class A / B / D) | ✅ read |

The module sends **RFC 5424 over UDP**; APP-NAME is `-`, the event class
is the MSGID (`decap` / `encap` / `common`) → Loki `{job="syslog", msg_id="decap"}`.

## 6. Receivers (RED + BLUE, both legs)

Use **IS-05**: it hands the module the sender's own SDP, so the video
format, both legs and the source filters are set in one activation and
cannot disagree with what is on the wire.

```bash
dhs consumer nmos connect \
  --node http://<fusion-red-ip> \
  --receiver <fusion receiver uuid> \
  --sender <sender uuid> --sender-node http://<sender node:port> \
  --dry-run          # print the PATCH; drop --dry-run to apply
```

- Receiver uuids: `GET http://<ip>/x-nmos/node/v1.2/receivers`; the label
  `VidRx 100/300/500/700` is SDI output CH2/4/6/8 on this model. ✅
- The Neuron's NMOS node is `http://10.6.255.102:3000`. ✅
- Disconnect: same command without `--sender`.
- **Clearing a receiver leaves no trace in the module's syslog** — only the
  IS-05 state shows it. ✅ (verified 2026-09-28)

A receiver whose stored format differs from the stream shows "no signal"
even though packets arrive. Re-connecting through IS-05 fixes it; setting
`flows.*` by hand does not change the format.

## 7. SDI (or HDMI) outputs

| Field | Values | Status |
|---|---|---|
| `sdi_output.<id>.input_signal_output_mode.loss_of_input` | `black`, `freeze`, `blue`, `no_signal` (no `freeze` on UDC models) | ✅ |
| `sdi_output.<id>.vpid.source` | `regenerated`, `override` | ✅ |
| `sdi_output.<id>.vpid.override_value` | `0x` + 8 hex digits | ✅ |
| `sdi_output.<id>.line_offset.frame_buffer` | 0 / 1 | ✅ |
| `sdi_output.<id>.sdi_aud_chans_cfg.ch<N>` | `<audio flow uuid>:<channel>:<n>`, empty = `:0:0` | ✅ format |

HDMI units report it in `self.information.output_media`; only the SDI
FusioN6 is on the bench, so HDMI fields are ⚠ unverified.

## 8. Senders (only when `encap_count` > 0)

Senders are the `tx` flows (on this model CH5 and CH7, fed by SDI inputs
5 and 7). Per leg (`flows.<uuid>.network.*`):

| Field | Range | Status |
|---|---|---|
| `dst_ip_addr` | IPv4 — **the module wraps bad octets** (`999.1.1.1` → `231.1.1.1`); dhs refuses them. A multicast address **re-derives `dst_mac`**, and setting the address back does not restore the MAC | ✅ |
| `dst_udp_port`, `src_udp_port` | 0–65535 | ✅ |
| `ttl` | 0–255 | ✅ |
| `dscp` | 0–63 (the module accepts 64: stay in range) | ✅ |
| `rtp_pt` | 96–127 dynamic (the module accepts 128: stay in range) | ✅ |
| `ssrc` | 0–4294967295 | ✅ |
| `enable` | 0 / 1 | ✅ |

Addressing follows the plant plan: RED video 239.1.0.n, audio 239.4.0.n,
ANC 239.8.0.n; BLUE = second octet + 64; ports 20000 / 30000 / 40000.

## 9. Value reference (proven on the module)

| Field | Wire values |
|---|---|
| `format.range` | NARROW, FULL, FULLPROTECT |
| `format.format_tcs` | SDR, PQ, HLG |
| `format.format_colorimetry` | BT709, BT2020, BT2100 |
| `format.aud_format` | `dash-30` (uncompressed), `dash-31` (compressed) |
| `format.anc_flow_profile` | 1, 2 |
| `cdis` | single, 2si, sqdiv |
| `sdi_input.*.frame_sync_audio.sdi_in_genlock` | freerun, locked |
| `self.system.flex_port_mode` | RT, OOB — **do not change remotely** |
| `format_code_*` (six) | one video format; write them together as one JSON object, never one by one |

Fields the module accepts but ignores (treat as read-only): `id`,
`version`, `type`, `format_type`, `switch_state`, and on a receiver
`pkt_filter_src_mac`, `pkt_filter_ssrc`, `ssrc`.

## 10. Verify

```bash
dhs consumer mnset get <ip> --path self.diag.flow.<rx flow uuid>.rtp_stream_info.0.status.pkt_rate   # ~216000 = 1080p50
```

Loki: `{job="syslog", device="<ip>"}` — "Packets are now received on
device N flow 0 primary/secondary" confirms both legs.

## 11. Factory reset (to learn the factory state) ⚠

`self.system.config_reset` resets the module. What it comes back with
(DHCP on or off, `flex_port_mode`, media management) has **not been
observed yet**. If it comes back with DHCP off or media management off,
the module is unreachable in-band and needs the OOB port. Run it only with
someone at the rack, and record the result in §2.
