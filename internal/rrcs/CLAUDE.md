# CLAUDE.md — RRCS (Riedel Router Control Software)

Atomic wire context for the RRCS connector. Read the root `CLAUDE.md` first;
this file holds only what is specific to RRCS. Scope and acceptance list:
[`docs/scope.md`](docs/scope.md). Tracker: #1399.

> **STATUS: started 2026-10-06. Nothing here is verified on a device.** Every
> fact below is the specification's word — "RRCS Interface Specification"
> 7.40 Rev 2, 27/03/2019 — cited by section. A real RRCS is expected on
> 2026-10-08; facts confirmed there are marked *verified* with the date.

## What it is

RRCS is the third-party gateway to a Riedel Artist intercom system: a
Windows program connected to one node of the Artist ring (§2). The
connector talks to RRCS. It never talks to an Artist frame.

## Transport (§5)

| Layer | Value |
|---|---|
| TCP | 8193 by default, provisionable (§5.4). RBIS, a second gateway, is on 8194 and is not described |
| HTTP | POST only. `User-Agent` and `Host` required. `Content-Type: text/xml`. `Content-Length` present and correct (§5.5) |
| Body | XML-RPC: one `<methodCall>` per request, one `<methodResponse>` per answer (§5.6) |
| Security | None described: no login, no TLS |

## XML-RPC as RRCS uses it (§5.6)

- Value tags: `<i4>` or `<int>`, `<boolean>` (0/1), `<string>`, `<double>`,
  `<dateTime.iso8601>`, `<base64>`, `<struct>`, `<array>`. A `<value>` with
  no type tag is a string.
- A response holds either `<params>` with one `<param>`, or a `<fault>`
  whose struct carries `faultCode` and `faultString` — never both.
- Names are case sensitive. The specification's own history records
  several corrections of case (`boolean` vs `Boolean`).

## Every request and answer

- The first parameter is the **transaction key**: one starting character
  followed by 10 digits. RRCS uses `R` for the requests it sends; the
  control system chooses any other character (§6.5).
- The answer echoes the transaction key and carries an **error code**.
  Two shapes occur: an array `[TransKey, ErrorCode, …]` (§11) and a struct
  with `TransKey` and `ErrorCode` members (§8.1 `GetAllActiveXps`, §8.7).

## Error codes (§7)

| Code | Meaning | Code | Meaning |
|---|---|---|---|
| 0 | Success | 14 | XML-RPC parameters wrong for this request |
| 1 | Transaction key invalid | 15 | Invalid conference or not found |
| 2 | Net address invalid | 16 | Invalid conference member or not found |
| 3 | Node address invalid | 17 | Invalid priority |
| 4 | Port address invalid | 18 | Invalid GPIO number |
| 5 | Slot no. invalid | 19 | Invalid gain value |
| 6 | Input gain invalid | 20 | Timeout |
| 7 | IP-address invalid | 21 | No permission |
| 8 | TCP-port invalid | 22 | Object does not exist |
| 9 | Label invalid | 23 | No USB-dongle available |
| 10 | Conference position invalid | 24 | Port is not online |
| 11 | Artist network not connected | 25 | Object property not supported |
| 12 | Route does not exist | 26 | Limit exceeded |
| 13 | Gateway is standby | 99 | Generic error |

## Addressing (§6.5)

| Element | Range | Note |
|---|---|---|
| Net | 1..255 | Usually 1 |
| Node | 2..255 | One Artist frame |
| Port | 0..255 | (slot − 1) × 8 + position on the card − 1 |
| Slot | 1..4 on Artist S, 1..16 on Artist M | |
| Priority | 0 below standard, 1 standard, 2 high, 3 paging, 4 emergency | |
| Gain | −36..36, in half decibels; −128 is mute (§8.5) | |
| Crosspoint volume | ≤ 0 mute; 1..255 is (value − 230) / 2 dB; > 255 is +12.5 dB | |
| Object ID | 32-bit | A configured object |

Helper types (§6.1 – §6.4): `TPortAddress` (IsInput, Node, Port; node and
port 0 is the null port), `TGroupPortAddress`, `TConferencePortAddress`
(with Talk and Listen rights), `TMemberChangeList`.

## Two directions

| Direction | Who connects | Content |
|---|---|---|
| Requests | We connect to RRCS | §8 |
| Notifications | RRCS connects to us, on the address, port and path given at registration | §9 |

The notification channel is a lease:

- `RegisterForAllEvents` starts it (§8.15.1).
- RRCS calls `GetAlive` on us whenever it sent nothing for one second. No
  answer and RRCS drops us, silently (§9.8).
- `IsRegisteredForAllEvents` tells whether the lease still holds; when it
  does not, both registration steps are done again (§9.9.1).
- Panel key events need `ChangePanelSpyRegistry` per panel on top (§8.12).
  `PanelSpyKeyEvent` carries `KeyAction` 0 pressed, 1 released; double-click
  and long-press are listed as not supported (§9.9.4).

## Method catalogue

Names as printed in the specification; exact case is confirmed on a device.

