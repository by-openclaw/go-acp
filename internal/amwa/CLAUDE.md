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

### DNS-SD service-name version transition (binding)

IS-04 v1.2 renamed the registration service from
`_nmos-registration._tcp` (legacy) to `_nmos-register._tcp` (modern).
v1.0 / v1.1 Registries advertise on the legacy name only; v1.2+ on the
modern one. The watcher MUST browse BOTH concurrently — a Node that
browsed only the modern name would miss every legacy Registry on the
link. See the
`internal/amwa/codec/dnssd/types.go` constants `ServiceRegister` +
`ServiceRegisterLegacy`.

### DNS-SD backend selection (multi-OS)

`internal/amwa/session/dnssd/` exposes [Browser] / [Responder]
**interfaces**, picked at process start by `NewBrowser` /
`NewResponder` based on what's reachable on the host:

| Host | Backend | Why |
|---|---|---|
| Linux + avahi-daemon on system DBus | **Avahi** via `org.freedesktop.Avahi.Server` (pure-Go via `github.com/godbus/dbus/v5`) | sub-ms cascade-timing per AMWA test_05/15/16; full RFC 6762/6763 corner-case handling |
| macOS | **Bonjour** via `libSystem` (CGo, planned #196) | Bonjour daemon always present; canonical path |
| Windows + Bonjour Service installed | **Bonjour** via `dnssd.dll` (CGo, planned #195) | matches Cerebrum / nmos-cpp Windows behaviour |
| anything else (no daemon, slim containers, etc.) | **stdlib** — pure Go `net.UDPConn` on 224.0.0.251:5353 | universal fallback; never removed; lower perf (500 ms read-deadline jitter) means cascade-timing tests degrade |

The choice is logged at INFO at start (`dnssd: using Avahi via DBus
(system daemon)` / `dnssd: using stdlib browser (no system daemon
detected)`). **Stdlib path is the floor — never delete it.** Future
contributors keep all OS paths so that a slim container (or a Windows
host without Bonjour, or a misconfigured systemd-less Linux box) still
runs dhs — degraded conformance, never broken.

Tracking: epic #194 (full daemon delegation), Phase A landed on
`feat/nmos-is04-amwa-conformance` for Linux Avahi; Phases B-windows
(#195) + B-macos (#196) + C (LXC multi-distro #197) follow on the same
protocol branch.

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

Every IS-* and MS-* spec has multiple stable releases that coexist on
the wire. The plugin MUST implement every track listed below — both
the wire `api_ver` (major.minor) AND the latest patch within that
minor (the spec text we strictly comply with). Authoritative version
numbers come from `internal/amwa/reference.md`.

Verified against specs.amwa.tv on **2026-08-26**. `impl` is what
`internal/amwa/codec/` actually ships; a gap between the two columns is
a MISSING implementation by the rule below, never a scope decision.

| Spec | Published (latest patch) | Wire `api_ver` | impl |
|---|---|---|---|
| IS-04 Discovery & Registration | v1.0.3 / v1.1.3 / v1.2.2 / **v1.3.3** | v1.0–v1.3 | ✅ v1.0–v1.3 |
| IS-05 Connection Management | v1.0.2 / v1.1.2 / **v1.2.0** | v1.0–v1.2 | ✅ v1.0–v1.2 |
| IS-07 Event & Tally | **v1.0.1** | v1.0 | ✅ v1.0 |
| IS-08 Channel Mapping | **v1.0.1** | v1.0 | ✅ v1.0 |
| IS-09 System Parameters | **v1.0.0** | v1.0 | ✅ v1.0 |
| IS-11 Stream Compatibility | **v1.0.0** | v1.0 | ✅ v1.0 |
| IS-12 Control Protocol | **v1.0.1** | v1.0 | ✅ v1.0 |
| IS-14 Device Configuration | **v1.0.0** | v1.0 | ✅ v1.0 |
| MS-05-01 / MS-05-02 | **v1.0.0** | v1.0 | ✅ v1.0 |
| BCP-002-01 / -02 Grouping, Asset | **v1.0.0** | — | ✅ |
| BCP-003-01 Secure Communications | **v1.0.1** | — | ⚠️ table said v1.0.0 |
| BCP-003-02 Authorization | **v1.0.0** | — | ❌ **MISSING** |
| BCP-003-03 Certificate Provisioning | **v1.0.0** | — | ❌ **MISSING** |
| BCP-004-01 / -02 Receiver/Sender Caps | **v1.0.0** | — | ✅ |
| BCP-005-01 EDID Mapping | **v1.0.0** | — | ❌ **MISSING** |
| BCP-005-02 / -03 IPMX HKEP, PEP | **v1.0.0** | — | ❌ **MISSING** |
| BCP-006-01 JPEG XS / -04 MPEG TS | **v1.0.0** | — | ✅ |
| BCP-007-03 MXL | **v1.0.0** | — | ❌ **MISSING** |
| BCP-008-01 / -02 Status Monitoring | **v1.0.0** | — | ✅ |

**Strict-spec rule (binding, no exceptions for AMWA-published versions):**
every minor AMWA has published is in scope. There is **no "deferred",
no "out of scope by design", no "we don't see it in the wild"** for
any minor in this table. If a minor is not implemented today, it is
a *missing* implementation to be added — never framed as a stable
product decision.

Convention: every listed version is a selectable parameter on the
plugin (mirroring `proto:tsl` v3.1/v4.0/v5.0). DNS-SD `api_ver` TXT
advertises every supported minor comma-separated
(e.g. `api_ver=v1.0,v1.1,v1.2,v1.3`). Server URL trees serve every
minor in parallel
(`/x-nmos/registration/v1.0/`, `/v1.1/`, `/v1.2/`, `/v1.3/`, …) on
the same store. Default to the highest mutually-supported minor;
**never silently downgrade**, never silently drop a track. Skipping
any version listed above is a spec violation, not a deferral.

Genuinely WIP at AMWA (no stable release yet — land when stable):
IS-13 Annotation, BCP-006-02 H.264, BCP-006-03 H.265, BCP-007-01 NDI.
These are the ONLY legitimate "land when stable" carve-outs; they
land the moment AMWA publishes a stable release.

**This table drifts, and drift here is invisible.** The 2026-08-26
audit found the previous version of it four specs and one minor behind
what AMWA had already published — IS-05 v1.2.0, IS-11, IS-14,
BCP-003-02/-03, BCP-005-01/-02/-03 and BCP-007-03 were all released
and none appeared here, so nothing flagged them as missing. Re-verify
against specs.amwa.tv whenever the AMWA testing tool gains a suite we
have no row for: the tool ships suites for exactly the published set,
so a suite we cannot name IS the signal. Suites present on the tool
today with no implementation behind them: BCP-005-01,
BCP-007-03.

---

### What the NODE role actually serves

The table above is about CODECS. A codec with nothing serving it is not
a working Node, and for a long time IS-07 and IS-08 were exactly that:
complete, tested, and mounted nowhere. This table is about the Node
provider (`internal/amwa/provider/`), which is a different question.

| API | Node surface | Where |
|---|---|---|
| IS-04 Node API | `/x-nmos/node/{ver}/` + DNS-SD + registration client | `node.go` |
| IS-05 Connection | `/x-nmos/connection/{ver}/` + SDP + activation scheduler | `connection*.go` |
| IS-07 Event & Tally | `/x-nmos/events/{ver}/` REST + the WebSocket at `…/ws` | `events.go` |
| IS-08 Channel Mapping | `/x-nmos/channelmapping/{ver}/` incl. `inputs`/`outputs` | `channelmapping.go` |
| IS-09 System | **client**, not server — the Node reads a System API | `system_client.go` |

IS-09 is the odd one and worth stating: every other row is something
the Node SERVES, and that one is something it CONSUMES. A Node that
ignores the System API picks its Registry from mDNS priority alone, so
a plant that deliberately pointed its devices at one Registry finds
this Node quietly registered somewhere else.

Two deployment modes are mutually exclusive on the wire and therefore
cannot both be tested against one process: IS-04 §4.2.1 requires a
registered Node to STOP advertising `_nmos-node._tcp`, so peer-to-peer
(Mode D) needs `--no-registry`.

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
   networks block multicast DNS for security policy reasons; some
   vendors ship Registry-less peer-to-peer mode that *still* relies
   on mDNS (EVS Cerebrum). Plan for four deployment modes from day
   one — all spec-compliant; the AMWA NMOS specs (IS-04 §3) remain
   the source of truth, modes only name `(mDNS, Registry)`
   combinations:
   - **Mode A — Full mDNS + Registry** (greenfield / spec-compliant peers).
   - **Mode B — Unicast Registry** (`--no-mdns --registry <ip>:<port>`).
   - **Mode C — Direct-Node** (`--no-mdns --no-registry --peer-list peers.csv`).
   - **Mode D — mDNS direct-Node** (`--mdns --no-registry`) — Cerebrum P2P.
   Default to mDNS but never assume it works. See
   [`docs/matrix-compliance.md`](docs/matrix-compliance.md) and
   [`docs/cerebrum-interop.md`](docs/cerebrum-interop.md).
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
    [`docs/matrix-compliance.md`](docs/matrix-compliance.md). Follows
    the repo-wide pattern in top-level `CLAUDE.md`
    "Spec-strict, no-workaround posture" → exception clause.
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

---

## Codec architecture (locked pattern — every NMOS spec)

Every NMOS spec MUST implement **every stable version listed above**,
not just the latest. To make that tractable across the whole 14-spec
suite — and to keep the cost of supporting a future v1.4 / v2.0
constant rather than linear — every spec follows ONE locked codec
pattern. The shared base is in
[`internal/amwa/codec/spec/`](codec/spec/) and is stdlib-only.

### Roles per layer (Layer 1, codec)

```
internal/amwa/codec/
  spec/                          # NMOS-wide base (Versioned interface, generic Registry[T],
                                 # SelectHighestMutual, ComplianceEvent + Reporter)
  is04/  is05/  is07/  is08/     # one folder per spec — canonical structs + Codec interface
  is09/  is12/  ms0501/  ms0502/
    codec.go                     # extends spec.Versioned with the spec's resource methods
    {node,device,...}.go         # canonical union structs (every minor's fields, omitempty)
    patterns.go enums.go         # shared regex / URN tables
    absorb.go                    # decode that records unknown fields instead of failing
    schemas/                     # AMWA's OWN JSON Schemas, verbatim, per patch release
      v1.0.3/ v1.1.3/ v1.2.2/ v1.3.3/
    v10/  v11/  v12/  v13/       # per-minor Strategy impls — SELF-CONTAINED
      codec.go                   # this minor's identity + its own drop table + its strip

  bcp/                           # JSON-shape validators (no own wire — layer onto host spec)
    bcp00201/  bcp00202/         # BCP-002-01 / 002-02
    bcp00401/  bcp00402/         # BCP-004-01 / 002-02
    bcp00601/  bcp00604/         # BCP-006-01 / 006-04
    bcp00801/  bcp00802/         # BCP-008-01 / 008-02
```

### Validation comes from AMWA's schemas. We write no rules.

**Every IS-04 validation rule lives in `codec/is04/schemas/`, copied
verbatim from github.com/AMWA-TV/is-04 at each patch tag.** They are
never edited. When AMWA publishes a new patch, the fix is to copy the
new set in — not to adjust Go.

This is the single most important rule in this package, because every
IS-04 bug we have had came from breaking it. Hand-written validators
are a paraphrase of the schemas, and a paraphrase drifts:

- a non-empty check on `label`/`description` that no schema states —
  failed all 176 Senders on a real EVS Neuron
- a v1.0 Flow failed for missing `frame_width`, which v1.0 has no
  concept of
- a v1.0 Device refused for `controls`, which AMWA's own v1.0
  `device.json` permits — the device was right and we were wrong
- `chassis_id: "ZZ"` rejected, where AMWA defines
  `anyOf [MAC-pattern, "^.+$", null]` and only `""` is invalid

sony/nmos-cpp has none of this class of bug for the same reason: it
embeds the AMWA schemas (`Development/nmos/is04_schemas/`) and
validates against them.

`internal/amwa/codec/jsonschema` is a stdlib-only draft-04 validator
(ADR-0006) covering exactly the keywords a scan of the four schema sets
reports. **An unimplemented keyword is REPORTED, never skipped** — a
silent skip means a document went unchecked and nobody notices;
`TestNoUnimplementedKeyword` fails the build if AMWA ships one.

### Each minor is self-contained

A `vXX/` package holds **only** what is specific to that minor: its
identity, its own drop table, its own strip helper. Nothing is shared
between minors and the duplication is deliberate — a change to v1.0
must be incapable of altering how v1.2 behaves. Each minor also gets
its own schema compiler over its own directory, so a v1.0 validator
physically cannot load a v1.3 schema.

The two directions have opposite postures, and that asymmetry is the
point:

| Direction | Rule | Posture |
|---|---|---|
| **encode** | drop what this minor lacks, then schema-check | **FATAL** — emitting a payload AMWA rejects is our bug |
| **decode** | parse tolerantly, schema deviations become events | **absorbed** — `nmos_is04_schema_deviation` at Warn |

We must not EMIT what AMWA would reject. But refusing to READ it costs
the operator the whole resource and tells them nothing actionable. Same
rule in IS-09: an out-of-spec `/global` is absorbed, because discarding
it sends the Node to a Registry the operator did not choose.

Three rules follow, and breaking any of them is how this rotted before:

- **Never hand-write a validation rule.** If the schema does not say
  it, it is not a rule. AMWA's schema is the authority, not our
  reading of it.
- **A drop table must never strip a REQUIRED property.** A required
  property is by definition not a later-minor field. Listing
  `channels` at v1.1 — where `source_audio.json` requires it — made
  every audio Source fail to register, and IS-04-01 reported it four
  tests away as "not found in the registry". `schemas.RequiredLeaves`
  plus each package's `drop_test.go` catch that at unit-test speed.
- **Decode at a minor validates at that minor.** Per-minor `DecodeX`
  calls `is04.ParseX` (decode + absorb + report, no validation), never
  `is04.DecodeX`, which validates against the canonical latest rules.

### OOP principles enforced

| Principle | Mechanism |
|---|---|
| **Encapsulation** | Per-minor field-gating + per-minor validation rules live inside each `vXX.Codec` impl. The plugin layer never sees minor-specific logic. |
| **Open/closed** | New minor (e.g. IS-04 v1.4 when AMWA ships it) = +1 file `v14/codec.go` + 1 init-time `Register` call. Zero edits to existing files. New spec (e.g. IS-13 when stable) = +1 folder following the template. |
| **Liskov substitution** | Any `Codec` interchangeable on the Registry store, Node API server, Controller client. Plugin code receives `is04.Codec` from `spec.Registry[is04.Codec]`. |
| **Single responsibility** | `spec/` is contracts + cross-cutting infra only. Per-spec packages hold canonical types. `vXX/` packages hold version deltas only. Plugin layer holds business logic only. |
| **Interface segregation** | `spec.Versioned` is three methods. Per-spec `Codec` interfaces extend it with only that spec's resource methods. No god-interface. |
| **Dependency inversion** | Plugin code depends on `spec.Versioned` + per-spec `Codec` interface, never on `vXX/*` directly. depguard rule enforces this in CI. Concrete impls are wired in via `init()` registration; tests substitute fakes via DI. |

### Idempotent registration

`spec.Registry[T].Register` is safe to call repeatedly with the same
instance under the same `(SpecID, APIVer)` key. Calling with a
different instance under an already-registered key panics — that's a
duplicate-init bug. Empty SpecID / APIVer / SpecPatch panic. Same
semantics as `internal/consumer.Register`.

### Cross-cutting concerns in `spec/`

- **Version selection.** `spec.SelectHighestMutual` picks the highest
  version mutually supported between us and a peer's `api_ver` TXT.
  Returns `ErrNoCommonVersion` (typed) when intersection is empty so
  the caller fires a compliance event — never silently downgrade.
- **Compliance events.** `spec.ComplianceEvent` carries
  `(SpecID, APIVer, SpecPatch, Code, Severity, Detail, Resource,
  PeerHost, At)`. `spec.Reporter` is the DI seam codecs use to emit
  events. Production wires a logger-backed reporter via
  `cmd/dhs/cmd_nmos.go`; tests pass `*spec.SliceReporter` for
  assertion. Codecs NEVER reach into a global.
- **No silent workarounds.** Per top-level CLAUDE.md "Spec-strict,
  no-workaround posture", every absorbed deviation MUST fire a
  ComplianceEvent — the codec keeps decoding, but the deviation is
  audited.

### Spec-strict version coverage rule

Skipping any minor listed in the [Versioning](#versioning) table is a
**spec violation**, not a deferral. The plugin owes:

- DNS-SD `api_ver` TXT advertises every supported minor
  comma-separated.
- Server URL trees serve every minor in parallel
  (`/x-nmos/<api>/v1.1/`, `/v1.2/`, `/v1.3/`, …) on one canonical
  store.
- Highest-mutual-minor selection on every peer handshake; never
  silently downgrade outside the intersection.

WIP-at-AMWA specs (IS-13 Annotation, BCP-006-02 H.264,
BCP-006-03 H.265, BCP-007-01 NDI) are the only legitimate "not yet"
scope — they land when AMWA stabilises them.

## Implementation order

Minimum viable slice for "9th protocol plugin works":
**IS-09 → IS-04 → IS-05 → IS-07 (WebSocket only) + BCP-002 + BCP-004**.

---

## Architecture diagrams

See [`docs/architecture.md`](docs/architecture.md) — one ASCII diagram
per spec showing role topology, transports, and message direction.
