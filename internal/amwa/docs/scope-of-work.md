# NMOS Scope of Work

Captured 2026-05-02 from design discussion on PR #189
(`feat/nmos-is04-amwa-conformance`). Living document — every commit that
expands the NMOS feature surface should update the matching row.

> Read order before touching code: this file → `architecture.md` →
> `integration-plan.md` → `internal/amwa/CLAUDE.md`.

## 0a. Binding rule — DNS-SD backend coverage (multi-OS)

`internal/amwa/session/dnssd/` exposes [Browser] / [Responder] interfaces
backed by per-OS implementations. Selection happens at process start
(`detect.go`) and is logged.

| Platform | Backend | Status | Issue |
|---|---|---|---|
| Linux + avahi-daemon | Avahi via DBus (pure-Go) | **landed** `eb55fb2` | #194 Phase A |
| macOS | Bonjour via libSystem (CGo) | planned | #196 |
| Windows + Bonjour Service | Bonjour via dnssd.dll (CGo) | planned | #195 |
| any host | stdlib `net.UDPConn` + in-tree codec | always present (floor) | — |
| Linux multi-distro test rig | LXC (Debian/Ubuntu/RHEL/Rocky) | planned | #197 |

**Stdlib path is the floor — never delete.** Anyone extending the
DNS-SD layer must keep all three OS paths compiling green. Removing
one to "simplify" breaks the user's fleet (WinSrv + Debian + Ubuntu +
RHEL + Rocky). The interface abstraction is the seam: backends are
isolated by build tags + a small `tryDaemon*` factory.

User explicitly approved the godbus dep + CGo paths 2026-05-02 with
the constraint: "no external lib was only to reduce risk, but if it
is mandatory we will do" + "we have winsrv, linux debian, ubuntu, rhel,
rocky". Strict-spec + perf + multi-OS + idempotent are non-negotiable.

## 0. Binding rule — strict every AMWA-published version

For every spec listed in `internal/amwa/CLAUDE.md` Versioning table:

- **Every minor AMWA has published is in scope.** No exceptions.
- **No "deferred", no "out of scope by design", no "we don't see it in
  the wild".** If a minor in the table is unimplemented today, it is
  *missing* and must be added — never reframed as a stable product
  decision.
- The only legitimate "land when stable" carve-outs are AMWA-WIP specs
  with no published stable release (IS-13 Annotation, BCP-006-02 H.264,
  BCP-006-03 H.265, BCP-007-01 NDI).
- This rule is binding for both Track A (Node) and Track B (Registry).
  A "universal Registry" or "universal Node" cannot honestly carry that
  adjective while skipping any AMWA-published minor.

See memory `feedback_amwa_strict_all_versions.md` and top-level
`CLAUDE.md` "Spec-strict, no-workaround posture" for the full rule.

---

## 1. Vision — what dhs becomes in the NMOS plane

```
            ┌───────────────────────┐
            │  Cerebrum / any AMWA  │ upstream Registry / Controller
            │       Registry        │
            └─────────▲─────────────┘
                      │ IS-04 Node API: dhs Node registers UP
            ┌─────────┴─────────────┐
            │      [dhs Node]       │ ← Track A — connector work
            │   IS-04..IS-12, BCPs  │   shipped (PR #189 v1.3 56/74)
            └─────────▲─────────────┘
                      │ Registration client → upstream Registry
                      │ (independent of any local Registry)
                      │
            ┌─────────┴─────────────┐ ◄────── sells standalone
            │   [dhs Registry]      │         Track B — product work
            │ universal, multi-ver  │         (NEW SCOPE)
            │ multi-vendor, hot     │
            │ TLS / OAuth / MQTT    │
            │   all opt-in          │
            └─────────▲─────────────┘
                      │ vendors register IN
        ┌─────────────┼─────────────────┐
        │             │                 │
   [Lawo VSM]   [Sony NXL]      [Ross Inception]   …vendor heterogeneity
   IS-04 v1.3   IS-04 v1.2       IS-04 v1.1         …no BCP-002 / no auth / etc.
```

dhs sits in the middle layer of the broadcast control plane:

- **South** — protocol connectors (ACP1/ACP2/Ember+/Probel/TSL/OSC/Cerebrum) talk to physical devices.
- **Middle** — dhs Node maps device state into NMOS resources and registers UP to a Registry (any vendor).
- **North** — dhs Registry (new product) accepts registrations from any vendor's Node, serves any IS-04 minor, optionally federates UP to Cerebrum.

