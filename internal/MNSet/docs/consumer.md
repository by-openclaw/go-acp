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

`watch` is not available: the module has no push channel. Poll with the
ADR-0030 monitor, or ship the module's own syslog (`self/syslog`).

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
