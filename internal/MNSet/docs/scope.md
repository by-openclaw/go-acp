# MNSet connector — scope of work (consumer only, no producer)

Decided from the live device on 2026-09-20. No code until this scope is
accepted; implementation then follows ADR-0014 (issue → branch → tests →
PR → CI → codeowner) and ADR-0025 (six deliverables).

## 0. Reality the scope is built on

- Fleet in the MN SET discovery range (10.6.40.50-99 / 10.7.40.50-99):
  **one** module today — `10.6.40.53`, FusioN6, sn 125061600012, fw
  0x68cd783f, app `2110-SDI-2R6T`. The "4 modules" are its 4 HDMI SFPs
  (cages 1/2/4/6); cage 3 = media fiber, cage 5 empty. The connector must
  still be generic: N modules × 6 cages × any SFP type.
- The module answers **every** resource directly, no MN SET in the path:
  streaming `self port flows sources receivers senders route devices sdi
  sdi_output sdi_input sdi_audio sdp receivers_sdp senders_sdp
  clean_switch refclk lldp telemetry` and system `self/{information diag
  firmware phy interfaces ipconfig static_route license system syslog
  protocols}`, plus `diag/{common firmware dns flow packet_interval_time
  devices 2110-7_engine refclk nmos}`.
- It is on the **media VLAN**: reachable from the LXC (Linux), not from
  the desk. The connector runs where a media-VLAN route exists.
- NMOS IS-04 v1.2 + IS-05 v1.0 on the module at `:80/x-nmos` (proven:
  receiver connect + fabric join).
- Syslog is already configured **from the module** to `10.6.250.101:514`
  with a full event catalog, and **nothing listens there** today.

## 1. Contract point 1 — REST connector, direct to the module

**Answer: MN SET is bypassed.** It is not a gateway for control. It stays
an *optional inventory source* (`GET /api/device` after login) and the
only place for arrays/SNMP/NBAPI/backup/presets — none of which the
connector needs to control a module.

Package `internal/MNSet/` (ADR-0001), HTTP/JSON, no `codec/` (ADR-0006
has nothing to lift). Registers `consumer.Register` as `mnset`.

**Verbs (ADR-0002 canonical set):**

| verb | what it does on the module |
|---|---|
| `info` | `self/information` + `self/system` + `self/firmware` (identity, app slot, uptime, temp, fan) |
| `walk` | every resource above → flat objects |
| `export` | DM (ADR-0022): identity `FusioN6@<fw>`, per program type; CSV/JSON/YAML like the other connectors; **DM-v1 = values+types** (REST has no min/max/description) |
| `get` / `set` | any leaf: read-modify-write PUT on the owning resource |
| `import` | apply a CSV/JSON (full or partial) — same shape as acp2/ccm, the Neuron essence-plan pattern |
| `watch` | ADR-0030 monitor: per-resource intervals (fast: flows, telemetry, refclk, receiver active; slow: port DDM, system, license) + the syslog event stream as the async channel |
| `health` / `status` | session + module state |
| NMOS pass-through | sender/receiver connect via the existing `nmos` consumer on `:80` (`--node`) |

