# NMOS — per-spec architecture diagrams

ASCII role topology + transport + message direction for every spec dhs
plans to support. Each diagram shows the **consumer** side (Controller)
and the **provider** side (Node, sometimes Registry) — and which spec
introduces a third box that has no consumer/provider mirror.

Common legend:
```
─────>   request / message direction
═════>   long-lived stream (WS, MQTT)
···>     periodic heartbeat / poll
[N]      Node side          (provider in dhs)
[R]      Registry side      (provider — separate process)
[C]      Controller side    (consumer in dhs)
```

---

## IS-04 — Discovery & Registration

Three-API system. The Registry is a **dual-face hybrid**: its left face
**consumes** device registrations + heartbeats, its right face
**provides** the catalogue to Controllers. Same process, same in-memory
store, two faces.

```
                     ┌──────────────────────────────────┐
                     │            DNS-SD                │
                     │   _nmos-register._tcp            │
                     │   _nmos-query._tcp               │
                     │   _nmos-node._tcp  (P2P only)    │
                     └──────────────────────────────────┘
                              ▲             ▲
                  advertise   │             │   browse
                              │             │

  ┌──────────────────────┐                                   ┌──────────────────────────┐
  │      [N] Node        │                                   │     [C] Controller        │
  │     (the device)     │ ◄──── direct REST to Node API ─── │      (operator UI)        │
  │                      │       for walk + IS-05 PATCH      │                           │
  │  Node API SERVER:    │       + IS-07 WS + IS-12 WS       │  Query API CLIENT:        │
  │   GET /self          │       (after Registry gave URL)   │   GET /nodes              │
  │   GET /devices       │                                   │   GET /senders /receivers │
  │   GET /sources       │                                   │   POST /subscriptions     │
  │   GET /flows         │                                   │       ──> ws_href         │
  │   GET /senders       │                                   │                           │
  │   GET /receivers     │                                   │                           │
  │                      │                                   │                           │
  │  Registration        │                                   │                           │
  │  CLIENT:             │                                   │                           │
  │   POST /resource     │                                   │                           │
  │   POST /health/...   │                                   │                           │
  └──────────┬───────────┘                                   └─────────────┬─────────────┘
             │                                                             │
   devices   │  POST /resource                                  controller │  GET + WS subscribe
   register  │  POST /health (heartbeat ···· 5 s)                queries   │
   UPSTREAM  │  DELETE /resource/{type}/{id}                    DOWNSTREAM │
             ▼                                                             ▼
   ┌──────────────────────────────────────────────────────────────────────────────┐
   │                         [R] REGISTRY (middleware)                            │
   │                                                                              │
   │  ┌──── ( CONSUMER face ) ───────────┐  ┌──── ( PROVIDER face ) ────────────┐ │
   │  │  Registration API server         │  │  Query API server                 │ │
   │  │   POST   /resource               │  │   GET /nodes /devices /sources    │ │
   │  │   POST   /health/nodes/{id}      │  │   GET /flows /senders /receivers  │ │
   │  │   DELETE /resource/{type}/{id}   │  │   RQL filters (?label= ...)       │ │
   │  │                                  │  │                                   │ │
   │  │  Heartbeat watchdog goroutine    │  │  Subscription API                 │ │
   │  │   (5 s default ttl,              │  │   POST /subscriptions ──> ws_href │ │
   │  │    12 s timeout, GC on lapse)    │  │   WS notify created/updated/      │ │
   │  │                                  │  │           deleted/sync stream     │ │
   │  │     INGESTS to ──>               │  │                                   │ │
   │  │     Resource catalogue ◄───      │  │   Reads from same in-memory store │ │
   │  └──────────────────────────────────┘  └───────────────────────────────────┘ │
   │                                                                              │
   │  Same process. Same in-memory store. Two faces: consumer + provider.         │
   └──────────────────────────────────────────────────────────────────────────────┘
```

Failure modes Controller must handle:
- Registry vanishes → DNS-SD re-browse, switch to next-priority Registry.
- Node misses heartbeat → resource silently disappears from Query API
  (subscription emits `deleted` notification).
- WS subscription drops → reconnect + replay recent state.

---

## IS-09 — System Parameters

One REST surface. Node bootstraps from System API before registering.

