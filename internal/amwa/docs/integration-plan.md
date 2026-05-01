# NMOS — integration test plan

Per-spec controller / provider implementation status, in the order
to drive integration tests against real Registry / Node / Controller
peers.

This is the canonical companion to
[`sequenced-tasks.md`](sequenced-tasks.md) (development order) and
[`conformance.md`](conformance.md) (AMWA NMOS Testing harness).

Status legend:

| Status | Meaning |
|---|---|
| **done** | Code on `main`, unit tests green, ready to drive against a peer. |
| **pending** | Identified follow-up work, tracking issue open. |
| **partial** | Some surfaces shipped, others still pending — see notes column. |
| **skipped** | Intentionally out of scope for v1; revisit later. |
| **n/a** | Role does not exist for this spec (e.g. internal-only API). |

## Provider vs Controller

NMOS uses a 3-role topology with a dual-face Registry. dhs maps to
the roles as follows (per
[`internal/amwa/CLAUDE.md`](../CLAUDE.md) "Roles"):

| NMOS role | dhs verb | Implements |
|---|---|---|
| **Node** | `dhs producer nmos serve` | Provider side of every Node-facing spec. |
| **Registry** | `dhs registry nmos serve` | Provider side of IS-04 Registration + Query, plus IS-09 System face when co-hosted. |
| **Controller** | `dhs consumer nmos <verb>` | Controller side — walks Query API, drives Node-side IS-05 / IS-07 / IS-08 / IS-12. |

The "Provider side" column below is whichever of Node / Registry
hosts the wire endpoint for that spec. The "Controller side" column
is the dhs Controller's behaviour against that endpoint.

## Per-spec test plan

