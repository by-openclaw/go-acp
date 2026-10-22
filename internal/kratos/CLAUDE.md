# CLAUDE.md — kratos (Kratos NGC2 IDU antenna controller)

## Authoritative spec

- *NGC2 IDU CORE — Application Programming Interface (API) Control
  Document*, Kratos Antenna Solutions, manual 7592827 **Rev. E**
  (June 2023), 144 pages — `assets/7592827 - Application Programming
  Interface(API) Control Document RevE.pdf`. The document is marked
  CONFIDENTIAL AND PROPRIETARY and "PRELIMINARY" (§1.3): it stays out of
  git. Section numbers below are its own.
- The data dictionary is a separate document (§10); "the most accurate
  data dictionary is always obtained by pulling the JSON SVT query from
  the device itself".

## What the device is

The indoor unit (IDU) that points a satellite dish: azimuth, elevation,
polarisation, tracking (steptrack, monopulse, smart track), beacon
receiver, satellite almanac, stow. Consumer only; the producer is
parked (operator, 2026-10-23).

## Transports — there is no WebSocket

| Half | Transport | Port | Purpose |
|---|---|---|---|
| REST (§2.1, §4) | HTTP | 3000 | low and medium security |
| REST | HTTPS | 4000 | high security (port 3000 blocked by iptables) |
| Notifications (§2.2, §7) | MQTT 3.1.1 (ISO/IEC 20922) | 1883 | §6.1 scenarios 1–2 |
| Notifications | MQTT over TLS | 8883 | §6.1 scenario 3 |

**Open question for Kratos:** §7 says the broker listens on "the default
port number (1822)"; the §6.1 diagrams say 1883 / 8883. The connector
defaults to 1883 (8883 with TLS) and takes `DHS_KRATOS_MQTT_PORT`.

## Security models (§2.4, §6)

- low: no login, a cookie is issued anyway; MQTT anonymous.
- medium: `POST security/login {"Operator","Password"}` → session
  cookie; every other query without it answers **401**; MQTT with the
  same username and password.
- high: the same over HTTPS 4000 and MQTT/TLS 8883, certificates.
- The cookie expires after about **one hour without a REST query** —
  MQTT traffic does not keep it alive (§6.1). `security/ping` extends it
  (§6.3). Not yet called by the connector (open item).

## Data model (§2–§3)

- **UID**: slash path rooted at the system root, e.g.
  `cpiasd/antenna/motor/status/targetPlatAz`. The REST URL takes the UID
  **without** the root (`consumer/cvtItem/antenna/motor/status/targetPlatAz`);
  the MQTT topic is the UID **with** it (§7). The root comes from the
  boundaries table (`UID` without a slash).
- **SVT** (`consumer/svtList[/subtree]`, valueNodeTbl): metadata of every
  value. `NodeType 2` = node. `UID` ending `#` = array, CVT rows replace
  `#` by an index (§2.2 note 2). `volatility -1` = never written to the
  CVT, MQTT only (§2 note 1). `AccessType` / `AccessLevel` have no
  documented meaning.
- **CVT** (`consumer/cvtList[/subtree]`, `consumer/cvtItem/<path>`):
  `Value` is text; a missing item answers an empty `parameters` array.
- `consumer/enumerations`, `consumer/dependencies` (`isHidden` 1 =
  meaningless in this configuration), `consumer/boundaries`,
  `consumer/alarmPointsList`, `consumer/arrayLabels`.
- Placeholders instead of null: `@NOMIN @NOMAX @NODEFAULT @NOUNITS
  @NODEP @INVALID`.
- Every answer is the envelope `{customerId, system, query, timeMs,
  parameters}` (§4.3). Columns arrive as string, number or null for the
  same name — the codec keeps text (`codec.Text`).

## Writes and actions

- SET (§5.39): `POST consumer/postMessage`
  `{"MCII":1,"NumArgs":2,"Message":"<UID>,<value>"}`, optional `RPID`
  (default 4 = SVTS Manager). No acknowledge: the connector reads the
  CVT back.
- Command verb (§9): same endpoint, `{"MCII":2,"NumArgs":1,"Message":"VERB arg…"}`.
  "No defined acknowledge or non-acknowledge", "no validation of the
  arguments". The verb list is "preliminary": ABANDON, ACQUIRE,
  CLEAR-SMART, DO-MPAMP-CAL, GOTO-LONGITUDE, INJECT-LOG, INCLIN-AQSN,
  MARK-POS-LOG, PAUSE-NOW, PEAK-NOW, POINT-AT-GLOBAL-SAT,
  RECOMPUTE-GLOBAL-SAT, RECOMPUTE-SAT-FAVORITE-LIST, RETUNE-GLOBAL-SAT,
  SAT-RELOAD, STANDBY, SPIRAL-SCAN, STOW, SYNC, RESUME-TRACK.
  **Open question:** MCII and NumArgs for a verb with arguments are not
  defined; the connector sends MCII 2, NumArgs 1, arguments inside
  Message (the §9 wording).
- `consumer/putData/<file>` uploads JSON data (ephemeris, §11, 4 000 000
  bytes max). Not exposed yet.

## Folder layout

```
codec/      stdlib only: envelope, table rows, postMessage bodies, UID rules, MQTT 3.1.1 packets
consumer/   package kratos: Plugin (REST + MQTT session), registry name "kratos"
docs/       consumer.md
assets/     the vendor PDF (not in git)
```

## Status against ADR-0025

| Deliverable | State |
|---|---|
| codec + unit tests from the spec | done (examples of the document, MQTT 3.1.1 tables) |
| consumer + CLI | done: info, walk, get, set, watch, export, command, table |
| producer | parked by the operator |
| integration on a real device | **missing — no NGC2 in the lab**; tested against an in-process fake only |
| replay fixtures | missing (need a real capture) |
| Wireshark dissector | not started (no dissector work until the connector is in use) |

## What NOT to do

- Never send the UID root in a REST path, never drop it from an MQTT topic.
- Never treat a `postMessage` 200 as "applied" — read it back.
- Never put the operator password on a command line: `DHS_KRATOS_USER` / `DHS_KRATOS_PASS`.
- Never commit the vendor PDF.