```
   ┌──────────────────┐                  ┌──────────────────────┐
   │   [N] Node       │  GET /global     │  System API server   │
   │   (boot stage)   │ ───────────────> │  (typically inside   │
   │                  │  JSON config:    │   the Registry)       │
   │                  │  - NTP servers   │                      │
   │                  │  - syslog target │                      │
   │                  │  - registry hint │                      │
   │                  │  - PTP domain    │                      │
   └──────────────────┘                  └──────────────────────┘
```

Discovery: `_nmos-system._tcp` mDNS-SD. No WebSocket. No PATCH —
config is read-only to the Node.

---

## IS-05 — Connection Management

Stage-then-activate. Wire is symmetric, but the Node validates
`transport_params` against real hardware caps while the Controller only
validates against scraped IS-04 caps.

```
   ┌─────────────────────┐                    ┌────────────────────────┐
   │   [C] Controller    │                    │   [N] Node (Sender)    │
   │                     │  GET /constraints  │                        │
   │                     │ ─────────────────> │                        │
   │                     │ <─────────────────  │  caps JSON             │
   │                     │                    │                        │
   │  PATCH /senders/{id}│  PATCH /staged     │                        │
   │      /staged        │ ─────────────────> │  validate vs caps      │
   │  body: {            │                    │  store in staged slot  │
   │   master_enable,    │ <─────────────────  │  echo full staged     │
   │   transport_params, │                    │                        │
   │   activation: {     │                    │                        │
   │     mode,           │  ── activation ──> │  copy staged → active  │
   │     time            │                    │  open RTP socket       │
   │   }                 │                    │                        │
   │  }                  │  GET /active       │                        │
   │                     │ ─────────────────> │                        │
   │                     │ <─────────────────  │  active state         │
   │                     │                    │                        │
   │                     │  GET /transportfile│                        │
   │                     │ ─────────────────> │                        │
   │                     │ <─────────────────  │  text/plain SDP       │
   └─────────────────────┘                    └────────────────────────┘

   Receivers mirror the same shape under /single/receivers/{id}/...
   plus a transport_file body field on PATCH /staged for SDP ingest.

   Atomic multi-resource: POST /bulk/senders, POST /bulk/receivers
```

Activation modes (in `staged.activation.mode`):
- `activate_immediate` — activate within the response.
- `activate_scheduled_relative` — at `now + activation.requested_time`.
- `activate_scheduled_absolute` — at TAI absolute time.

---

## IS-07 — Event & Tally

Two transports (WS + MQTT) carrying the same JSON envelopes. Producer
attaches to in-process state; Consumer is pure subscriber.

```
   ┌──────────────────────┐                   ┌────────────────────────┐
   │  [N] Node (Source)   │                   │   [C] Controller       │
   │                      │                   │     (or another Node   │
   │  Events API (REST):  │  GET /sources/{id}│      acting as receiver│
   │   /sources/{id}/type │ <────────────────  │                        │
   │   /sources/{id}/state│                   │                        │
   │                      │                   │                        │
   │  WS sender:          │                   │                        │
   │   ws://.../events    │═════════════════>  │  WS subscriber         │
   │   message_type ∈     │   {state, health, │  command_subscription  │
   │   {state, health,    │    reboot,        │  command_health        │
   │    reboot, shutdown} │    shutdown}      │                        │
   └──────────────────────┘                   └────────────────────────┘

                                  - or -

   ┌──────────────────────┐    publish        ┌────────────────────────┐
   │  [N] Node (Source)   │ ────────────────> │   MQTT broker           │
   │                      │                   └──────────────┬──────────┘
   └──────────────────────┘                                  │ subscribe
                                                              ▼
                                                   ┌────────────────────┐
                                                   │  [C] Controller     │
                                                   └────────────────────┘
```

Event types (data formats): `boolean`, `number`, `string`, `enum`.
Source `format = urn:x-nmos:format:data`.
Sender `transport ∈ urn:x-nmos:transport:websocket | urn:x-nmos:transport:mqtt`.

The dhs slice ships **WebSocket only first**; MQTT layered on top once
WS is wire-validated.

---

## IS-08 — Audio Channel Mapping

REST-only, stage/activate pattern like IS-05.