**Settings surfaces the connector owns** (all confirmed writable resources):
ipconfig (IP/DHCP/VLAN/hostname), protocols (mDNS, SAP), refclk (PTP mode,
delay_req, timeouts), syslog (server/port/enable + monitoring flags),
system (reboot, config/counters reset, flex_port_mode, IGMP version,
2022-7 class), static_route, phy/interfaces (FEC, per-port), sdi_output /
sdi_input / sdi_audio (audio channel map, VPID, loss-of-input mode),
flows (the ST2110 legs: dst ip/port, igmp_src_ip, enable), receivers /
senders. Read-only: license, firmware slots, lldp, diag/*, telemetry.

**Metrics (device-native, harvested into the standard ConnectorMetrics):**
telemetry/{node,ports,devices,warnings}; port DDM (temp, vcc, bias, tx/rx
power, alarm/warning bits); flows `pkt_cnt` / `switch_state` per leg;
refclk status/locked_interface/delay_req; system core_temp,
core_voltage, fan_speed, uptime; MN SET macs status (bandwidth, FEC
corrected/uncorrected, packet drop) when MN SET is used. Exposed through
`--metrics-addr` like every consumer (metrics layer itself stays on the
deferred refactor).

**Logs / the REST-native "trap":** the module emits syslog events —
common: ptp_event, temp_event, northbound_api_event,
rtp_timestamp_audio_event, fan_speed · encap: sdi_event, **no_signal** ·
decap: output_flywheel, memory_pkt_error, dash7_fifo_error,
**flow_impairment**, frame_repeat, frame_skipped. Scope: `set` the syslog
target to our promtail receiver (1514 RED / 1515 BLUE) so every event
lands in Loki labelled by device, and `watch` subscribes to that stream.
This delivers your "mcast set → stream; 0.0.0.0 → loss → event" test
without SNMP: PUT the receiver flow, observe `no_signal` /
`flow_impairment` in the log.

**Validation (ADR-0025 tier 2/3, real device, Ansible):** the HDMI
monitor test you are wiring, the loss-of-signal event test above, and
the IRD-antenna-lock test — all read back on the module, the fabric and
Loki.

## 1a. The per-Fusion setup recipe (from the RTBF procedure, MN-Set §)

The procedure "Ajout d'équipement — Riedel Fusion 6" fixes the order and
the constraints. Each manual step maps to a REST resource the connector
already reaches directly, so `setup` is an `import` with this recipe:

| step (procedure) | REST resource on the module | rule |
|---|---|---|
| physical: SFP on 10/25G, **FEC OFF** both sides, DHCP first | `self/phy` (fec), `self/ipconfig` | port must come up before anything |
| network: fixed IP + the **2 LIVE addresses** (RED/BLUE), gateway/port checked | `self/ipconfig`, `self/interfaces` (e1/e2) | enter unicast **before** un-ticking DHCP; device reboots |
| rename: Device Name = MediaConf name (e.g. `br-s-rfus6-001`) | `self/ipconfig.hostname` | **never rename after NMOS is configured** — Cerebrum IP Routing is built from the NMOS label |
| PTP: domain **100** on both connections, verify GM MAC (RED …6d:3a / BLUE …6d:3b), both fibres "Present" | `refclk`, `diag/refclk` | apply, then verify lock |
| NMOS: mode **Manual**, registry per entity (FCR/CONTRIB/HA/Playout/SI/NOC…) | `diag/nmos` + NMOS config resource | registry list is per site/preset |
| flows: HDMI Fusion → **activate output channel 2 only** (not 1); non-HDMI → activate every flow per channel | `flows[].network[].enable`, `receivers`/`senders` | matches the even-channel decode we verified |
| syslog target | `self/syslog` | our promtail (1514/1515) |

The upstream pipeline the procedure describes is exactly what we already
hold: **MediaConf** owns the flux plan (its fields Split / Type / Flux /
R-S / Input-Output **are the xlsx columns** we parsed), generates the
RED/BLUE multicast per sender, and can "push multicast to the device"
over NMOS; **Cerebrum** adds NMOS UUID → SubId / User Label → IP Routing.
So the connector's `import` consumes the MediaConf export, and the NMOS
check verifies what Cerebrum will see.

## 2. Contract point 2 — SNMP (after the REST connector)

Reuses `internal/snmp` (v1/v2c/v3 manager + trap-listen, already built).
Scope: (a) enable + array + NBAPI bind moved off 127.0.0.1 in MN SET
(your clicks); (b) collect the MIB — the MN SET export zip, plus the
website you will point me to for the missing modules; (c) MIB → DM-v2
(fills min/max/description/enum from OID SYNTAX/DESCRIPTION); (d) poll
1610 + traps to 162 through our trap-listen, correlated with the syslog
events. **Needs from you:** the MIB website URL / files.

## 3. Contract point 3 — NBAPI (after SNMP)

Read the NBAPI tech doc you share, then: array create, bind to the host
IP, `9080/rest/<array>/<idx|0>/emsfp/node/v1/<resource>` with auth. Same
resources as the direct REST, array-scoped — useful for many modules via
one endpoint and as the SNMP scope. Not required for single-module
control. **Needs from you:** the NBAPI doc.

## 4. "Do what Riedel does" — feasible on the same REST

| MN SET feature | our equivalent |
|---|---|
| discovery range → inventory | `discover` sweep of a range for `emsfp/node/v1/self/information` |
| Rest page GET/PUT + presets | `get`/`set`/`import` |
| backup / restore | `export` / `import` DM |
| CSV export / apply, network spreadsheet | DM CSV export / import |
| audio mapping templates | `sdi_audio` / `sdi_output` maps via import |
| firmware upgrade, arrays, users | out of scope for the consumer (admin functions) |

## 5. Deliverables per ADR-0025 (what "done" means)

1. codec — n/a (HTTP/JSON); session + typed resource models
2. consumer — the verbs above, 100 % coverage floor like acp2/ccm
3. producer — **PARKED, by the codeowner's decision** (2026-09-24). Not
   missing, not deferred pending discovery, not an oversight to be
   raised again: this connector is consumer-only until somebody asks
   for a producer, and if that happens it gets built then. ADR-0025's
   deliverable 2 does not apply to mnset while this stands.
4. Wireshark — `dhs_mnset.lua` for HTTP/JSON on :80 / 8080 / 9080 (CLAUDE.md
   requires a dissector per plugin; REST is self-describing so it is thin)
5. testdata — `device.json`, `mnset-dm.csv`, `fusion-nmos-node.json`,
   endpoint lists (already captured), plus fixtures per resource
6. docs — `internal/MNSet/{CLAUDE.md, docs/README, consumer, runbook}` +
   ADR-0028 artifact layout; integration via Ansible from `dhs-debian`

## 6. Open items before the first line of code

- the six screenshots as files (I could not view them inline)
- MIB website URL (point 2), NBAPI tech doc (point 3)
- agree the connector runs on Linux (LXC / media-VLAN host)
- agree the syslog-to-Loki step is the alarm path for the REST phase

## Decisions 2026-09-20 (operator) — what goes through MN SET and what does not

| channel | path | status |
|---|---|---|
| control / DM (REST) | **module direct**, `http://<module>/emsfp/node/v1` | done (this PR) |
| syslog events | **module direct**: `self.syslog.config.{server,port,enable}` + `self.syslog.monitoring.*` flags → our promtail `:1514` → Loki | **verified live**: after enabling the event classes and provoking a 15 s flow loss on CH2, Loki holds `host=emsfp-a2-10-0c` lines "Video frame repeated error on device 1/2 {Rate…}", "Flywheel sdi_load event occurred on device 2" |
| SNMP | **the module has no SNMP agent**: UDP 161 answers ICMP port-unreachable (v1 and v2c, from 10.6.250.101), and its DM carries no SNMP/trap setting. The module only *emits* SNMP towards MN SET's 1620; the pollable agent (1610) and traps are MN SET's. | parked: only possible through MN SET, which the operator does not want |
| NBAPI (`:9080`) | MN SET only | parked, in case of future need |

Consequence for monitoring: events = syslog (module → promtail → Loki), values = REST polling (ADR-0030 monitor). No SNMP layer on the module side.
