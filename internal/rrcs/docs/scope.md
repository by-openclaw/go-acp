# RRCS connector — scope of work (consumer first)

Agreed with the codeowner on 2026-10-06. Implementation follows ADR-0014
(issue → branch → tests → PR → CI → codeowner), ADR-0025 (six
deliverables) and ADR-0027 (no PR while a deliverable is missing).
Tracker: #1399.

## 0. Reality the scope is built on

- **RRCS** (Riedel Router Control Software) is the third-party gateway to an
  Artist intercom system: a program on a Windows PC, connected to one node
  of the Artist ring, offering a network socket with a published protocol.
  The connector talks to that program, never to a frame.
- Source in hand: "RRCS Interface Specification" **7.40 Rev 2**, 27/03/2019,
  240 pages. The codeowner reports that RRCS 9 exists and no longer needs
  the USB dongle (error 23 in 7.40). What changed on the wire between 7.40
  and 9 is unknown until a real one answers.
- **No real RRCS has been reached yet.** An installation is expected on
  2026-10-08. Everything below that is not marked *verified* is the
  specification's word.
- The specification contains printing errors (a doubled tag in §6.2, a
  stray space in a member name in §6.3, a history full of case
  corrections). Where the document and a real RRCS disagree, the root
  "spec-strict" posture applies: absorb, name the deviation, never guess an
  encoding.

## 1. What is built

Package `internal/rrcs/` (ADR-0001), registered as `rrcs`.

| Part | Content |
|---|---|
| `codec/` | XML-RPC call / response / fault in both directions, the value types, the four helper types, the transaction key, the error codes. Standard library only (ADR-0006). |
| `consumer/` | The session that sends requests to RRCS, and the HTTP listener that receives its notifications and answers its keep-alive. |
| `provider/` | Not decided. Consumer first; a stand-in RRCS is added only if it proves cheap. |

One gateway. The working/standby pair (§10) is out of scope for now; the
session still reports the gateway state and refuses cleanly on error 13.

## 2. Addressing

Two identifiers, both from the protocol.

| Identifier | Meaning | Use |
|---|---|---|
| net · node · port | The physical place. Port = (slot − 1) × 8 + position − 1 (§6.5) | Ports, keys, GPIO, crosspoints |
| object ID (32-bit) | A configured object | Conferences, groups, IFBs, logic sources; also carried by ports |

Paths:

```
gateway
net.N.node.N
net.N.node.N.card.S
net.N.node.N.port.P
net.N.node.N.port.P.key.E.G.K      expansion · page · key
net.N.node.N.port.P.gpi.N   .gpo.N
conference.ID   group.ID   ifb.ID   logic.ID
xp.SRC.DST
```

A property is one more element (`net.1.node.2.port.12.alias`). Names —
alias, label, long name — are properties and never part of a path.

"Panel" is not an object type of the protocol: a panel is a port whose port
type is a panel (§8.8 `GetAllPorts`).

## 3. Verbs

Each verb is described as text and approved before it is implemented
(AGENTS.md workflow rule). The read side comes first.

| Step | Verbs | Built on |
|---|---|---|
| 1 | `info` | `GetVersion`, `GetState`, `IsConnectedToArtist` |
| 2 | `list ports\|panels\|cards\|conferences\|groups\|ifbs\|logic\|gpi\|gpo\|xp`, `tree` | `GetAllPorts`, `GetAllIFBs`, `GetAllLogicSources_v2`, `GetObjectList`, `GetAllActiveXps` |
| 3 | `props PATH`, `get PATH` | `GetObjectPropertyNames`, `GetObjectProperty` |
| 4 | `watch` | `RegisterForAllEvents`, the listener, `GetAlive`, `IsRegisteredForAllEvents` |
| 5 | `set`, `ensure`, `matrix` | crosspoints, volume, gains, alias, label, GP outputs, logic sources |
| 6 | `alarm` | the §9.7 notifications against an alarm template |
| 7 | `config`, keys, panel spy, cloning, trunking, dialling | §8.10 – §8.17 |

All features of the specification are in scope in the end. Mapping panels
and keys onto routers is a later decision and not part of these steps.

## 4. Notifications are mandatory

RRCS sends notifications by calling an HTTP endpoint that the control
system exposes (§9). The consumer therefore:

- gives RRCS an address and a port that RRCS can reach;
- answers `GetAlive`, which RRCS sends after one second without traffic —
  an unanswered ping removes the registration without any error (§9.8);
- asks `IsRegisteredForAllEvents` periodically and registers again when the
  answer is false (§9.9.1);
- needs a second step per panel, `ChangePanelSpyRegistry`, to receive key
  events: pressed and released are both notified (§9.9.4).

## 5. Acceptance list

Frozen. A line is done only with its evidence.

| # | Item | Evidence |
|---|---|---|
| A1 | Codec round-trips every value type and helper type | Unit tests, bytes from the specification |
| A2 | Codec decodes what a real RRCS sends for every implemented method | Committed captures |
| A3 | `info` against a real RRCS | Captured run |
| A4 | Every `list` verb, `tree`, `props`, `get` against a real RRCS | Captured runs |
| A5 | `watch` keeps its registration for an hour and survives a restart of either side | Captured run with the keep-alive count |
| A6 | A crosspoint change made on the system arrives as a notification | Captured run |
| A7 | A key pressed and released on a panel arrives as two notifications | Captured run |
| A8 | Every implemented verb refuses cleanly: gateway standby, Artist not connected, port not online, object missing | Unit tests, and captured runs where the condition can be produced |
| A9 | Wireshark dissector names method, transaction key, addresses and error code for both directions | Capture opened in Wireshark |

## 6. To verify on the real RRCS

| # | Question |
|---|---|
| V1 | Are the method and member names exactly as printed, including case? |
| V2 | Does an object ID survive a configuration edit and a reload? |
| V3 | Does a configured but unplugged port stay in the lists? |
| V4 | Which properties does `GetObjectProperty` return per port type? |
| V5 | Can the commands on a key be read back, or only set? |
| V6 | How large and how slow are the list answers on a full system? |
| V7 | Does RRCS write a log of its own? The specification never mentions syslog |
| V8 | Which version is installed, and what differs from 7.40? |

## 7. Not in scope

- The working/standby pair and its switchover.
- RBIS (port 8194), named in §5.4 and described nowhere.
- Committing Riedel's specification to this public repository — the
  codeowner's decision; the connector's documents cite it by section.