```
   ┌─────────────────────┐                 ┌────────────────────────────┐
   │   [C] Controller    │  GET /io        │  [N] Node (Device with     │
   │                     │ ─────────────>  │   channel-mapping API)     │
   │                     │ <─────────────  │  inputs/outputs caps        │
   │                     │                 │                            │
   │  POST /map/         │ ─────────────>  │  validate channel routing  │
   │      activations    │                 │  (output_id, channel_index │
   │   { activation_id,  │                 │   <- input_id, channel)    │
   │     map: {          │                 │                            │
   │       outputs: [...]│ <─────────────  │  201 Created + active map  │
   │     },              │                 │                            │
   │     activation: {   │                 │                            │
   │       mode, time    │                 │                            │
   │     }               │                 │                            │
   │   }                 │                 │                            │
   │  GET /map/active    │ ─────────────>  │                            │
   └─────────────────────┘                 └────────────────────────────┘
```

Surfaced via IS-04 Device `controls` URN `urn:x-nmos:control:cm/v1.0`.

---

## IS-12 + MS-05-01 + MS-05-02 — Control Protocol

Single WebSocket per Device carrying a model defined by MS-05-02. The
"Ember+ shape, NMOS plumbing" spec.

```
   ┌────────────────────────┐                ┌─────────────────────────┐
   │   [C] Controller        │                │   [N] Device            │
   │                         │  WS connect    │                         │
   │   ws://.../ncp          │ ─────────────> │   IS-12 server          │
   │                         │ <════════════> │                         │
   │   marshalls NcObject    │   JSON envelope│   hosts MS-05-02 model: │
   │   datatypes             │    messageType │                         │
   │   tracks subscriptions  │   ∈ {Command,  │   OID 1 = root NcBlock  │
   │                         │      Cmd Resp, │   ├─ DeviceManager      │
   │                         │      Subscr,   │   ├─ ClassManager       │
   │                         │      Subscr R, │   ├─ SubscriptionMgr    │
   │                         │      Notif,    │   └─ application blocks │
   │                         │      Error}    │                         │
   │                         │                │   per-class method      │
   │                         │                │   dispatch (Get/Set/    │
   │                         │                │    Invoke)              │
   └────────────────────────┘                └─────────────────────────┘
```

MS-05-01: architecture document only (no wire).
MS-05-02: class library — NcObject root, NcBlock containers, NcWorker,
NcManager family (DeviceManager, ClassManager, SubscriptionManager),
NcTouchpoint linking model OIDs back to IS-04 UUIDs.

Surfaced via IS-04 Device `controls` URN `urn:x-nmos:control:ncp/v1.0`.

---

## BCP-008-01 / BCP-008-02 — Status Monitoring

Feature sets layered onto IS-12 / MS-05-02. Server-implemented,
client-consumed. No separate transport.

```
   ┌────────────────────────┐                ┌─────────────────────────┐
   │   [C] Controller        │                │   [N] Device            │
   │                         │   IS-12 WS     │                         │
   │   subscribes to:        │ <════════════> │   NcReceiverMonitor     │
   │   - linkStatus           │                │     (BCP-008-01)        │
   │   - connectionStatus    │                │                         │
   │   - externalSync...     │                │   NcSenderMonitor       │
   │   - streamStatus        │                │     (BCP-008-02)        │
   │   - transmissionStatus  │                │                         │
   │   - essenceStatus       │                │   touchpoint -> IS-04   │
   │                         │                │     Sender/Receiver UUID│
   │   renders fault graph   │                │                         │
   └────────────────────────┘                └─────────────────────────┘
```

---

## IS-13 — Annotation (WIP)

REST PATCH sibling to IS-04 Node API.

```
   ┌─────────────────────┐                  ┌────────────────────────┐
   │   [C] Controller    │  PATCH /node/    │   [N] Node              │
   │                     │      label       │                        │
   │                     │ ─────────────>   │   persist annotation   │
   │   user edits a label│      description │   reflect into IS-04    │
   │                     │      tags        │   resource update      │
   │                     │ <─────────────   │   (POST /resource on   │
   │                     │   200 OK         │    Registry)           │
   └─────────────────────┘                  └────────────────────────┘

   Same shape under /sender/{id}, /receiver/{id}, /source/{id},
   /flow/{id}, /device/{id}.
```

Surfaced via IS-04 Node `controls` URN `urn:x-nmos:control:annotation/v1.0`.

---

## BCP-002 / BCP-004 / BCP-006 / BCP-007 — Pure JSON-shape rules

