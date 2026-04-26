# NMOS — sequenced implementation plan

Numbered task list to take dhs from "scaffold only" to "production-grade
NMOS Node + Registry + Controller". Each numbered chunk maps to a future
GitHub issue + PR.

The minimum viable NMOS plugin = items **#1 through #6**. Everything
after that is incremental coverage.

---

## Phase 0 — Scaffold (this branch, this PR)

Tracking: epic [#146](https://github.com/by-openclaw/go-acp/issues/146).

| Step | Deliverable |
|---|---|
| 0.1 | `internal/amwa/CLAUDE.md` — atomic per-protocol context. |
| 0.2 | `internal/amwa/docs/architecture.md` — ASCII per-spec diagrams. |
| 0.3 | `internal/amwa/docs/sequenced-tasks.md` — this file. |
| 0.4 | `internal/amwa/docs/matrix-compliance.md` — per-vendor compliance tracker (Lawo VSM verified). |
| 0.5 | README.md row for NMOS (planned). |
| 0.6 | `agents.md` — `nmos` added to `<proto>` set. |
| 0.7 | `internal/amwa/reference.md` — already on main (catalogue). |

Zero Go code. Zero CLI verbs. Pure design.

Acceptance: scaffold PR merges cleanly to `main`; epic #146 captures
the full scope.

---

## Phase 1 — Foundation: discovery + registration

### #1 — DNS-SD client + server + unicast fallback (both sides)

Pure infrastructure, no NMOS semantics yet. **Three deployment modes
must work from day one** because real-world peers block mDNS or omit
the Registry entirely (see
[`matrix-compliance.md`](matrix-compliance.md)).

- mDNS responder + browser using `github.com/hashicorp/mdns` OR
  hand-rolled stdlib (decision in PR).
- Service types: `_nmos-register._tcp`, `_nmos-query._tcp`,
  `_nmos-system._tcp`, `_nmos-node._tcp`.
- TXT record encoding/decoding (`api_proto`, `api_ver`, `api_auth`,
  `pri`).
- **Unicast DNS-SD** (RFC 6763 §10) — fall back to authoritative DNS
  SRV+TXT lookups when mDNS yields nothing.
- **CSV peer-list bootstrap** — `--peer-list peers.csv` reads
  `host,port[,api_ver]` lines for direct-Node mode.
- **CLI flags landed in this PR** (used by every later phase):
  - `--mdns` / `--no-mdns` (default: on).
  - `--registry <host>:<port>` (skip discovery, dial Registry directly).
  - `--peer-list FILE` (skip discovery + Registry, walk static Nodes).
  - `--advertise-host <ip>:<port>` (override the IP that gets put in
    the DNS-SD A/SRV record — needed when binding 0.0.0.0).
- Compliance events: `nmos_mdns_disabled`, `nmos_mdns_no_response`,
  `nmos_csv_peer_unreachable`.
- `dhs` library helper: `nmos/discovery/{Browse,Announce,Resolve}`.

Out of scope: IPv6-only networks (track in follow-up).

Estimated PR size: ~800 LOC + tests.

### #2 — IS-09 System API (server + client)

Smallest NMOS spec. Lets us validate the "REST + DNS-SD" plumbing
before tackling IS-04.

- Codec: `internal/amwa/codec/is09/` — JSON Schema for `global`
  resource.
- Provider: `dhs producer nmos serve --role system --config FILE`.
- Consumer: `dhs consumer nmos system <hint>` — fetch + dump.
- Tests: round-trip JSON, DNS-SD advertise + browse, fallback to
  config file when no `_nmos-system._tcp`.

Estimated PR size: ~400 LOC.

### #3 — IS-04 Node API (provider side)

The Node serves its own resource graph + heartbeats to a Registry.

- Codec: `internal/amwa/codec/is04/` — Node, Device, Source, Flow,
  Sender, Receiver JSON Schemas (v1.3.x first; v1.2.x added in #4b).
- Provider: HTTP server on Node side; `GET /self`, `/devices`,
  `/sources`, `/flows`, `/senders`, `/receivers`.
- Registration client: POST `/resource`, POST `/health/nodes/{id}`
  every 5 s, DELETE on shutdown.
- Heartbeat back-off + re-register on `404`.
- DNS-SD announce of Node API (P2P fallback).

Estimated PR size: ~1500 LOC + tests.

### #4 — IS-04 Registry (dual-face middleware)

The Registry is a hybrid — its left face **consumes** device
registrations + heartbeats, its right face **provides** the catalogue
to Controllers. Same process, same in-memory store, two faces. This
is why dhs needs a new top-level CLI verb `dhs registry nmos serve`
rather than reusing `dhs producer`.

Consumer face (Registration API server):
- POST `/resource` — ingest Node + Device + Source + Flow + Sender +
  Receiver registrations.
- POST `/health/nodes/{id}` — heartbeat watchdog (5 s ttl, 12 s
  timeout default).
- DELETE `/resource/{type}/{id}` — explicit deregistration.
- Garbage-collect resources when heartbeats lapse.

Provider face (Query API server):
- GET `/{nodes|devices|sources|flows|senders|receivers}` with
  RQL-style filters (`?label=...&description=...`).
- POST `/subscriptions` — returns a `ws_href` for change notifications.
- WebSocket subscription stream — emit
  `created` / `updated` / `deleted` / `sync` per resource change.

Shared infra:
- Resource store (in-memory map; persistence parked for #20).
- DNS-SD announce of BOTH faces (`_nmos-register._tcp` for the consumer
  face, `_nmos-query._tcp` for the provider face).

Estimated PR size: ~2000 LOC + tests.

#### #4b — IS-04 v1.2 + v1.1 back-compat

Layer older versions on top of #3/#4 once the v1.3 core is solid.

### #5 — IS-04 Controller (consumer side)

Three modes mirror the deployment modes from #1:

- **Registry mode** (default): DNS-SD browse for `_nmos-query._tcp`,
  Query API client, WS Subscription client.
- **Unicast Registry** (`--registry <ip>`): skip browse, hit the host
  directly.
- **Direct-Node** (`--peer-list peers.csv`): skip Registry entirely,
  fan-out walk per Node (Lawo VSM use-case — see
  [`matrix-compliance.md`](matrix-compliance.md#lawo-vsm--nmos-client-generic-driver)).
- WS Subscription client opens `ws_href`, parses notifications, fires
  `nmos_subscription_dropped` on unexpected close.
- CLI:
  - `dhs consumer nmos walk <reg-host>` — registry walk.
  - `dhs consumer nmos walk-node <node-host>` — single-Node walk.
  - `dhs consumer nmos walk-nodes --peer-list peers.csv` — fan-out walk.
  - `dhs consumer nmos watch <reg-host>` — Query WS subscription.
- Compliance events: `nmos_registry_not_supported`,
  `nmos_query_api_missing`, `nmos_node_api_version_downgrade`.

Estimated PR size: ~1000 LOC.

### #6 — BCP-002 + BCP-004 conformance

Pure JSON-shape rules baked into the IS-04 codec.

- BCP-002-01 Natural Grouping tags (`urn:x-nmos:tag:grouphint/v1.0`).
- BCP-002-02 Asset DI tags (`urn:x-nmos:tag:asset/...`).
- BCP-004-01 Receiver `caps.constraint_sets` schema.
- BCP-004-02 Sender `caps.constraint_sets` schema.

Validators run on encode + decode. No new CLI verbs.

Estimated PR size: ~500 LOC.

---

**Milestone — minimum viable NMOS plugin.** After #1–#6:
- `dhs producer nmos serve` advertises a real Node.
- A real registry can pick it up and walk its tree.
- `dhs consumer nmos walk` walks a real registry.
- Plugin appears in `dhs list-protocols`.
- Round-trip integration test against an external NMOS Registry
  reference impl (e.g. nmos-cpp-registry).

---

## Phase 2 — Connection control

### #7 — IS-05 Connection Management (provider side)

- Codec: `internal/amwa/codec/is05/`.
- Provider: REST endpoints under `/x-nmos/connection/v1.1/single/...`
  + `/bulk/...`.
- Three activation modes: immediate / scheduled-relative /
  scheduled-absolute (uses TAI clock; bring in PTP-aware time helper
  if not already shared).
- transport_params validation against caps.
- SDP encode/decode for RTP Senders.
- Surface IS-05 in IS-04 Device `controls` URN `urn:x-nmos:control:sr-ctrl/v1.1`.
- Compliance-event detection on the consumer-write side: peer 404 on
  `/constraints` → `nmos_constraints_endpoint_missing`; peer 405/404
  on `/bulk/*` → `nmos_bulk_unsupported`; peer rejects scheduled
  activation → `nmos_scheduled_activation_unsupported` and retry as
  `activate_immediate` (Lawo VSM behaviour per
  [`matrix-compliance.md`](matrix-compliance.md)).

Estimated PR size: ~1500 LOC.

### #8 — IS-05 Connection Management (consumer side)

- CLI: `dhs consumer nmos connect --sender <uuid> --receiver <uuid>`.
- PATCH `/staged` builder (master_enable, transport_params, activation).
- SDP fetch + parse.
- Bulk activation helper.

Estimated PR size: ~600 LOC.

### #9 — IS-08 Audio Channel Mapping (both sides)

Same shape as IS-05 but for in-Device audio routing.

- Codec for `/io`, `/map/inputs/{id}`, `/map/outputs/{id}`,
  `/map/active`, `/map/activations`.
- Provider: enforce mapping graph constraints.
- Consumer: CLI `dhs consumer nmos remap`.
- IS-04 Device `controls` URN `urn:x-nmos:control:cm/v1.0`.

Estimated PR size: ~900 LOC.

---

## Phase 3 — Events + tally

### #10 — IS-07 Event & Tally (provider side)

- WebSocket sender: `ws://.../events`.
- Message types: `state`, `health`, `reboot`, `shutdown`.
- Event-state cache + diff-emit on subscribe.
- `command_subscription`, `command_health` ingestion from clients.
- IS-04 Source `format = urn:x-nmos:format:data`, Sender
  `transport = urn:x-nmos:transport:websocket`.

Estimated PR size: ~700 LOC.

### #11 — IS-07 Event & Tally (consumer side)

- WS subscriber.
- CLI: `dhs consumer nmos watch-events <node>`.
- Event-type schemas (boolean, number, string, enum).

Estimated PR size: ~400 LOC.

### #12 — IS-07 MQTT transport

Layered on top of #10/#11. Adds `urn:x-nmos:transport:mqtt`. Pull in
an MQTT client lib (decision in PR — `eclipse/paho.golang` likely).

Estimated PR size: ~600 LOC.

---

## Phase 4 — Device control + monitoring (the big one)

### #13 — MS-05-02 datatype framework

Pure-Go class library. No wire yet.

- Class hierarchy: `NcObject`, `NcBlock`, `NcWorker`, `NcManager`,
  `NcDeviceManager`, `NcClassManager`, `NcSubscriptionManager`.
- Datatype registry (NcInt32, NcUint64, NcString, NcEnum, NcStruct,
  NcArray, NcParameter, etc.).
- Touchpoint helper linking model OIDs back to IS-04 UUIDs.
- Class-ID dotted-format (`1.1.1`, etc.).

Estimated PR size: ~1500 LOC + tests.

### #14 — IS-12 wire (provider side)

- WebSocket server with JSON envelope (`messageType`,
  `protocolVersion`, `handle`).
- Per-class method dispatch (Get / Set / InvokeMethod).
- Subscription manager: track subscribed OIDs, emit Notification on
  property change.
- Surface in IS-04 Device `controls` URN `urn:x-nmos:control:ncp/v1.0`.

Estimated PR size: ~1500 LOC.

### #15 — IS-12 wire (consumer side)

- WebSocket client.
- Datatype marshaller + unmarshaller.
- Subscription tracker.
- CLI: `dhs consumer nmos ncp <ws-url>` — walk + Get/Set.

Estimated PR size: ~700 LOC.

### #16 — BCP-008-01 Receiver Status Monitoring

Feature set on top of MS-05-02 / IS-12.

- `NcReceiverMonitor` class (linkStatus, connectionStatus,
  externalSynchronizationStatus, streamStatus + per-status messages).
- Touchpoint to IS-04 Receiver UUID.

Estimated PR size: ~500 LOC.

### #17 — BCP-008-02 Sender Status Monitoring

Mirror of #16 on the Sender side.

- `NcSenderMonitor` class (linkStatus, transmissionStatus,
  externalSynchronizationStatus, essenceStatus).

Estimated PR size: ~500 LOC.

---

## Phase 5 — Codec / transport profiles

Each is a profile applied during IS-04 + IS-05 encoding, not a
standalone codec. Land per device-class as needed.

### #18 — BCP-006-01 NMOS With JPEG XS

Flow `urn:x-nmos:format:video.jpegxs`; SDP `a=fmtp` rules; IS-04
Source/Flow/Sender attribute rules.

### #19 — BCP-006-04 NMOS With MPEG-TS

Flow `urn:x-nmos:format:mux` with `media_type=video/MP2T`.

(BCP-006-02 H.264, BCP-006-03 H.265, BCP-007-01 NDI — tracked but
deferred until WIP versions stabilise upstream at AMWA.)

---

## Phase 6 — Polish

### #20 — Persistence

Registry resource store on disk (file-backed, atomic rename pattern
already used by `internal/storage/`).

### #21 — IS-13 Annotation

WIP at AMWA — implement once spec stabilises.

### #22 — Wireshark dissector

`internal/amwa/wireshark/dhs_nmos.lua`.

- HTTP body inspection: highlight `/x-nmos/...` paths, decode JSON
  response bodies into per-resource sub-trees.
- WS subscription stream: render `pre`/`post`/`type` in Info column.
- IS-12 envelope: render `messageType` + `handle` + per-OID summary.
- IS-07 envelope: render `message_type` + value snapshot.

Per house rules: full-from-scratch, no delegation to Wireshark
built-in. Stage at the same time as Phase 1 for early debugging.

### #23 — `dhs metrics` integration

Per-session counters (registrations, subscriptions, IS-12 commands,
IS-07 events) wired into the existing `internal/metrics/Connector`
shape, exposed via `--metrics-addr :9100` and CSV/MD export.

### #24 — Proxy gateway mode

`dhs producer nmos serve --proxy <upstream-registry>` — re-expose an
upstream Registry as a local Node + Registry pair, bridging
controllers to devices through dhs.

---

## Phase 7 — Out of scope (v1)

- IS-10 Authorization (OAuth 2 / mTLS) — separate epic.
- TLS / wss everywhere — couples to IS-10.
- Persistent IS-04 store across restarts — see #20.
- IS-13 Annotation while still WIP at AMWA.
- BCP-007-01 NDI while still WIP at AMWA.
- BCP-006-02 H.264 / BCP-006-03 H.265 while still WIP at AMWA.

---

## Per-spec asymmetry summary (mirror question)

The user asked: "consumer + provider mirror same commands?" Short
answer: **only for the pure-JSON BCPs**. Detail:

| Spec | Mirror? |
|---|:---:|
| IS-04 | ✗ (3 surfaces — Node, Registry, Controller) |
| IS-05 | partial (REST shape mirrors; validation diverges) |
| IS-07 | ✗ (producer attaches to in-process state) |
| IS-08 | partial |
| IS-09 | ✗ (server is Registry; client is Node) |
| IS-12 + MS-05-02 | ✗ (server hosts model tree, client only marshals) |
| IS-13 | partial |
| BCP-002 / BCP-004 / BCP-006 / BCP-007 | ✓ pure JSON-shape, no wire |
| BCP-008-* | ✗ (server-only feature set) |

So the dhs CLI surface for NMOS will follow the existing
producer/consumer split, but the **commands inside each side will
diverge much more than for our other 8 protocols**. Plan code to allow
that asymmetry instead of forcing symmetry.