| Seq | Spec | Provider side | Provider status | Controller side | Controller status | AMWA suite | Notes |
|---:|---|---|---|---|---|---|---|
| 1 | **IS-09** v1.0.0 System | `dhs registry nmos serve` — `/x-nmos/system/v1.0/global` | done | Node bootstrap reads `/global` on startup | done | IS-09-02 | PR #153 + retrofit #161. |
| 2 | **IS-04** Discovery (DNS-SD) | mDNS announce of `_nmos-{register,query,system,node}._tcp` | done | mDNS browse + unicast SRV/TXT fallback | done | covered by IS-04-0x | Codec at `internal/amwa/codec/dnssd/`. |
| 3 | **IS-04** v1.1/v1.2/v1.3 Node API | `dhs producer nmos serve` — `/x-nmos/node/<ver>/{self,devices,sources,flows,senders,receivers}` | done | `dhs consumer nmos walk` enumerates the resource graph | done | IS-04-01 | Multi-version codec PR #160; Controller PR #162. |
| 4 | **IS-04** Registration API | `dhs registry nmos serve` — Node POSTs `/resource` + `/health/...` | done | n/a (Node is the client; Controller never touches this surface) | n/a | IS-04-02 | PR #157. |
| 5 | **IS-04** Query API | `dhs registry nmos serve` — Query face | done | `dhs consumer nmos walk <reg-host>` | done | IS-04-02 | PR #157 + #162. |
| 6 | **IS-04** Query API WS subscriptions | Registry WS subscription endpoint | done | `dhs consumer nmos watch <reg-host>` (subscription grain consumer) | pending | IS-04-04 | Server done in #157; client `watch` verb pending. |
| 7 | **IS-04** Node P2P fallback | Node mDNS direct-advertise (Mode D) | done | Direct-Node CSV mode (Mode C) | done | IS-04-03 | EVS Cerebrum-style P2P verified. |
| 8 | **IS-05** v1.0.2 single endpoints | Node `/single/{senders,receivers}/{id}/{staged,active,transportfile}` | pending | `dhs consumer nmos connect` PATCHes `/staged` + activation | pending | IS-05-01 v1.0 | Codec landed (#174); plugin tracker #163. |
| 9 | **IS-05** v1.1.2 bulk endpoints | Node `/bulk/{senders,receivers}` | pending | Controller bulk PATCH | pending | IS-05-01 v1.1 | Codec landed (#174); plugin tracker #163. |
| 10 | **IS-05** Controller orchestration | n/a | n/a | full stage → activate flow over many Senders | pending | IS-05-03 | tracker #163. |
| 11 | **IS-08** v1.0.1 Audio Channel Mapping | Node `/x-nmos/channelmapping/v1.0/{io,active,staged,activations}` | pending | Controller POSTs `/map/activations` (immediate + scheduled) | pending | IS-08-01 | Codec landed (#178); plugin tracker #165 (reopened). |
| 12 | **IS-07** v1.0.1 Event & Tally over WebSocket | `dhs producer nmos events serve` — fanout state events to subscribed sources | done | `dhs consumer nmos events watch --sources <uuid,uuid>` | done | IS-07-01 | PR #176 codec, #177 WS layer. |
| 13 | **IS-07** v1.0.1 Event & Tally over MQTT | MQTT bridge over Publisher | skipped | MQTT subscriber | skipped | IS-07-01 | Parked — IS-07 §6 lists WS + MQTT as parallel options; WS covered, MQTT for follow-up. |
| 14 | **MS-05-01** Control Architecture | n/a (architecture document, no wire) | n/a | n/a | n/a | covered by IS-12-01 | PR #180 ships datatypes; informs IS-12. |
| 15 | **MS-05-02** v1.0.0 Control Framework | Node hosts NcObject tree + ClassManager + SubscriptionManager | partial | Controller marshals NcMethodResult / NcDescriptor responses | partial | covered by IS-12-01 | Codec landed (#180); class registry + subscription manager pending alongside IS-12 plugin. |
| 16 | **IS-12** v1.0.1 Control Protocol | Node WS server dispatching `Command` / emitting `Notification` | pending | Controller WS client sending `Command` + tracking `Subscription` | pending | IS-12-01 (invasive) | Codec landed (#179); WS server + client pending tracker #166. |
| 17 | **BCP-002-01** Natural Grouping | Node attaches `urn:x-nmos:tag:grouping/v1.0` on Source/Sender/Receiver/Flow | pending | Controller honours `grouphint` when offering routes | pending | (none — JSON-shape rule) | Validator landed (#181); attach/honour behaviour layers onto IS-04 plugin. |
| 18 | **BCP-002-02** Asset Distinguishing Information | Node attaches `urn:x-nmos:tag:asset/v1.0` on Sender/Receiver | pending | Controller surfaces asset tags in UI | pending | (none) | Validator landed (#181). |
| 19 | **BCP-004-01** Receiver Capabilities | Node advertises `caps.constraint_sets` on Receiver | pending | Controller filters Senders against Receiver caps | pending | (none) | Validator landed (#181). |
| 20 | **BCP-004-02** Sender Capabilities | Node advertises `caps.constraint_sets` on Sender | pending | Controller filters Receivers against Sender caps | pending | (none) | Validator landed (#181). |
| 21 | **BCP-006-01** NMOS With JPEG XS | Node sets `flow.media_type=video/jxsv` + IS-05 transport_params | pending | Controller honours JPEG XS constraints | pending | (none) | Validator landed (#181). |
| 22 | **BCP-006-04** NMOS Support for MPEG TS | Node sets `flow.media_type=video/MP2T` + mux format URN | pending | Controller honours MPEG TS constraints | pending | (none) | Validator landed (#181). |
| 23 | **BCP-008-01** Receiver Status Monitoring | Node exposes `NcReceiverMonitor` class via IS-12 | pending | Controller subscribes to monitor properties | pending | BCP-008-01 | Validator landed (#181); class registration depends on MS-05-02 plugin. |
| 24 | **BCP-008-02** Sender Status Monitoring | Node exposes `NcSenderMonitor` class via IS-12 | pending | Controller subscribes to monitor properties | pending | BCP-008-02 | Validator landed (#181); class registration depends on MS-05-02 plugin. |
| 25 | **AMWA NMOS Testing harness** | n/a | n/a | n/a | n/a | (meta) | PR #183 wires the harness; awaits user manual approval after first real run pins image digest + populates per-suite expected.json. |
| 26 | **Wireshark dissector** | n/a — passive observer | done | n/a — passive observer | done | (meta) | `internal/amwa/wireshark/dhs_nmos.lua` decodes mDNS, HTTP `/x-nmos/`, WebSocket text frames (IS-04 grain / IS-07 message_type / IS-12 messageType). PR #182. |

## Pending plugin work

To unlock end-to-end integration testing the following plugin chunks
need to land on `main`:

1. **IS-04 Controller `watch` verb** (seq 6) — WS subscription
   consumer layered on top of `dhs consumer nmos walk`. Small —
   reuses `internal/amwa/session/http` WebSocket client + the
   IS-04 Query grain envelope.
2. **IS-05 plugin** (seq 8 / 9 / 10, tracker #163) — Node-side
   single + bulk handlers, Controller stage/activate orchestration,
   `dhs consumer nmos connect` verb.
3. **IS-08 plugin** (seq 11, tracker #165 reopened) — Node-side
   `/map/...` handlers, Controller mapping diff tool.
4. **IS-12 + MS-05-02 plugin layer** (seq 15 / 16, tracker #166) —
   Node WS server, ClassManager, SubscriptionManager, Controller
   WS client.

Each ships behind the existing `feedback_per_spec_issue.md` rule —
one PR per sub-tracker, auto-merge on green CI per
`feedback_nmos_auto_merge.md`, manual approval owed only on PR #183
(integration-test gate).

## Driving the test sweep

Per-suite invocation:

```bash
tests/integration/nmos/scripts/run-suite.sh <suite-id>
```

See [`tests/integration/nmos/README.md`](../../../tests/integration/nmos/README.md)
for the bring-up + bump-pin recipe; see
[`conformance.md`](conformance.md) for the gating rules and image
digest pinning protocol.

The order in this table is the recommended sweep order — each row's
preconditions are everything in the rows above it. Rows marked
**done** can be exercised today; rows marked **pending** unblock as
the listed plugin chunks merge.