| Group | Methods | § |
|---|---|---|
| Crosspoints | `SetXp`, `SetXpPrio`, `SetXpDestructive`, `KillXp`, `GetXpStatus`, `GetAllActiveXps`, `GetActiveXpsRange` | 8.1 |
| Volume | `SetXpVolume`, `GetXpVolume` | 8.2 |
| Names | `SetPortAlias`, `GetPortAlias`, `SetPortLabel`, `GetPortLabel` | 8.3, 8.4 |
| Gains | `SetInputGain`, `GetInputGain`, `SetOutputGain`, `GetOutputGain` | 8.5 |
| GPIO | `SetGpOutput`, `GetGpInputState`, `GetGpOutputState` | 8.6 |
| Logic sources | `SetLogicSourceState`, `GetAllLogicSources`, `GetAllLogicSources_v2` | 8.7 |
| Status | `GetState`, `SetStateWorking`, `SetStateStandby`, `GetVersion`, `GetAlive`, `GetAllCaps`, `IsConnectedToArtist` | 8.8 |
| Lists | `GetAllPorts`, `GetAllIFBs`, `GetObjectList`, `GetObjectProperty`, `GetObjectPropertyNames`, `GetCommandList`, `GetErrorCodeList` | 8.8, 8.9 |
| Configuration | `ConfigurationChange`, `BufferConfigurationChange`, `ApplyConfigurationChange` | 8.10 |
| Keys | `SetKeyLabel`, `SetKeyMarker`, `SetKeyLabelAndMarker`, their `Clear…` forms, `GetAllRemoteKeys`, `PressKey` | 8.11 |
| Panel spy | `ChangePanelSpyRegistry` | 8.12 |
| Cloning | `ClonePort` | 8.13 |
| Registration | `RegisterForEvents`, `RegisterForEventsEx`, `RegisterForAllEvents`, the GP-input and GP-output forms, their opposites, `XpVolumeChangeRegistryAdd` / `Remove` / `Reset`, `IsRegisteredForEvents`, `IsRegisteredForAllEvents` | 8.15 |
| Trunking | `GetTrunkPorts`, `GetTrunklineSetup`, `GetTrunklineActivities`, `GetTrunkIfbs` | 8.16 |
| Dial | `DialNumber`, `HangUpCall` | 8.17 |

`GetObjectList` types (§8.9.1, case-insensitive): `conference`, `group`,
`port`, `ifb`, `logic-source`, `logic-destination`, `gp-input`, `gp-output`,
`user`, `audiopatch`, `client-card`.

Notifications (§9): `CrosspointChange`, `XpVolumeChange`, `GpInputChange`,
`GpOutputChange`, `LogicSourceChange`, `ConfigurationChange`, `SendString`,
`SendStringOff`, `GetAlive`, the panel-spy events, and the alarms
`UpstreamFailed`, `DownstreamFailed`, `ClientFailed`, `NodeControllerFailed`
with their `…Cleared` forms, `NodeControllerReboot`, `PortActive`,
`PortInactive`, `GatewayShutdown`, `GatewayState`.

## Timing the specification gives (§12)

| Kind | Typical, idle system |
|---|---|
| Internal to RRCS (state, version, registration) | 1 ms |
| Queries | 10 ms |
| Changes of state (crosspoint, volume, gain, GP output, logic source) | 50 ms |
| Changes of configuration (`SetPortAlias`, `ConfigurationChange`) | several seconds, longer if another PC edits the configuration |

## Printing errors in the specification

None of these is a wire deviation; they are errors of the document.

| # | Where | What | Handling |
|---|---|---|---|
| P1 | §6.2 `TGroupPortAddress` | An opening `<member>` tag is printed twice, so the block is not well-formed as printed | The type is the four members listed: `IsInputOnly`, `Node`, `Port`, `SecondChannel` |
| P2 | §6.3 `TConferencePortAddress` | The member name is printed `UseSecondChannel ` with a trailing space, once in the whole document | Member names are trimmed on decode |
| P3 | §11.1 | The text says source port 35 and destination port 69; the example sends ports 19 and 3 | The example is used for its XML shape only |
| P4 | §3 history | Several entries correct the case of names (`boolean`, `TrunkingPortAddr`, `XpVolumeChangeRegistryAdd`) | Names are case sensitive; a name is trusted only once a device has accepted it |

The two exchanges printed in §11 are standard XML-RPC, with `<params>`
and `<param>`. The codec emits and accepts that form only.

## What NOT to do

- Never assume `KillXp`, `SetGpOutput` or `SetLogicSourceState` reaches
  "off": RRCS undoes only what RRCS itself set (§8.1, §8.6, §8.7).
  `GetXpStatus` can stay true after a `KillXp`.
- Never treat the notification registration as permanent. It is a lease
  kept by answering `GetAlive`.
- Never use a name (alias, label, long name) as an identifier. Operators
  change them.
- Never renumber: a port address is positional. A missing card leaves a gap.
- Never send configuration changes in a tight loop. They take seconds and
  collide with other editors (§12.4).
- Never run it over an open network. The protocol has no authentication.
- Never import `dhs/*` from `internal/rrcs/codec/` (ADR-0006).

## Oracle (ADR-0034)

A real RRCS with an Artist node. No independent implementation of the
protocol is known. Until captures exist, unit tests take their bytes from
the specification text.
