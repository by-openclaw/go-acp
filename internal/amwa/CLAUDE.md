# CLAUDE.md — AMWA NMOS

Atomic per-protocol context for the AMWA NMOS suite (9th dhs protocol plugin).
Read alongside the top-level [`CLAUDE.md`](../../CLAUDE.md) and the
[catalogue reference](reference.md).

> **NMOS is not a single protocol.** It is a suite of ~14 specifications
> spanning discovery, registration, connection management, event & tally,
> channel mapping, parameter caps, and device control. Each spec ships its
> own JSON Schema set, its own version track, its own role pair, and its
> own discovery rules.

---

## Roles

NMOS introduces a third role that does not fit the existing dhs
consumer/provider split:

| NMOS role | dhs slot | What it does |
|---|---|---|
| **Node** | provider | A device — exposes a resource graph (Node → Device → Source → Flow → Sender / Receiver), serves the Node API, posts heartbeats to a Registry. |
| **Registry** | provider (separate process) | Holds the resource catalogue, observes Node heartbeats, serves the Query API + WebSocket subscriptions. |
| **Controller** | consumer | Discovers Registries via DNS-SD, walks the Query API, commands Nodes via IS-05/08/12. |

A fully compliant implementation MUST implement BOTH (or all THREE) sides
of every spec it claims, AND should be able to act as a **proxy gateway**
(NMOS in / NMOS out — bridging any controller to any device through dhs).

CLI surface mapping (planned):

```
dhs producer nmos serve        # Node side (default)
dhs producer nmos serve --role registry
dhs consumer nmos walk <reg>   # Controller side
```

---

## Wire layer index

| Layer | Carrier | Specs |
|---|---|---|
| **Service discovery** | mDNS-SD (`_nmos-register._tcp`, `_nmos-query._tcp`, `_nmos-system._tcp`, `_nmos-node._tcp` for P2P) | IS-04 §3, IS-09 |
| **Bootstrap config** | HTTP/JSON, single `/global` resource | IS-09 |
| **Resource graph** | HTTP/JSON REST (`/x-nmos/<api>/<ver>/...`) + WebSocket subscription on Query API | IS-04 |
| **Connection control** | HTTP/JSON REST, stage-then-activate pattern, SDP payload for RTP | IS-05, IS-08 |
| **Event & tally** | WebSocket OR MQTT, JSON envelope (`message_type` ∈ {state, health, reboot, shutdown}) | IS-07 |
| **Device model + control** | WebSocket, JSON envelope (`messageType` ∈ {Command, CommandResponse, Subscription, SubscriptionResponse, Notification, Error}) | IS-12 (wire), MS-05-01 (architecture), MS-05-02 (class library) |
| **Annotation** | HTTP/JSON PATCH (sibling of Node API) | IS-13 (WIP) |
| **Profiles** | JSON-Schema rules layered onto IS-04 / IS-05 | BCP-002, BCP-004, BCP-006, BCP-007 |
| **Status feature sets** | MS-05-02 classes over IS-12 | BCP-008-01 (Receiver), BCP-008-02 (Sender) |

Common HTTP base: `http(s)://<host>:<port>/x-nmos/<api>/<version>/...`
Common content-type: `application/json` everywhere.

DNS-SD TXT records on every IS-* announcement:
```
api_proto = http | https
api_ver   = v1.0,v1.1,v1.2,...
api_auth  = true | false
pri       = <int>          # lower = higher priority
```

---

## Resource graph (the IS-04 universe)

```
Node (the device)
└── Device (a coherent control surface inside the Node)
    ├── Source (an essence origin)
    │   └── Flow (a specific encoding of a Source)
    │       └── Sender (a network egress carrying a Flow)
    └── Receiver (a network ingress consuming someone else's Sender)
```

UUIDs are the only stable identifier. Labels are mutable / human-friendly /
non-unique. Every other spec (IS-05, IS-07, IS-08, IS-12) addresses
resources by their IS-04 UUID.

---

## Versioning

Every IS-* and MS-* spec has multiple stable releases that coexist on the
wire:

| Spec | Tracks worth supporting |
|---|---|
| IS-04 | v1.3.x (current), v1.2.x (still common), v1.1.x (legacy) |
| IS-05 | v1.1.x (current), v1.0.x (still common) |
| IS-07 | v1.0.x |
| IS-08 | v1.0.x |
| IS-09 | v1.0.x |
| IS-12 | v1.0.x |
| MS-05-01 / MS-05-02 | v1.0.x |
| BCP-* | v1.0.0 each (BCP-006-02 / 006-03 / 007-01 still WIP) |

Convention: every supported version becomes a selectable parameter on the
plugin (mirroring the `proto:tsl` v3.1/v4.0/v5.0 pattern). Default to the
latest stable; never silently downgrade.

---