No wire of their own. They layer into the IS-04 / IS-05 encoders +
decoders as JSON-Schema validators.

```
   ┌──────────────────┐         ┌──────────────────┐
   │  IS-04 encoder   │ ──────> │  BCP-004-01      │  Receiver caps shape
   │  IS-04 decoder   │ ──────> │  BCP-004-02      │  Sender caps shape
   │                  │ ──────> │  BCP-002-01      │  Natural Grouping tags
   │                  │ ──────> │  BCP-002-02      │  Asset DI tags
   │                  │ ──────> │  BCP-006-01      │  JPEG XS profile
   │                  │ ──────> │  BCP-006-04      │  MPEG-TS profile
   └──────────────────┘
```

Implemented as schema validators inside the IS-04 / IS-05 codec
packages, not as separate plugin slots.

---

## Deployment modes — mDNS not always available

Real-world deployments break the "everyone on one multicast LAN"
assumption. Production end-user networks block mDNS for security
policy; some matrix vendors (Lawo VSM) ship with no Registry support
at all. dhs supports three modes from day one:

### Mode A — full mDNS + Registry  (default)

Greenfield / lab / spec-compliant peers.

```
   ┌────────┐  mDNS  ┌──────────┐  mDNS  ┌────────────┐
   │ [N]    │ <───── │ DNS-SD   │ ─────> │ [C]        │
   │ Node   │        │ multicast│        │ Controller │
   └───┬────┘        └────┬─────┘        └─────┬──────┘
       │                  │                    │
       │  POST /resource  │                    │  GET + WS subscribe
       ▼                  ▼                    ▼
                ┌─────────────────────┐
                │  [R] Registry       │
                │  (consumer face +   │
                │   provider face)    │
                └─────────────────────┘
```

### Mode B — unicast Registry  (`--no-mdns --registry <ip>:<port>`)

Hardened deployments. mDNS firewalled but a Registry still exists.

```
   ┌────────┐                              ┌────────────┐
   │ [N]    │                              │ [C]        │
   │ Node   │                              │ Controller │
   └───┬────┘                              └─────┬──────┘
       │  POST /resource                         │  GET + WS subscribe
       │  (host from --registry FLAG)            │  (host from --registry FLAG)
       ▼                                         ▼
                ┌─────────────────────┐
                │  [R] Registry       │
                │  --advertise-host   │
                │     <ip>:<port>     │
                └─────────────────────┘
```

### Mode C — direct-Node, no Registry  (`--no-mdns --no-registry --peer-list FILE`)

Lawo VSM use-case (no Registration API support — see
[`matrix-compliance.md`](matrix-compliance.md)). End-user networks
where mDNS AND Registry are blocked.

```
   ┌────────┐                              ┌──────────────────────┐
   │ [N]    │                              │ [C]      Controller   │
   │ Node   │  ◄── direct REST per Node ── │                       │
   │        │      (host list comes from   │  --peer-list peers.csv│
   │        │       --peer-list FILE)      │                       │
   └────────┘                              │  reads:                │
   ┌────────┐                              │   nodeA.lan,2080      │
   │ [N]    │  ◄────────────────────────── │   nodeB.lan,2080      │
   │ Node   │                              │   192.0.2.5,8000      │
   └────────┘                              └──────────────────────┘
   ┌────────┐
   │ [N]    │  ◄──────────────────────────
   │ Node   │
   └────────┘
```

In Mode C the Controller fans out per-Node walks, IS-05 PATCHes, and
IS-07 WebSocket subscriptions. There is no Registry, no Query API,
no WS subscription stream — every Node is targeted directly.

---

## Proxy gateway topology (eventual goal)

A fully wired dhs can sit between any controller and any device:

```
        ┌────────────┐         ┌─────────────┐         ┌────────────┐
        │ Foreign    │ NMOS    │     dhs     │ NMOS    │ Foreign    │
        │ Controller │ ──────> │  (proxy)    │ ──────> │ Node       │
        └────────────┘  IS-04  │             │  IS-04  └────────────┘
                        IS-05  │  Node side  │  IS-05
                        IS-07  │  +          │  IS-07
                        IS-12  │  Controller │  IS-12
                               │  side       │
                               └─────────────┘
```

This is the same pattern as the existing dhs proxy story for ACP1 →
Ember+ etc., just with NMOS terminology on both ends.
