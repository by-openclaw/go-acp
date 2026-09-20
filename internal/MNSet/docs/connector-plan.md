# MNSet connector — DM, watch, metrics, SNMP/NBAPI plan

Grounded in what the live device (10.6.40.53) and MN SET (10.6.250.105)
actually expose, surveyed 2026-09-20. This is the build plan; full Go
implementation follows the normal issue→PR workflow (ADR-0014) and the
metrics work stays aligned with the deferred metrics refactor.

## 1. The DM problem — metadata is NOT in REST

The device REST (`http://<ip>/emsfp/node/v1/<endpoint>`) returns **values
and structure only**. Verified across `self, port, flows, interfaces, phy`:
**no minimum / maximum / description / unit / enum** anywhere. This is the
opposite of acp2 (pid/plen typed props) and the Neuron REST (OpenAPI
schema with minimum/maximum/enum). So a REST-only DM is a **value dump with
inferred types**, not a constrained schema.

Where the real metadata lives:

| source | gives | cost |
|---|---|---|
| device REST | values, structure, current state | free, open, no auth |
| **SNMP MIB** | **OID DESCRIPTION, SYNTAX INTEGER(min..max), units, enums** | needs SNMP enabled + MIB export (auth) |
| NBAPI (9080) | same values as REST, per-array | needs array + auth |
| vendor OpenAPI | — | does not exist |

**Conclusion: the DM is built in two layers.**
- **DM-v1 (now, no auth):** walk every `emsfp/node/v1` endpoint, emit one
  row per leaf: `path, value, kind (inferred), endpoint`. Same CSV columns
  as the other connectors, with min/max/description/enum **blank**.
- **DM-v2 (full):** parse the **SNMP MIB** for each OID's DESCRIPTION +
  SYNTAX range + units, key it to the REST path, and fill the blank columns.
  The MIB is the only machine-readable metadata Riedel ships.

## 2. DM export

`dhs consumer mnset export <mnset-host> [--device <ip>] --format csv`

1. `GET /api/device` (MN SET) → device list, or `--device 10.6.40.53` direct.
2. For each device, walk the 19 working node endpoints:
   `self port flows sources receivers senders route devices sdi sdi_output
   sdi_input sdi_audio sdp receivers_sdp senders_sdp clean_switch refclk
   lldp telemetry`.
3. Flatten each to `path=value`, infer kind (bool/int/float/string/uuid).
4. Emit the standard DM CSV; merge MIB metadata when available (DM-v2).

Identity key (ADR-0022): `FusioN6@<current_version>` from `self.information`
(base_type + sw version), e.g. `FusioN6@0x68cd783f`. One DM file per program
type (`2110-SDI-2R6T` vs `GTW_2110_8ch`).

## 3. Watch

No native push (Riedel emSFP has no WebSocket, no IS-07). Two poll modes:
- **REST timer-interval** (default, like CCM): poll the changing endpoints
  (`flows`, `telemetry`, `refclk`, `port` DDM, `receivers/active`) per-OID
  intervals via the ADR-0030 monitor. Fast-changing: flow pkt_cnt, SFP
  power. Slow: link, PTP, licence.
- **SNMP trap/poll** (after enable): the agent on **1610** for polling, and
  traps to a configured Trap IP:Port for change events. Monitoring only,
  no SET (per manual).

NMOS side: the device's own IS-04 v1.2 / IS-05 v1.0 on `:80/x-nmos` is the
clean surface for sender/receiver state + connect (already exercised live).

## 4. Metrics to collect

Standard connector metrics (`ConnectorMetrics`) at the HTTP session layer:
rx/tx requests+bytes, latency p50/p95/p99, decode/HTTP errors, reconnects,
poll interval health. Plus device-native metrics harvested from these
endpoints:

| metric | source endpoint | field |
|---|---|---|
| stream packet count / lock | `flows` | `network[].pkt_cnt`, `switch_state` |
| receiver active / decoding | `receivers` + IS-05 `/active` | `master_enable`, `subscription` |
| **SFP DDM health** | `port` / MN SET macs status | temperature, vcc, tx_bias, tx/rx power, alarm/warning flags |
| link | `port` | `link`, `speed` |
| bandwidth | MN SET `/api/macs/{id}/status` | actual/expected Rx/Tx, **fecCorrected/Uncorrected, packetDropRate** |
| PTP | `refclk` | `status`, `locked_interface`, `delay_req`, `announceReceiptTimeout` |
| NMOS | `:80/x-nmos` node self | registry status, connection count |
| warnings | `telemetry/warnings` | device warning list |

These map cleanly onto the Prometheus/Grafana model already built (heap/cpu
per instance + connector counters + a per-device panel), when the metrics
refactor lands.

## 5. Enable SNMP + NBAPI — the concrete steps

Both need MN SET auth. Login is `POST /api/authentication/login/{user}`
body `{"password":"…"}` — verified working, but current `.secrets/mnset.json`
creds are **rejected** (that account is set at MN SET install; the fabric
password does not apply). Once real creds are in:

**SNMP (for MIB metadata + monitoring):**
1. token = login.
2. Create an **Array** grouping the device(s) — MN SET backend
   (array create endpoint; UI "Array" tab).
3. **Enable SNMP** on the array (agent starts on UDP **1610**).
4. Wait ~5 min; **export MIB** (zip of MIB modules) → parse for DM-v2.
5. Poll `1610` v1 (no community) or v2c (`public`); traps to Trap IP:Port.

**NBAPI (per-array REST at 9080):**
- After the array exists, `GET/PUT http://<host>:9080/rest/<array>/<idx|0>/
  emSFP/node/v1/<endpoint>` — same resources as the direct device REST, but
  proxied and array-scoped, with auth.

**`/api/appsetting`** (token-gated) exposes/edits the live REST, SNMP agent,
UDP-1620 and trap ports — read it first to confirm the running port config.

## 6. Build order (when approved + creds in)

1. Connector skeleton (`consumer.Protocol` + Factory), HTTP session with the
   `ConnectorMetrics` accessor, `.secrets/mnset.json` auth.
2. `export` (DM-v1) — REST walk → CSV, identity `FusioN6@<ver>`.
3. `watch` — ADR-0030 monitor over the poll endpoints, per-OID intervals.
4. SNMP enable + MIB parse → DM-v2 (fills min/max/description).
5. NMOS pass-through for sender/receiver connect (reuse the nmos consumer).
6. Wireshark: HTTP/JSON is self-describing; the emSFP node is on :80, MN SET
   on 8080/9080 — a dissector is optional (REST), SNMP once enabled.
7. Metrics wired per §4 into the shared Prometheus path (post-refactor).

## Blockers (need you)

- **MN SET admin creds** (the install-time account) → unlocks appsetting,
  array create, SNMP enable, MIB export, NBAPI. Everything metadata- and
  monitoring-rich hangs off this.
- Decision to start the Go implementation (issue + branch per ADR-0014);
  the metrics layer stays deferred until the metrics refactor.
