# MNSet — Riedel MN SET / MuoN eMSFP / Fusion connector

Atomic wire/API context for the MNSet connector. Read this before touching
`internal/MNSet/`. The upstream is **REST/JSON over HTTP**, not a binary
wire protocol — there is no `codec/`.

## What it is

**MN SET** is Riedel's north-bound management application for MuoN eMSFP
and Fusion devices (MediorNet family). It runs as a server (seen at
10.6.250.105) and fronts one or more physical devices. A **Fusion 6**
(`2110 Encap/Decap - F6`, EmBOX6) is the device behind it in the lab: a
ST2110 ↔ SDI/HDMI gateway.

Discovered live 2026-09-20 (no vendor OpenAPI exists; endpoints below are
reverse-engineered from the running server + the MN SET manual PDF in
`assets/`).

## Transports / ports

| service | port | proto | notes |
|---|---|---|---|
| MN SET REST (app API) | 8080 | HTTP/JSON | `/api/*` — device, array, appsetting, auth, file, syslog, user |
| NBAPI REST (per-array) | 9080 | HTTP/JSON | `/rest/<array>/<index>/emSFP/node/v1` — only after an Array is created |
| SNMP agent | **1610** | UDP | default; **off until an Array is created + SNMP Enabled** (manual p47-48). v1 (no community) or v2c (`public`). Monitoring only, no SET |
| SNMP rx-from-devices | 1620 | UDP | MN SET *receives* device SNMP here — NOT a poll port |
| SNMP trap | configurable | UDP | Trap IP + Trap Port set per array |

**Do not** poll SNMP on 1620 — that is inbound device→MN SET. Poll 1610,
and only after SNMP is enabled on an array (`/api/array` must be non-empty).

## Auth

`/api/authentication/login` (POST) → token; `checkToken`, `logout`.
`/api/appsetting` returns 403 without a token. `/api/device` is readable
without auth on this deployment. Store creds per `.secrets/` (KV v2), like
cerebrum/ccm.

## Object model (`GET /api/device` → array of devices)

One Fusion 6 device carries 44 top-level keys. The ones that matter:

| key | meaning |
|---|---|
| `info` | type, serial, base_type (FusioN6), input/output_media (st2110, sdi) |
| `senders` (12) | `{id, device_id, flow_id}` — thin; detail is in `flows` |
| `receivers` (36) | `{id, device_id, flow_id}` — thin; a receiver subscribes via its flow |
| `flows` (96) | the real record — `network[]` holds the ST2110 subscription (see below) |
| `sdiOutputs` (8) | SDI/HDMI output routing + audio channel config |
| `sdiAudios` (6), `sdi` (2) | SDI audio + input config |
| `ports` (6), `sfps` (6) | physical cages; HDMI is `MN-Z-SFP-1T-HDMI-1.4` in sfps 0,1,3,5 |
| `programs` (3) | loaded FPGA app, e.g. `MN-FusioN-6-B-APP-25-2110-SDI-2R6T-N` |
| `nmos` / `diagNmos` | IS-04/05 state |
| `interfaces`, `ipconfig`, `sfps` | media network |

### The subscription record (`flows/<uuid>` → `network`)

A receiver/sender has two flows — `flow_id.0` primary (RED) and `flow_id.1` secondary (BLUE) — each with ONE `network` record (only the 4 `route/bulk` flows carry a `network[]` array). To make a receiver pull a stream, edit `network` on both flows:

```
dst_ip_addr   multicast group to join   (e.g. 239.131.3.134 = Neuron VTX-01 RED)
dst_udp_port  20000                     (match the sender)
igmp_src_ip   sender source IP for SSM  (e.g. 10.6.40.50 RED / 10.7.40.50 BLUE)
rtp_pt        96
enable        1
```
RED + BLUE = the two flows (ST 2022-7). Default unconfigured is
`192.168.0.1:10000 → 239.0.1.2:20000`.

## HDMI monitoring of a Neuron output (the use case)

Fusion receives the Neuron's ST2110 sender and drives an HDMI SFP so an
operator sees it on a monitor. Chain: pick a Fusion **receiver** → set its
**flow.network[]** to the Neuron sender's multicast (RED+BLUE) → route that
receiver to an **sdiOutput** wired to an HDMI SFP. See
`docs/hdmi-monitor-config.md`.