## Asymmetric specs (consumer ≠ provider)

Most NMOS specs do NOT round-trip the same code path on both sides. Plan
for divergent implementations:

| Spec | Why it's asymmetric |
|---|---|
| **IS-04** | Three surfaces: Node API (provider), Registration API + Query API (Registry), Query API client + WS subscription (Controller). Node POSTs heartbeats; Controller does WS subscription long-polls. Different code each side. |
| **IS-05** | Wire is symmetric (PATCH `/staged`), but Node validates `transport_params` against real hardware caps; Controller only validates against scraped IS-04 caps. |
| **IS-07** | Producer attaches to in-process state changes, optionally bridges to MQTT. Consumer is pure WS or MQTT client. Producer message-type set is a superset. |
| **IS-09** | Server publishes config (typically co-hosted with Registry). Client is Node bootstrap. Two different deploy targets, no shared code. |
| **IS-12 + MS-05-02** | Server hosts live device-model tree (block hierarchy, ClassManager, SubscriptionManager). Client only marshals/unmarshals datatypes. ClassManager + per-class method dispatch is server-only. |
| **BCP-008-01 / 008-02** | Same asymmetry as IS-12 — feature sets are server-implemented, client-consumed. |
| **IS-08** | Wire is symmetric (REST staged/active), but provider owns the mapping graph and constraint enforcement; consumer just diffs target mappings. |
| **IS-13** | PATCH wire is symmetric, but server must persist + reflect annotations into IS-04 updates. |

The clean mirrors (pure JSON-shape rules, no wire of their own) are:
**BCP-002-01 / BCP-002-02 / BCP-004-01 / BCP-004-02 / BCP-006-* / BCP-007-01**.
These layer into the IS-04 / IS-05 encoders — no separate plugin slots.

---

## Quirks worth remembering

1. **mDNS-SD is mandatory** for production deployments. P2P fallback
   (`_nmos-node._tcp`) only fires when no Registry is discoverable.
2. **Registries observe heartbeats.** A Node missing a heartbeat (default
   5 s interval, 12 s timeout) is removed from the Query API — the
   Controller observes the WS Subscription event for it. Implement
   client-side back-off and re-registration on `404` from POST `/health`.
3. **PATCH `/staged` does not activate.** Activation is a separate body
   field (`activation.mode`) inside the same PATCH OR a follow-up PATCH.
   Three modes: `activate_immediate`, `activate_scheduled_relative`,
   `activate_scheduled_absolute`.
4. **`master_enable` gates the Sender / Receiver.** Even with a fully
   staged target, nothing happens until `master_enable=true`.
5. **SDP transport.** RTP-based Senders carry their full SDP via
   `GET /single/senders/{id}/transportfile` (text/plain). Receivers
   accept SDP via `PATCH .../staged` body field `transport_file.data`.
6. **IS-04 `controls` array** is how IS-05, IS-07, IS-08, IS-12, IS-13
   are surfaced — each entry has a `type` URN and an `href`. Walk it
   to discover what a Device actually supports.
7. **MS-05-02 `OID 1` is always the Device root block.** The
   ClassManager and SubscriptionManager live as child OIDs of OID 1.
8. **`x-nmos` namespace is reserved** (URLs, URNs, JSON keys). Never
   put non-NMOS content under it.
9. **Auth is IS-10** (out of scope for v1). All endpoints currently
   support `api_auth=false`.
10. **Common-pitfall: confusing Node API with Registration API.** Node
    serves the Node API to anyone who asks. Node CLIENT-CALLS the
    Registration API on the Registry. Two different code paths, easy
    to mix up.

---

## What NOT to do

- Never bypass DNS-SD with hard-coded URLs in production code (CLI flag
  for testing is fine).
- Never assume a Registry exists — implement P2P fallback for the Node
  side from day one.
- Never PATCH IS-04 resources directly — they're read-only on the Node
  API. Annotations go through IS-13 (when stable); other resource
  updates re-register through the Registration API.
- Never invent endpoints under `/x-nmos/*` — every path is spec-defined.
- Never use the same OID across Devices in MS-05-02 — OIDs are
  Device-scoped, not Node-scoped.
- Never skip BCP-004 caps when registering a Receiver — a Controller
  with a Sender it can't filter against will refuse to connect.
- Never log or store raw transport_file SDP without scrubbing — it can
  contain operator-private network plans.

---

## Implementation order

See [`docs/sequenced-tasks.md`](docs/sequenced-tasks.md). Minimum viable
slice for "9th protocol plugin works": **IS-09 → IS-04 → IS-05 → IS-07
(WebSocket only) + BCP-002 + BCP-004 conformance.**

---

## Architecture diagrams

See [`docs/architecture.md`](docs/architecture.md) — one ASCII diagram
per spec showing role topology, transports, and message direction.
