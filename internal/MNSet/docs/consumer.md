# `dhs consumer mnset` — CLI walkthrough

Two shapes, same verbs (ADR-0022 frame / slot / card):

| host | shape |
|---|---|
| a module (`10.6.40.53`, port 80) | one slot, **0** |
| MN SET (`10.6.250.105 --port 8080`) | the **frame**: one slot per managed module, ordered by module MAC; `info` prints the map (slot → id / ip / serial / lldp / MN SET status) |

Port 0 (the default) tries the module on 80, then MN SET on 8080. In
both shapes a module's channels (CH1..CH8 on a FusioN6) are not slots;
they are entries inside the `devices`, `receivers`, `senders` and
`flows` resources, addressed by path. With a frame, add `--slot N`.

Slot status: `present` = the module answers; `error` = MN SET lists it
ONLINE but it does not answer; `no_card` = MN SET lists it OFFLINE. The
frame reads `/api/device` unauthenticated (as this deployment serves
it); a token-gated MN SET is logged into with `$MNSET_USER` /
`$MNSET_PASS` from the environment — never a flag, never printed.

## Verbs

| Verb | What it does |
|---|---|
| `discover --range R …` | sweep addresses for modules (`self/information` answers) |
| `inventory <mnset-host> --user U` | ask MN SET for the modules it manages (`$MNSET_PASS`) |
| `info <host>` | slots + identity `FusioN6@0x68cd783f` per slot |
| `walk <host>` | every leaf of every resource, as `resource.path = value` |
| `export <host> --format csv` | the same, to a file (the DM export, like acp2 / ccm) |
| `import <host> <file>` | apply a snapshot's writable values (read-modify-write per field) |
| `get <host> --path P` | one leaf |
| `set <host> --path P --value V` | one leaf; the PUT carries the whole resource document |
| `status` / `health` | session health (reachable / connected / live) |

`watch` **polls**: the module has no push channel, so a watch is the
dictionary's poll plan (`dm/fusion6.json` → `poll`: which leaves, how
often — stream counters 15 s, telemetry/PTP/links 10 s, SFP/temperature
30 s, configuration 60 s) expanded over the slot's tree and run by the
ADR-0030 monitor; only changes are printed. The health leaves (packet
counters and rates, sequence errors, node telemetry, refclk status and
locked interface, port link, SFP DDM currents, core temperature, fan
speed) carry `on_change: false`: every sample is published, marked as a
repeat, so the alarm engine sees a condition that PERSISTS (a counter
still stopped, a link still down) while the display still shows changes
only. `--path P` narrows the plan
to that subtree; `--slot -1` (the default) covers every present slot of
a frame. On a frame (MN SET host) the watch also re-reads MN SET's
device list every 10 s and prints slot events (`slot` path: present /
error / no_card / removed) when a module appears, vanishes or falls
silent. Leaves of one record polled in the same 2 s cost one GET.
Events the module raises itself (no signal, PTP, temperature) travel by
its syslog (runbook §6).

## Alarms

`watch` judges each value against the per-model alarm template
(ADR-0033) and prints one `[alarm]` line per verdict change, with its
RFC 5424 severity on the structured sink. The reference template for
this module is tracked at `internal/MNSet/alarm/fusion6.alarm.json`;
install it once per site:

```
dhs consumer mnset alarm import internal/MNSet/alarm/fusion6.alarm.json     --model FusioN6@0x68cd783f          # changed=true, then changed=false
dhs consumer mnset alarm list --model FusioN6@0x68cd783f
dhs consumer mnset alarm test --path refclk.status --value 0
dhs consumer mnset watch <host> --slot 1            # --no-alarm to silence
```

Every other object the module defines is in the view too, as `info`:
the template ends with a catch-all, and a device with no template
at all gets the built-in one, so `watch` shows a severity for every
line rather than hiding what nobody wrote a rule for.

It carries four rows, each naming its source: SFP temperature and
supply voltage for cage 3 (bands read from that optic's own DDM
thresholds — another part number publishes other numbers and needs its
own row), PTP lock (`refclk.status` 3 = locked), and the e1 media link.
Thresholds are site data, so the installed copy lives in the cache
bucket (`.cache/alarm/mnset/`, ADR-0020) and `alarm set` edits it in
place.

## Path grammar

```
<listing tokens…>.<json path…>
```

The module publishes a tree of **listings** (`GET` answers `["name/", …]`)
down to **documents** (JSON) or **text** (the `sdp/`, `receivers_sdp/`,
`senders_sdp/` items are raw SDP). A path is followed the same way the
walk descends: each token that a listing names is one more URL segment;
the first token that lands on a document starts the JSON path inside it.
`a.b` and `a[b]` are the same token, so a path copied from an export and
a path typed by hand agree.