## The connector (`consumer/`, package `mnset`) — issue #1110

Consumer only. **The producer is PARKED by the codeowner's decision**
(2026-09-24) — not missing, not pending discovery, and not to be raised
again; if a producer is ever wanted it gets built then. See
[`docs/scope.md`](docs/scope.md) §5. It bypasses MN SET: `http://<module>/emsfp/node/v1/…`
directly (every one of the 30 node resources answered on the module itself,
verified 2026-09-20 on FusioN6 fw 0x68cd783f). MN SET is asked for one thing,
its device list (`Inventory`, login = raw-text password body, `X-AUTH-TOKEN`).

| Piece | Where | Rule |
|---|---|---|
| tree rules + writability | `resources.go` | the module's own LISTINGS (`["name/",…]`) drive the walk and path resolution — no catalogue in code; `writable` (by root) gates `set` and `Object.Access` |
| document → objects | `flatten.go` | keys sorted, arrays by index, `json.Number` kept so ints PUT back as ints; `null` → `KindRaw` |
| frame | `frame.go` | host = MN SET (:8080) → one slot per managed module, ordered by module MAC; `/api/device` gives the list + ONLINE/OFFLINE + media IP, the data still comes from each module; slot status: present (answers) / error (MN SET says ONLINE, module silent) / no_card (OFFLINE); login only if `/api/device` is token-gated (`MNSET_USER`/`MNSET_PASS` env, via `SetCredentials`) |
| plugin | `plugin.go` | host = module → one slot (0); port 0 tries the module on 80 then MN SET on 8080; `Walk` descends listings from the root (depth ≤ 6), unserved resources are deviations; `resolve` follows listings token by token; `SetValue` = resolve → replace field → PUT whole doc to the item URL → GET read-back (the read-back is the answer) |
| sweep + inventory | `discover.go` | `Discover` probes `self/information` per address (worker pool, sorted by IP); `Inventory` = MN SET `/api/device` |
| CLI | `cmd/dhs/cmd_mnset.go` | `discover`, `inventory` only; every other verb is the generic one through the registry |
| dissector | `wireshark/dhs_mnset.lua` | post-dissector over http; filter `dhs_mnset` |

Path grammar: listing tokens then the JSON path, dotted or indexed —
`flows.<uuid>.network.dst_ip_addr` (one `network` record per flow; a receiver's RED and BLUE are two flows, `receivers.N.flow_id.0/1`);
`self.ipconfig.hostname`, `self.diag.flow…`. Text items (`sdp.<uuid>`) are
one string leaf; there is no `diag` root (404) — it is `self/diag`.

Identity (ADR-0022): `<base_type>@<current_version>` → `FusioN6@0x68cd783f`.
No push channel on the module → `Subscribe` **polls**: `pollwatch.Poller`
(neutral, `internal/consumer/pollwatch`) runs the ADR-0030 monitor over the
dictionary's `poll` plan (`dm/fusion6.json`: patterns → intervals) expanded
on the walked tree; slot -1 = every present slot; a frame-wide watch also
re-reads MN SET's device list every 10 s and emits `slot` events. Fetched
documents are cached 2 s so polled leaves of one record cost one GET
(`SetValue` always reads fresh). Module-raised events travel by syslog.
Coverage floor 100 % in CI (mnset and pollwatch).

## What NOT to do

- Never poll SNMP on 1620 (inbound port); use 1610 after enabling.
- Never expect NBAPI `/rest/...` to work with zero arrays — create one first.
- `receivers`/`senders` are thin pointers; never read config from them —
  resolve through `flows`.
- ST2110 uses two legs (RED/BLUE); always set both `network[]` entries.
- Never poll SNMP on the module itself: it has no agent (UDP 161 port-unreachable, no SNMP keys in its DM); it only emits towards MN SET. Events come from the module's own syslog (`self.syslog.*`), values from REST polling.
- Never `set` the six `flows.<uuid>.format.format_code_*` one by one: the module validates the tuple on every PUT. Write the node with a JSON object (`--path flows.<uuid>.format --value '{…}'`) — one merged PUT. Codes: `testdata/video-formats.json` (from MN SET's bundle). The ST 2110 program has no SD format; 625i50 is refused.
