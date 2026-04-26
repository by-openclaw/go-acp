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

NMOS uses a **3-role topology** with a **dual-face Registry** in the
middle. The Registry does not fit the existing dhs consumer/provider
split because it is BOTH at once — depending on which face you look at:

```
devices  ──register/heartbeat──>  ( consumer )  REGISTRY  ( provider )  ──query+WS──>  controller
   │                                  Registration API     Query API                       │
   └── Node API (each device)                                                              │
                                                       ╰── controller commands devices ────╯
                                                                  via IS-05 / IS-07 / IS-08 / IS-12
                                                                  directly on each Node API
```

| NMOS role | dhs vocabulary | What it does |
|---|---|---|
| **Node** (device) | provider (Node API) + outbound client to Registry | Exposes resource graph (Node → Device → Source → Flow → Sender / Receiver), serves the Node API to anyone, POSTs registrations + heartbeats to a Registry. |
| **Registry** (middleware) | **consumer of device registrations** + **provider of catalogue** | Left face: receives `POST /resource` and `POST /health/...` from Nodes — it consumes their data. Right face: serves Query API + WebSocket subscriptions to Controllers — it provides the catalogue. Same process, two faces. |
| **Controller** (operator UI) | consumer (Query API + Node APIs) | Discovers Registries via DNS-SD, walks the Query API, then commands Nodes directly via IS-05/07/08/12. |

A fully compliant implementation MUST implement ALL THREE sides of
every NMOS spec it claims. NMOS-internal bridging (NMOS controller →
dhs → NMOS Node, e.g. across network segments) follows from
implementing all three roles correctly; it is not a separate feature.

> **Out of scope for this plugin:** anything that translates between
> NMOS and another dhs protocol. Cross-protocol architecture lives
> elsewhere; the NMOS plugin only ever speaks NMOS.

CLI surface mapping (planned):

```
dhs producer nmos serve                       # Node side (the device — default)
dhs registry nmos serve                       # Registry side (dual-face middleware)
                                              #   - serves Registration API (consumes from Nodes)
                                              #   - serves Query API + WS subs (provides to Controllers)
dhs consumer nmos walk     <reg-host>         # Controller — walk a Registry
dhs consumer nmos watch    <reg-host>         # Controller — subscribe via Query WS
dhs consumer nmos connect  <node-host> ...    # Controller — drive IS-05 on a Node
```

The new `dhs registry` top-level verb is needed because the Registry's
dual-face nature does not fit `producer` (which historically means
"serves a canonical tree of my own state") nor `consumer` (which
historically means "talks outbound to a remote device").

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

1. **mDNS-SD is preferred but NOT always available.** Many end-user
   networks block multicast DNS for security policy reasons. Plan for
   three deployment modes from day one:
   - **Full mDNS + Registry** (greenfield / spec-compliant peers).
   - **Unicast Registry** (`--no-mdns --registry <ip>:<port>`).
   - **Direct-Node** (`--no-mdns --no-registry --peer-list peers.csv`).
   Default to mDNS but never assume it works. See
   [`docs/matrix-compliance.md`](docs/matrix-compliance.md).
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
11. **Real matrix vendors are partially compliant.** Lawo VSM (verified
    2026-04-26 from docs) supports Node API + IS-05 over HTTP only —
    no Registration API, no Query API, no WebSocket, no MQTT, no IS-07,
    no IS-12, no mDNS. Implement the full spec, then fire compliance
    events on each peer-side gap. Track per-vendor in
    [`docs/matrix-compliance.md`](docs/matrix-compliance.md). Mirror of
    the Probel salvo deviation pattern (top-level `CLAUDE.md`
    "Spec-strict, no-workaround posture" → exception clause).
12. **No scheduled activations against Lawo.** Lawo VSM rejects
    `activate_scheduled_relative` / `_absolute` and silently coerces
    to immediate. Detect, fire `nmos_scheduled_activation_unsupported`,
    retry as `activate_immediate`.
13. **One Registry per Node, one Query API per Controller.** IS-04
    v1.3.3 mandates single-target selection: *"The Node selects a
    Registration API to use based on the priority"*. HA is
    client-driven failover via `pri` ranking + 5xx fallback, NOT
    multi-Registry replication. dhs supports active/passive priority
    pair + ST 2022-7 dual-network out of the box; active/active
    shared-store is out of scope for v1. See
    [`docs/ha.md`](docs/ha.md). Heartbeat 5 s, GC 12 s — failover
    must complete inside GC window.
14. **`pri` 0–99 are production; 100+ are dev.** Spec carves the
    range so dev Registries can't accidentally consume a live
    deployment. Default Registry `--priority 0`; CI / lab profiles
    bump to 100+. Fire `nmos_registry_dev_pri` if a `pri >= 100`
    appears in a production-mode session.

---

## What NOT to do

- Never make DNS-SD mandatory — many production networks block it.
  Always offer `--no-mdns` + `--peer-list` / `--registry` fallback.
- Never assume a Registry exists — implement direct-Node fallback for
  the Node + Controller sides from day one (Lawo VSM has no Registry).
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

## Strict-dependency architecture

NMOS code lives in **four layers** with **enforced one-way dependency
flow**:

```
LAYER 4  cmd/dhs/cmd_nmos.go                    (CLI)
LAYER 3  internal/amwa/{consumer,provider,registry}  (PLUGIN)
LAYER 2  internal/amwa/session/*                (SESSION)
LAYER 1  internal/amwa/codec/*                  (CODEC — stdlib only)
```

A package in layer N may import layer < N only. Cross-plugin imports
between `consumer/`, `provider/`, `registry/` are forbidden. Codec
packages must remain stdlib-only (lift-to-own-repo ready — same rule
as every other dhs protocol). Cross-protocol imports
(`internal/<other-proto>/*`) are forbidden outside neutral
infrastructure.

A new Tier-1 plugin slot `internal/registry/` lands with NMOS to host
the Registry's dual-face middleware role.

Enforcement: depguard golangci-lint rule + `go list -deps` audit test
+ PR review checklist. Full rules + inter-codec dependency graph + CI
config in [`docs/dependencies.md`](docs/dependencies.md).

## Conformance gate — AMWA NMOS Testing tool

Every NMOS PR that ships an implementation chunk MUST pass the matching
**AMWA NMOS Testing** suite (<https://github.com/AMWA-TV/nmos-testing>).
The tool runs as Docker, acts as Mock Registry / Mock Node /
probe-client, and exercises every claimed IS-* / BCP-* / MS-* suite.

Per-phase suite mapping in
[`docs/sequenced-tasks.md`](docs/sequenced-tasks.md). Pass / Fail /
Could-Not-Test gating + scope-outs documented in
[`docs/conformance.md`](docs/conformance.md).

This is the canonical NMOS gate. Vendor-specific integration tests
(Lawo VSM, nmos-cpp, etc.) layer on top — passing AMWA is necessary,
not sufficient.

## Implementation order

See [`docs/sequenced-tasks.md`](docs/sequenced-tasks.md). Minimum viable
slice for "9th protocol plugin works": **IS-09 → IS-04 → IS-05 → IS-07
(WebSocket only) + BCP-002 + BCP-004 conformance.**

---

## Architecture diagrams

See [`docs/architecture.md`](docs/architecture.md) — one ASCII diagram
per spec showing role topology, transports, and message direction.