| Path | Meaning |
|---|---|
| `self.ipconfig.hostname` | management hostname |
| `self.syslog.config.server` | where the module sends its events |
| `receivers.N.flow_id.0` / `.flow_id.1` | the receiver's primary (RED) and secondary (BLUE) flow uuids |
| `flows.<uuid>.network.dst_ip_addr` | that flow's multicast (one `network` record per flow; RED and BLUE are two flows) |
| `flows.<uuid>.network.dst_udp_port` | its port (20000 video / 30000 audio / 40000 anc on the Neuron) |
| `flows.<uuid>.network.enable` | flow enabled (1/0) |
| `flows.<uuid>.network.igmp_src_ip` | SSM source (0.0.0.0 = any) |
| `devices.7.label` | channel 8 label (`devices` is a plain array, indexed) |
| `port.1.link` | SFP cage 1 link state |
| `sdp.<uuid>` | the SDP text of that flow |
| `refclk.mode` | PTP mode |

The FusioN6 root lists 19 resources: `self port flows sources receivers
senders route devices sdi sdi_output sdi_input sdi_audio sdp receivers_sdp
senders_sdp clean_switch refclk lldp telemetry`; `self` lists 11 more
(`information diag firmware phy interfaces ipconfig static_route license
system syslog protocols`); listings nest to four (`route.bulk.sender.<uuid>`).

## Examples

```
dhs consumer mnset info 10.6.250.105 --port 8080        # the frame: every module MN SET manages
dhs consumer mnset export 10.6.250.105 --port 8080 --format csv --out fleet.csv   # all slots

dhs consumer mnset discover --range 10.6.40.0/24
IP               PORT  BASE       SERIAL         FW           TYPE                           APP
10.6.40.53       80    FusioN6    125061600012   0x68cd783f   22 - ST2110 UHD Transceiver    MN-FusioN-6-B-APP-25-2110-SDI-2R6T-N

dhs consumer mnset export 10.6.40.53 --format csv --out .cache/exports/fusion-53.csv

dhs consumer mnset get 10.6.40.53 --path flows.59d52e04-…-40a36ba2100c.network.dst_ip_addr
239.0.1.2

dhs consumer mnset set 10.6.40.53 --path flows.59d52e04-…-40a36ba2100c.network.dst_ip_addr --value 239.131.3.134
239.131.3.134            ← the value the module holds AFTER the write, read back
```

## Writes

`set` is read-modify-write: the connector resolves the owning document
(`flows/<uuid>`, never the collection), replaces the one field, PUTs the
**whole document** back to that URL (the module rejects partial bodies),
then reads it back. The value printed is the read-back, never
the request. Types are preserved: a numeric field receives a number, a
boolean a boolean — `--value 20000` on `dst_udp_port` goes out as
`20000`, not `"20000"`.

A refused write surfaces the module's own message:
`mnset PUT flows: module answered 400 key 'x' not found`.

Only these roots accept PUT (the rest are read-only and `set` refuses
after resolving, before writing): `flows receivers senders route sdi
sdi_output sdi_input sdi_audio clean_switch refclk self/ipconfig
self/syslog self/protocols self/static_route self/system self/phy
self/interfaces`. A text item (an SDP) is never written.

## What the export contains

Everything the module lists, descended from the root — no catalogue in
the code, so a firmware that adds a resource exports without a change.
On the FusioN6 that is ~1,100 leaves in ~3 s (≈300 GETs). A resource the
module lists but does not serve is recorded as a deviation (logged at
WARN) and the walk continues. Values only — the API carries no
min/max/enum/description; that metadata lives in the SNMP MIB (later
layer, see `scope.md`).

## Writing a validated tuple (a node) in one PUT

Some records are validated as a whole on every PUT — a flow's video
format is six `format_code_*` fields the FusioN6 checks together
("Flow failed specific application validation" when they disagree).
Single-field `set`s can only pass by accident and leave the record in a
mixed state. Give the node a JSON object instead; the keys you give are
merged over the module's, and the module gets ONE PUT:

```
dhs consumer mnset set 10.6.40.53 --path flows.<uuid>.format   --value '{"format_code_t_scan":0,"format_code_p_scan":0,"format_code_mode":0,"format_code_format":0,"format_code_rate":5120,"format_code_sampling":0}'   # 1080i50
```

The code tables are MN SET's own, extracted to
[`../testdata/video-formats.json`](../testdata/video-formats.json)
(`VIDEO_FORMATS` = the ST 2110 program list, `VIDEO_FORMATS_2022` = the
2022-6 list). The ST 2110 list holds **no SD**: 625i50 is refused by the
module (verified 2026-09-21), so an SD source (the RX1290 at 625) must be
delivered to the Fusion as HD (1080i50 / 720p50 / 1080p50). A list node
takes a JSON array and is replaced whole.