A single dhs box can host both Node and Registry roles in the same process; they're independent listeners.

---

## 2. Two tracks, parallel

| Track | Scope | Product status | Ships when |
|---|---|---|---|
| **A — Node conformance** | dhs as universal Node tested against real Registry across versions × deployment shapes | Existing connector work, extends PR #189 | Each phase merges incrementally |
| **B — dhs-registry product** | dhs as universal Registry, sellable as turn-key single binary | NEW — not started yet, separate epic | Phased: Phase 1 = MVP, Phase 5 = full vendor-leniency |

Tracks share `internal/amwa/codec/` only. Otherwise disjoint:
- A → `internal/amwa/provider/`
- B → `internal/amwa/registry/` (new package)

---

## 3. Track A — Node conformance

### 3.1 What's done (PR #189)

- IS-04-01 v1.3 against AMWA Mock Registry: 56 Pass / 1 Fail / 1 Warning / 1 Manual.
- The single Fail (test_16) is a Docker Desktop Windows cascade-timing race; expected to pass on Linux Docker with native networking.
- 10 spec-correct quirks captured in memory `feedback_amwa_test_quirks.md`.

### 3.2 What changes per the design discussion

**Drop the AMWA Mock track entirely.** Test only against real Registry stacks. Reasons:
- Round-trip "own encoder ↔ own decoder" is not wire compliance (memory `feedback_real_peer_closes_self_test`).
- Mock semantics (shared resource store, fixed-version advertise) hide bugs that real nmos-cpp surfaces.
- Production deployments never see AMWA Mock — they see Cerebrum or nmos-cpp.

**Five deployment shapes, five compose templates:**

| Shape | Discovery | Transport | Auth | IS-07 | Side services |
|---|---|---|---|---|---|
| `lab-mdns` | mDNS only | HTTP | none | WebSocket | nmos-cpp + AMWA Test + dhs Node |
| `prod-dnssd` | unicast DNS-SD | HTTP | none | WebSocket | + dnsmasq (DNS-SD records) |
| `prod-tls` | DNS-SD | HTTPS | none | WSS | + step-ca (internal CA) |
| `prod-full` | DNS-SD | HTTPS | OAuth2 | WSS | + Keycloak (OAuth2) |
| `prod-mqtt` | DNS-SD | HTTPS | OAuth2 | MQTT | + Mosquitto (broker) |

**Layering order: lab-mdns → prod-dnssd → prod-tls → prod-full → prod-mqtt.** Each layer = its own commit. Never merge red CI.

### 3.3 Test cell matrix (real-Registry only, mock dropped)

| Suite | Versions | × Shapes | Cells |
|---|---|---|---|
| IS-04-01 (Node API) | v1.0, v1.1, v1.2, v1.3 | 4 (mdns, dnssd, tls, full) | 16 |
| IS-04-03 (Discovery) | v1.0, v1.1, v1.2, v1.3 | 4 | 16 |
| IS-05-01 (Connection) | v1.0, v1.1 | 1 (lab-mdns) | 2 |
| IS-05-02 (Connection + IS-04) | IS-04 v1.{0,1,2,3} × IS-05 v1.{0,1} | 3 (mdns, dnssd, full) | 24 |
| IS-07-01 (Events API) | v1.0 | 1 | 1 |
| IS-07-02 (Events Comm WS) | v1.0 | 1 (lab-mdns) | 1 |
| IS-07-02 (Events Comm MQTT) | v1.0 | 1 (prod-mqtt) | 1 |
| IS-08-01 (Channel Mapping) | v1.0, v1.1 | 1 | 2 |
| IS-08-02 (CM + IS-04) | IS-04 v1.{0,1,2,3} × IS-08 v1.{0,1} | 3 | 24 |
| IS-10-01 (Authorization) | v1.0 | 1 (prod-full) | 1 |
| IS-12-01 (Control) | v1.0 | 1 | 1 |
| BCP-002-01 / 02 | v1.0 | 1 (lab-mdns) | 2 |
| BCP-003-01 (TLS) | v1.0 | 1 (prod-tls) | 1 |
| BCP-003-02 (Auth) | v1.0 | 1 (prod-full) | 1 |
| BCP-004-01 (Receiver Caps) | v1.0 | 1 | 1 |
| BCP-006-0X (Codec profiles) | v1.0 each | 1 | 3 |
| BCP-008-01 / 02 | v1.0 | 1 | 2 |
| **Total** | | | **~99** |

DRY layout: 5 compose templates + ~99 `.env` files. One-line runner per cell.

### 3.4 Folder layout

```
tests/integration/nmos/
├── matrix.md                          # live status of every cell
├── README.md                          # how to run + matrix overview
├── compose/
│   ├── lab-mdns.yml                   # nmos-cpp + dhs Node + AMWA Test
│   ├── prod-dnssd.yml                 # + dnsmasq
│   ├── prod-tls.yml                   # + step-ca
│   ├── prod-full.yml                  # + Keycloak
│   └── prod-mqtt.yml                  # + Mosquitto
├── env/
│   ├── is04-01/{lab-mdns,prod-dnssd,prod-tls,prod-full}-v1.{0,1,2,3}.env
│   ├── is04-03/...
│   ├── is05-01/{v1.0,v1.1}.env
│   ├── is05-02/{shape}-is04v1.{0,1,2,3}-suitev1.{0,1}.env
│   ├── is07-02/{ws-v1.0,mqtt-v1.0}.env
│   ├── is10-01/prod-full-v1.0.env
│   ├── is12-01/v1.0.env
│   ├── bcp-002-01/v1.0.env
│   ├── bcp-003-01/prod-tls-v1.0.env
│   └── ...
├── runner/
│   ├── run.ps1                        # .\runner\run.ps1 is04-01 prod-tls-v1.3
│   ├── run.sh                         # POSIX equivalent
│   └── run-all.ps1                    # iterate every .env, write matrix.md
└── registry/
    └── README.md                      # parked — see Track B for dhs-registry
```

`.env` per cell, ≈ 4 lines:

```
SUITE=IS-04-01
DHS_API_VER=v1.3
SHAPE=prod-tls
TEST_VERSION=v1.3
COMPOSE_PROJECT_NAME=dhs_is04_01_prod_tls_v13
```

### 3.5 Side-service images (Track A)

Test-only Docker images, all OSS, no Go-dep impact:

| Service | Image | Purpose |
|---|---|---|
| nmos-cpp Registry | `rhastie/nmos-cpp:latest` | real Registry serving all IS-04 minors |
| AMWA Test | `amwa/nmos-testing:latest` | probe-side test runner |
| dnsmasq | `jpillora/dnsmasq:latest` | unicast DNS-SD |
| step-ca | `smallstep/step-ca:latest` | internal CA for BCP-003-01 |
| Keycloak | `quay.io/keycloak/keycloak:latest` | OAuth2 for IS-10 / BCP-003-02 |
| Mosquitto | `eclipse-mosquitto:latest` | MQTT broker for IS-07-02 |

---

## 4. Track B — dhs-registry product

New scope. Sellable single-binary Registry. Turn-key — `dhs-registry serve` "just works" with mDNS-only out of the box; production knobs layer in via flags.

### 4.1 Universal Registry — strict-spec requirements

Every row table-stakes; missing rows = not universal:

| Capability | What it must do | Why vendor heterogeneity forces it |
|---|---|---|
| **IS-04 versions** | v1.0 + v1.1 + v1.2 + v1.3 simultaneously, parallel URL paths | mixed-vendor fleet (Lawo v1.3, Sony v1.2, legacy v1.0) |
| **Discovery (advertise)** | mDNS AND unicast DNS-SD from one binary | some vendors browse mDNS only, some only DNS-SD |
| **Schema downcast on serve** | Accept v1.0-shape POST, serve at v1.3 path with field-completion / drop | Controllers querying records produced on a different version |
| **Vendor leniency** | Accept records missing optional BCP fields, fire compliance event, never reject | many vendors lack BCP-002 grouphint, BCP-004 caps |
| **TLS** | HTTP or HTTPS, opt-in via `--tls-cert/--tls-key` | BCP-003-01 is best-current, not mandatory |
| **OAuth2** | None or Bearer required, opt-in via `--oauth-issuer` | IS-10 not deployed everywhere |
| **IS-07 events** | WebSocket always; MQTT if `--mqtt-broker` set | some vendors WS-only, some need MQTT |
| **Query API** | REST GETs + WebSocket `/query/v1.X/ws/{id}` push | Controllers subscribe via WS for live updates |
| **Heartbeat** | Accept any cadence, refresh in-memory TTL, evict on miss | vendor heartbeat intervals vary 5s–30s |
| **Cascading** | Optionally forward records UP to a parent Registry | multi-site federation |

### 4.2 Phasing

| Phase | Scope | Issue / branch |
|---|---|---|
| **B-0 — design** | Architecture doc, ADRs for store / HA / federation, register at `internal/amwa/registry/CLAUDE.md` | — |
| **B-1 — MVP** | Single instance, file-backed records, mDNS-only advertise, all four IS-04 minors, **no TLS, no OAuth, no MQTT**. Conformance: AMWA IS-04-02 (Registry suite) + IS-04-03. **Core deliverable: spec-correct universal Registry.** | new epic |
| **B-2 — production layers** | + unicast DNS-SD advertise + TLS (BCP-003-01) + OAuth2 (IS-10 + BCP-003-02) + MQTT broker integration (IS-07-02 events) | follows B-1 |
| **B-3 — high availability** | shared-filesystem active-passive (closes #127), lease-based mDNS advertise election, drain-before-exit, cold-start eviction | follows B-2 |
| **B-4 — federation** | Parent-Registry support: dhs-registry registers itself UP to Cerebrum (or any AMWA Registry), forwards record updates | follows B-3 |
| **B-5 — vendor leniency** | Vendor-leniency mode (per-vendor compliance event taxonomy), schema-downcast quality polish, real-vendor interop (Lawo / Sony / Ross) | follows B-4 |

### 4.3 Architectural rule — never restart

**Process lifetime > data lifetime > config lifetime.** Every mutation surface is hot-swappable; the only acceptable cause for `dhs-registry` to exit is binary upgrade, and even that goes through HA failover where possible.

| Layer | What changes | Restart? | Hot-swap mechanism |
|---|---|---|---|
| Node lifecycle (register / deregister / heartbeat / TTL expiry) | continuous | NEVER | RWMutex on in-memory index, atomic per-record file writes |
| Record content (field updates, GC) | continuous | NEVER | atomic `.tmp` → `rename` per file; readers see old or new, never torn |
| Configuration (ACL, vendor-leniency, parent URL, log level) | rare | NEVER | SIGHUP / file-watch → `atomic.Pointer[*Config]` swap |
| TLS cert (rotation: Let's Encrypt 90d, internal CA rotation) | rare | NEVER | `tls.Config.GetCertificate` closure re-reads from disk |
| OAuth2 issuer (JWKS rotation) | rare | NEVER | periodic JWKS refetch + atomic swap |
| API version enable/disable (turn v1.0 off when last legacy Node retires) | rare | NEVER | atomic mux pointer swap |
| HA failover (active partner dies) | event | NEVER on the survivor | lease expiry → atomic role swap |
| Binary upgrade (dhs-registry vN → vN+1) | rare | only the upgrading instance | rolling: drain passive → upgrade → swap → upgrade ex-active. Single instance: brief gap, Nodes retry on next heartbeat (spec-tolerant) |

**Concrete design rules that fall out of this:**

| Rule | Why |
|---|---|
| Heartbeat path: zero allocation, zero disk | 65 535 Nodes × 1 hb / 5s = 13k req/s; disk on hb path = death |
| Heartbeat path: never takes write-lock; just bumps `atomic.Int64 LastSeen` | writer-starvation under load |
| Per-record file (one YAML per UUID), not one mega-file | one register can't block another's heartbeat |
| Index protected by `sync.RWMutex` | readers (Query API) never block writers (registers) |
| Config: `atomic.Pointer[*Config]`, no mutex on read path | hot-reload swaps pointer; in-flight handlers finish on old config |
| Listeners: `SO_REUSEPORT` (Linux/macOS) for blue/green | two binaries co-listen during upgrade; Windows fallback = HA |
| WebSocket subscriptions: per-conn goroutine + `ctx.Done()` | dropping one WS doesn't affect others; reload doesn't kick subs |
| Drain-before-exit: SIGTERM → stop accepting → finish in-flight → snapshot → exit | no lost records on planned shutdown |
| Cold-start eviction: load YAMLs as `unknown-state`, demand heartbeat within 1× interval | prevents serving stale records after restart |

### 4.4 Persistence design

Per CLAUDE.md "Files only — no DB, no Redis":

| Path | What | Write trigger |
|---|---|---|
| `~/dhs/registry/nodes/{uuid}.yaml` | Node record | register / delete only — NEVER on heartbeat |
| `~/dhs/registry/devices/{uuid}.yaml` | Device record | register / delete |
| `~/dhs/registry/senders/{uuid}.yaml` | Sender record | register / delete |
| `~/dhs/registry/receivers/{uuid}.yaml` | Receiver record | register / delete |
| `~/dhs/registry/flows/{uuid}.yaml` | Flow record | register / delete |
| `~/dhs/registry/sources/{uuid}.yaml` | Source record | register / delete |
| `~/dhs/registry/index.bin` | optional — fast-startup index snapshot | periodic (5 min), on drain |
| (RAM-only) | heartbeat TTLs, WebSocket subscriptions, query subscriptions | every heartbeat (atomic) |

Atomic write pattern: write `.tmp` → `os.Rename()` (POSIX atomic on same FS).

Restart recovery: load every YAML into RAM index, mark records `unknown-state`, demand heartbeat within 1× heartbeat-interval before serving in Query API. Stale records evict.

### 4.5 HA / sharing design (Phase B-3)

Default = single instance, file store on local FS. HA is opt-in via flag — keeps "no setup" promise.

| Pattern | Setup cost | When to use |
|---|---|---|
| **Active-passive, shared FS** (NFS/SMB/Ceph mount) | mount required | small fleets, simplest |
| **Active-passive, lease via parent Registry** | none beyond having a parent | federations, multi-site |
| **Active-active CRDT** | OUT OF SCOPE | violates CLAUDE.md "no extra infrastructure" |

Active-passive shared-FS specifics:
- Both instances mount `~/dhs/registry/` from shared storage.
- Instances coordinate via lease file `~/dhs/registry/.lease` (atomic write of `{instance-id, expires-at}`).
- Active instance holds lease, advertises mDNS, accepts registrations.
- Passive watches lease; on expiry, takes lease, starts advertising.
- Records ARE shared via FS; **config is per-instance** (each reads its own `/etc/dhs/registry.yaml`) so a bad config blast-radius is one box.

Federation pattern (Phase B-4): two dhs-registry siblings both register as Nodes with a parent (Cerebrum). Parent picks one as authoritative for a region.

### 4.6 Operator surfaces (all hot)

```
dhs-registry serve --state-dir /var/lib/dhs-registry --config /etc/dhs/registry.yaml

# all hot — no restart, no dropped Nodes, no dropped subscriptions:
dhs-registry reload                          # re-read /etc/dhs/registry.yaml
dhs-registry node list                       # query in-memory index
dhs-registry node evict <uuid>               # admin force-evict (local op, not via spec)
dhs-registry version enable v1.0
dhs-registry version disable v1.0
dhs-registry tls reload
dhs-registry vendor-mode strict|lenient
dhs-registry parent set https://cerebrum.local/x-nmos/
dhs-registry parent unset

# only for binary upgrade — orchestrated via HA:
dhs-registry drain                           # stop advertising, finish in-flight, exit clean
                                             # passive partner takes over via lease
```

### 4.7 Vendor heterogeneity policy

Vendors won't all support every IS / BCP / Auth combination. The Registry must **never reject a spec-conformant-but-feature-light record**.

| Missing thing | What dhs-registry does |
|---|---|
| BCP-002 grouphint absent | Accept; fire `vendor_missing_bcp_002_grouphint` compliance event |
| BCP-004 caps absent | Accept; fire `vendor_missing_bcp_004_caps` event |
| BCP-008 audio/video caps absent | Accept; fire matching event |
| IS-10 OAuth2 token absent (Registry runs `--oauth-required=false`) | Accept; mark record `auth=none` |
| IS-10 OAuth2 token absent (Registry runs `--oauth-required=true`) | Reject 401; spec-compliant |
| Heartbeat interval out of spec range | Accept; fire `vendor_unexpected_heartbeat_interval` event |
| api_ver mismatch (vendor speaks v0.9 / v9.0) | Reject 400 (no spec coverage) |

Compliance events surface in `dhs-registry events tail` and in metrics (`dhs_registry_vendor_deviation_total`). Operator decides whether to enforce or report.

### 4.8 Dependencies — what dhs-registry pulls in

Stdlib-first, per CLAUDE.md. New deps if Phase B requires:

| Phase | New dep | Why |
|---|---|---|
| B-1 | `github.com/grandcat/zeroconf` (already in tree for Node) | mDNS advertise |
| B-1 | none new | file store, RWMutex, atomic — all stdlib |
| B-2 | `github.com/coreos/go-oidc/v3` (or stdlib JWT) | OAuth2 token validation |
| B-2 | `github.com/eclipse/paho.golang/paho` | MQTT client (broker integration) |
| B-3 | none new | shared-FS lease via os.Rename |
| B-4 | reuses existing IS-04 client code | federation = dhs Node code re-targeted at parent |

No Redis, no Postgres, no etcd. Ever.

---

## 5. Product / role choices that are not AMWA-published-minor skips

> **Reminder — Section 0 binds:** every AMWA-published minor in the
> `internal/amwa/CLAUDE.md` Versioning table is in scope. The items
> below are product and role decisions about *non-minor* surface area —
> not skips of any published wire version.

| Item | Why this is a product/role choice (not a minor skip) |
|---|---|
| dhs as **Controller** role (separate from Node + Registry) | a *role* choice, not a minor skip; Cerebrum + others fill this |
| Active-active Registry replication | an *implementation* choice; HA is shared-FS or federation, not replication |
| IS-04-02 suite running against an AMWA Mock Node | a *test-shape* choice; Mock Node is for testing other Registries, dhs IS the Registry |

### Audit follow-ups (Versioning-table additions)

These are AMWA-published specs that are NOT currently in the
`internal/amwa/CLAUDE.md` Versioning table and need an explicit
in-scope decision (default: in scope per Section 0 unless they're
AMWA-WIP-without-stable-release):

| Spec | AMWA stable? | Action |
|---|---|---|
| BCP-003-01 Secure Communication (TLS) | yes — published | already implicitly in scope via Track B Phase B-2; add to Versioning table |
| BCP-003-02 Authorization | yes — published | already in scope via B-2; add to table |
| BCP-003-03 Certificate Provisioning | yes — published | add to table; implement when reaching B-2/B-5 |
| IS-10 Authorization | yes — published | already implicitly in scope; add to table |
| MS-04 Privacy Encryption Protocol | nascent (AMWA-WIP) | legitimate "land when stable" carve-out per CLAUDE.md WIP list |
| IS-13 Annotation | nascent (AMWA-WIP) | legitimate carve-out |
| BCP-006-02 H.264, BCP-006-03 H.265, BCP-007-01 NDI | nascent (AMWA-WIP) | legitimate carve-outs |

---

## 6. Open questions / decisions pending

| # | Question | Owner | Default if no decision |
|---|---|---|---|
| 1 | Track B — open epic now or after Track A merges? | user | open now in parallel |
| 2 | Registry product name: `dhs-registry` (separate binary) vs `dhs registry serve` (subcommand)? | user | subcommand (one binary) |
| 3 | Sell as binary download, container image, or both? | user | both |
| 4 | License model for the Registry product (open source, source-available, commercial)? | user | TBD |
| 5 | HA in B-3: ship shared-FS only, or shared-FS + federation? | user | shared-FS for B-3, federation in B-4 |
| 6 | Vendor leniency: ship `lenient` as default or `strict` as default? | user | `lenient` default; `strict` opt-in |

---

## 7. Glossary

- **Node** — IS-04 producer of resources (Senders, Receivers, Flows, Sources, Devices). dhs ships this.
- **Registry** — IS-04 record store + Query API for Controllers. Track B is dhs's first Registry implementation.
- **Controller** — IS-04 consumer that queries the Registry and orchestrates senders/receivers via IS-05. Cerebrum is one. dhs does not ship one.
- **Federation** — dhs Registry registers itself UP to a parent Registry, propagating records.
- **Vendor leniency** — Registry mode that accepts records missing optional BCP fields, firing compliance events instead of rejecting.
- **Hot-swap** — config / cert / version state changes at runtime without dropping any Node session or Controller subscription.
