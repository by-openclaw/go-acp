# NMOS — EVS Cerebrum interop notes

The EVS Cerebrum control system ships an **IS-04 Registry implementation**
as one of its NMOS surfaces. This doc captures what we know about how
that registry behaves, derived from the vendor-supplied
[`NMOS IS-04-5 Help.pdf`](NMOS%20IS-04-5%20Help.pdf) shipped alongside
this file.

> **Scope.** This doc is about Cerebrum's NMOS Registry surface
> (HTTP, port 8080) only. It is unrelated to Cerebrum's Northbound
> XML/WebSocket API on port 40007 — those are two independent
> product surfaces with no shared discovery, transport, or session
> state. NB lives in `internal/cerebrum-nb/`; this doc never touches it.

> **Source.** `NMOS IS-04-5 Help.pdf` (8 pages), shipped by EVS with
> Cerebrum. Verified: 2026-04-29 (documentation review — no live
> Cerebrum-NMOS testbed yet). Live verification at RTBF pending.

---

## 1. Why this doc

[`matrix-compliance.md`](matrix-compliance.md) already tracks Lawo VSM as
a **Node-only** vendor (no Registry, no Query API, IS-05 over HTTP only).
Cerebrum is the **inverse** — it ships a full IS-04 Registry
implementation and runs as the catalogue middleware between Nodes and
Controllers. Cerebrum is the most likely first NMOS Registry peer dhs
will meet at RTBF, so its deviations need to be classified before
integration starts.

Three jobs:

1. Pin down the three NMOS deployment modes Cerebrum supports, so we
   know which dhs CLI surfaces have to interop with which Cerebrum
   surface.
2. Surface a previously-unrepresented deployment mode in our schema
   (**Mode D — mDNS direct-Node, no Registry**), required by Cerebrum's
   peer-to-peer mode.
3. Catalogue Cerebrum's documented spec deviations as compliance events
   we have to fire when peering with it.

---

## 2. Cerebrum's three NMOS modes

| # | Cerebrum mode | Cerebrum role | Discovery | Registry |
|---|---|---|---|---|
| 1 | **External Registry** | Node + Controller talking to someone else's IS-04 Registry | mDNS or unicast hint | external (3rd-party) |
| 2 | **Peer-to-peer** | Nodes only; no central catalogue | mDNS (mandatory — multicast must traverse) | none |
| 3 | **Hosted Registry** ("Network Media Server" device) | Cerebrum **is** the IS-04 Registry | Cerebrum advertises via Bonjour | Cerebrum |

ASCII view:

```
Mode 1 — Cerebrum connects to someone else's Registry
  Cerebrum ── HTTP ──> 3rd-party Registry <── HTTP ── 3rd-party Nodes

Mode 2 — Peer-to-peer
  Cerebrum <── mDNS ──> 3rd-party Nodes
                       (no Registry — direct Node-API walking only)

Mode 3 — Cerebrum hosts the Registry
  3rd-party Nodes ── HTTP ──> Cerebrum:8080 (Registry) <── HTTP ── 3rd-party Controllers
```

---

## 3. Mapping to dhs deployment modes

[`matrix-compliance.md`](matrix-compliance.md) "Deployment modes"
defines three today: **A** (full mDNS + Registry), **B** (unicast
Registry), **C** (direct-Node, no mDNS, no Registry). Cerebrum's mode 2
("peer-to-peer with mDNS but no Registry") doesn't fit any of them —
mDNS is mandatory but Registry is absent. We need a new **Mode D**.

| Cerebrum mode | dhs equivalent | Notes |
|---|---|---|
| 1. External Registry | **A** (`--mdns`) or **B** (`--no-mdns --registry`) — we're a Node and/or Controller; Cerebrum is too | Both peers register against a third-party Registry |
| 2. Peer-to-peer | **D** (NEW) — `mDNS direct-Node`, no Registry | Required for Cerebrum P2P; not yet wired in CLI |
| 3. Hosted Registry | **A** or **B** — we're a Node and/or Controller; Cerebrum is the Registry | dhs `producer nmos serve` + `consumer nmos walk` against Cerebrum:8080 |

**Mode D — new deployment mode:**

| Property | Value |
|---|---|
| Discovery | mDNS-SD on `_nmos-node._tcp` (peer service type, not `_nmos-register._tcp`) |
| Registry | none — direct-Node walking only |
| Heartbeats | N/A (no Registry to heartbeat against) |
| Producer flags | `dhs producer nmos serve --mdns --no-registry` |
| Consumer flags | `dhs consumer nmos walk-nodes --mdns --no-registry` |
| Fallback | once mDNS resolves a peer set, the rest of the flow degrades into direct-Node walking |

**Principle:** dhs supports **every** deployment topology a peer might
require. AMWA NMOS specifications (IS-04 §3 in particular) remain the
authoritative source of truth — Mode D is not a spec deviation, it
just names a `(mDNS-on, Registry-absent)` combination AMWA already
permits. Canonical references for Mode D:
[`architecture.md`](architecture.md) §"Mode D" + ASCII,
[`matrix-compliance.md`](matrix-compliance.md) "Deployment modes"
table, [`internal/amwa/CLAUDE.md`](../CLAUDE.md) Quirks #1.

---

## 4. Cerebrum's hosted Registry — implementation details (peer side)

These are facts about **Cerebrum's** environment, not about dhs. We
surface them so integration teams know what their Cerebrum host looks
like before we connect.

| Aspect | Cerebrum behaviour |
|---|---|
| Default port | 8080 (HTTP, no TLS in default config) |
| Host OS observed | Windows Server 2022 Datacenter |
| mDNS lib | Bonjour for Windows (Apple installer required on the Cerebrum host) |
| `_nmos-system._tcp.local` PTR cadence | every 10–20 s |
| Multicast group | 224.0.0.251 (UDP/5353) |
| Supported endpoints | `GET /x-nmos/registration/`, `GET /x-nmos/registration/v1.x/`, `POST /x-nmos/registration/v1.x/resource`, `DELETE /x-nmos/registration/v1.x/resource/{type}/{id}`, `POST /x-nmos/registration/v1.x/health/nodes/{id}` |
| Heartbeat / GC | 5 s default / 12 s default; configurable up to **30 000 ms** (hard ceiling — see §7.2) |
| Required Win prereq | `netsh http add urlacl url=http://*:8080/x-nmos/registration/ user=EVERYONE listen=yes delegate=yes` (and the same for `/query/`) — on the Cerebrum host, run as Administrator |
| Firewall | Inbound TCP 8080 must be open on the Cerebrum host |
| Bonjour failure mode | Registry surfaces error `-65540` → restart Bonjour service on the Cerebrum host |

> **dhs is OS-agnostic.** Our IS-04 Registry implementation uses the Go
> standard library (no Bonjour service, no `netsh`, no Windows-specific
> dependency); it runs on Linux, macOS, Windows. The list above
> documents Cerebrum's environment so field teams know what to install
> on the Cerebrum host, not what dhs requires on its own host.

---

## 5. Spec deviations — compliance events to fire when peering with Cerebrum

Each of these matches the "absorb-and-fire" pattern from
[`matrix-compliance.md`](matrix-compliance.md). Fire at most once per
(session, peer, deviation) tuple.

| Deviation | Cerebrum behaviour | dhs response | Compliance event |
|---|---|---|---|
| Query API absent | Vendor PDF: *"external third party control systems cannot interrogate the registry directly"*. Only Registration API endpoints served. | If we're the Controller, fall back to direct-Node walking against Cerebrum's known Nodes. | `nmos_query_api_missing` (existing) |
| IS-05 activation always-immediate | Single PATCH; activation always coerced to `now`. | Detect rejection of `activate_scheduled_*`; retry as `activate_immediate`. | `nmos_scheduled_activation_unsupported` (existing — applies to Cerebrum too) |
| IS-05 single-PATCH expectation | Cerebrum waits for HTTP 200 OK before next PATCH on same device. | Don't pipeline IS-05 PATCHes when peer is Cerebrum; serialise per-device. | `nmos_is05_serial_patch_required` (NEW) |
| SDP legs truncated | Cerebrum reads only the first 2 leg/stream definitions in `m=` blocks (vendor PDF: *"Cerebrum currently only use the first two definitions of legs/streams"*). | dhs encoder/decoder supports unlimited legs. When our Sender has >2 legs and peer is Cerebrum, fire event so operator knows extra legs were silently dropped peer-side. | `nmos_sdp_legs_truncated_peer` (NEW) |
| Sender identity tuple uniqueness | Cerebrum requires (m/c addr, origin addr, port) globally unique across senders; duplicates are highlighted red in their UI but not rejected. | Detect duplicates ourselves before announcing — Cerebrum will mis-route otherwise. | `nmos_sender_identity_collision` (NEW) |
| `a=group:DUP primary secondary` expected for redundant flows | Cerebrum expects this exact spelling; if absent, the second leg is treated as a separate flow. | dhs encoder emits the canonical form already; integration tests must verify the line is preserved end-to-end. | n/a (we already comply — no event) |

Naming convention: `nmos_` prefix; new events land in the NMOS
compliance catalogue when the codec-side compliance package is wired
(see `internal/amwa/CLAUDE.md` "Strict-dependency architecture").

---

## 6. Sub-device pattern (SubID) — TODO verify against Cerebrum tech docs

Cerebrum's IS-04 Registry has a **SubID** concept layered on top of NMOS
UUIDs. The vendor PDF describes two referencing modes:

| Mode | How a sender/receiver is addressed | Hardware-swap behaviour |
|---|---|---|
| **Without SubID** | Full UUID path: `<node UUID>\|<device UUID>\|<sender or receiver UUID>` | All UUIDs change post-swap → every route reconfigured manually |
| **With SubID** | `<SubID>.<sender or receiver label>` | SubID-to-Node binding updates once; route configurations referencing SubID + label survive |

Concrete walkthrough (paraphrased from PDF — see TODO below):

```
Pre-swap:
  Node X (uuid=A1, label="CAM-01-room-3", SubID="cam-01")
    Sender (uuid=S1, label="SDI-OUT-1")

  Cerebrum route references:  cam-01.SDI-OUT-1   ✓

Hardware swap (different MAC, different UUIDs):
  Node X' (uuid=B7, label="CAM-01-room-3", SubID rebound to "cam-01")
    Sender (uuid=S9, label="SDI-OUT-1")

  Cerebrum route references:  cam-01.SDI-OUT-1   ✓ still valid
```

**Anti-pattern called out by name in the vendor PDF:** EVS XT Servers
include the unit's serial number in sender/receiver labels. After
hardware swap, the **labels** change too, breaking the
SubID-as-stable-key promise. Cerebrum's documented workaround: deploy XT
Servers **without** SubID assignment, accepting full-UUID-path
referencing instead.

> **TODO — verify against Cerebrum tech docs.** The SubID story above
> is a paraphrase of one paragraph in `NMOS IS-04-5 Help.pdf`. Before
> relying on SubID semantics in integration tests:
>
> 1. Pull canonical Cerebrum technical documentation via vendor support
>    contact (not the help PDF, which is operator-facing).
> 2. Verify: does SubID survive a node UUID change without operator
>    action? What's the failure mode if two replaced nodes claim the
>    same SubID? What happens during the swap window when both old and
>    new node could be heartbeating?
> 3. Confirm the EVS XT label-instability anti-pattern is the only
>    documented case, or whether other peers exhibit similar
>    label-mutation behaviour.

---

## 7. Warnings + open question

### 7.1 Open question — Cerebrum's missing Query API

Cerebrum's IS-04 Registry deliberately does not expose `/x-nmos/query/*`.
The vendor PDF states this as a fact, not a roadmap item. Open: is this
a deliberate product choice (Cerebrum routes everything through its own
UI) or a planned-but-unimplemented surface?

If deliberate: any third-party Controller (dhs, Lawo, anyone) peering
with Cerebrum-as-Registry must walk each Node directly. Mode A (full
mDNS + Registry) effectively degrades to Mode B-with-mDNS-discovery
against a Cerebrum peer — the Registry's discovery face is usable, the
catalogue face is not.

### 7.2 Warning — `30 000 ms` is a hard ceiling on Cerebrum's GC timeout

The "Timeout" parameter in Cerebrum's Advanced Comms dialog is capped
at 30 000 ms. dhs Nodes peering with a Cerebrum Registry must:

- Send heartbeat at ≤ ~6 s (matches default 5 s with margin).
- **Never** request a Registry-side GC timeout > 30 000 ms — Cerebrum
  will silently clamp to 30 000 ms, which makes the dhs-side keep-alive
  cadence calculation incorrect.
- Treat 30 000 ms as the hard maximum tolerated outage window for a
  Node peering with Cerebrum.

### 7.3 Warning — Cerebrum's internal datastore XML is not a public format

Cerebrum stores Registry state in
`..\Data Files\Generic Device Data\<ip>.xml` on the Cerebrum host. This
is an internal datastore format. It exists; dhs does **not** parse,
generate, or synchronise it.

Don't:

- Confuse this with Cerebrum's Northbound XML wire protocol (port
  40007) — different product surface, different XML schema, no
  relationship to NMOS.
- Read or write the file from dhs as a side-channel.
- Treat the file shape as stable across Cerebrum versions.

If RTBF needs Registry-state replication outside Cerebrum's own SQL or
Virtual-IP mechanism, we provide it through dhs primitives (Mode D peer
sync, Mode B unicast Registry replication once HA lands), not by
touching this file.

---

## Cross-references

- [`internal/amwa/CLAUDE.md`](../CLAUDE.md) — NMOS plugin top-level
  context. Quirks #1 enumerates deployment modes A/B/C/D.
- [`matrix-compliance.md`](matrix-compliance.md) — vendor-by-vendor
  compliance tracker. Cerebrum row to be added once a live testbed
  exists.
- [`architecture.md`](architecture.md) — IS-04 / IS-05 role topology
  diagrams.
- [`dns-sd-unbound.md`](dns-sd-unbound.md) — Mode B unicast DNS-SD
  recipe for pfSense Unbound, including the `cerebrum` instance live-
  verified 2026-04-30.
- [`NMOS IS-04-5 Help.pdf`](NMOS%20IS-04-5%20Help.pdf) — vendor source
  for everything in this doc.

---

## NMOS Phase A live test — observed 2026-05-01

dhs Node bundle (Node + Device + audio Source + audio L24 Flow + RTP
Sender + RTP Receiver) registered against Cerebrum via
`bin/dhs producer nmos serve --no-mdns --registry http://10.100.0.5:8080
--bind 0.0.0.0:18080 --advertise-host 10.6.239.113:18080
--config tests/fixtures/nmos/cerebrum-test-node.json --api-ver v1.3`.
All 6 POSTs accepted; resources visible in Cerebrum's UI with all
linkages correct. Subscription-bug fix in PR #187 lands the `receiver_id`
/ `sender_id` field correctly on the wire.

Three remaining mismatches between what dhs sends and what Cerebrum
serves back via Query API:

| Field | dhs wire (verified vs spec) | Cerebrum Query API echo | Spec rule |
|---|---|---|---|
| `sender.version` | `"1777651501:0"` (TAI) | `""` | required, `^[0-9]+:[0-9]+$` (resource_core.json) |
| `receiver.version` | `"1777651501:0"` | `""` | same as above |
| `flow.sample_rate` | `{"numerator":48000,"denominator":1}` | field dropped | required for audio raw flows (flow_audio_raw.json `required`) |
| `sender.subscription.active` | `false` | `true` (auto-flipped) | spec value reflects "actively delivering"; dhs Sender is not |

Pattern: `flow.version`, `source.version`, `device.version`,
`node.version` round-trip correctly; only sender + receiver versions
are blanked. `sample_rate` drop is specific to audio raw flows.
Active-flip happens on Sender only.

Bonus deviation: Cerebrum's UI mis-renders UTF-8 em-dash `—` as
Latin-1 `â€"` — the wire bytes are spec-correct UTF-8; only the
display layer is broken. Workaround: keep dhs labels/descriptions
ASCII when testing.

### Device updates MERGE controls — observed live 2026-08-28

IS-04 registration updates replace the whole resource document.
Cerebrum's hosted Registry instead **unions `device.controls` across
updates**: after a dhs Node re-registered with corrected control
hrefs, Cerebrum's Query API served BOTH the fresh controls and the
stale ones from the previous registration — six `sr-ctrl` entries for
a device whose own Node API document carried exactly three. Proof
method: fetch the device from the Node API and from Cerebrum's Query
API and diff `controls[]`; the Node-side document is what dhs actually
POSTed.

Operational consequences:

- A Node that ever registered a bad control href cannot fix it by
  re-registering — the stale entry persists until the operator clicks
  **Forget** on the node in Cerebrum's NMOS Nodes table (the node
  re-registers clean within a heartbeat cycle).
- Controllers reading Cerebrum's catalogue must expect duplicate
  control types and pick the one matching the node's current
  `api.endpoints` — or better, the Node API document.
- dhs side: `provider/connection_mount.go upsertControl` now REPLACES
  same-type controls at attach time, so a dhs Node never publishes a
  stale-vs-fresh pair itself, whatever a bundle file carried.

Compliance event `nmos_registry_update_merged_peer` is the candidate
name once the controller-side read-back comparison is wired; until
then this note is the tracking artifact.

### Cerebrum CONSUMING the dhs Registry — verified live 2026-08-28

The inverse direction works, and answers §7.1's open question in
practice: a Cerebrum **Network Media** device (its External-Registry
client mode) discovered `dhs-nmos-registry` via Bonjour, selected it,
and consumed the catalogue as a full controller — seven concurrent
HTTP connections and **six IS-04 v1.3 WebSocket subscriptions**, one
per resource type (`/nodes /devices /sources /flows /senders
/receivers`). A real EVS Neuron registered in the dhs Registry
(208 senders/receivers) reached Cerebrum's UI entirely through us:
Neuron → dhs Registration API → dhs Query-WS → Cerebrum.

Setup notes that cost time, for the next person:

- Device Type **Network Media** = external-registry client;
  **Network Media Server** = Cerebrum hosting its own registry. The
  client never fires while a hosted-registry device at `pri=0`
  outranks the external one — disable the hosted device, or announce
  the external registry at `pri=0`.
- The dialog's *Primary IP Address* is the Cerebrum host's OWN
  interface (a subnet selector), never the registry's address; the
  registry's host:port arrive via the Bonjour SRV record.
- Cerebrum subscribes at v1.3 only, one subscription per resource
  type, non-persistent.
- **Query pagination (measured 2026-08-28, post vendor release)**: the
  default page size is STILL 10, but the release fixes the pagination
  *machinery* — no-param collection GETs now carry proper RFC 5988
  `Link: rel="next"/"prev"` plus `X-Paging-Limit/Since/Until` headers,
  so a conformant client walks the full set. A client that ignores
  `Link` still silently sees only the first 10 of everything; the dhs
  controller follows `Link` and is unaffected.
- **First-page-only reads.** The Network Media client fetches page one
  of each Query collection and never follows the `Link: rel="next"`
  cursors (packet-traced 2026-08-28: zero paging-parameter requests).
  Against a registry whose default page is 100 newest-first, a plant
  larger than one page silently loses everything registered before
  the newest device — on ours, one Neuron's 208 resources crowded the
  dhs node entirely out of Cerebrum's view, flipping with
  registration order. Remedy on the dhs registry:
  `dhs registry nmos serve --page-limit-default 1000` (spec-legal —
  the no-param page size is implementation-defined; explicit client
  limits still win, conformance default untouched at 100).
- **ROOT-CAUSE CORRECTION (2026-08-28, evening).** The two findings
  below — "delete + re-add serves cache" and "no auto-reconnect" —
  were measured while the dhs registry's OWN mDNS announce was
  defective: Avahi also published the registry's A-record on loopback,
  so a Bonjour client resolving the SRV target could receive
  `127.0.0.1`, connect to itself, and look permanently dead with no
  error. The instant loopback was removed from the announce
  (`deny-interfaces=lo` on the registry host), Cerebrum connected and
  rendered the full catalogue WITHOUT any operator action — validating
  the operator's statement that "Cerebrum connects if a registry
  exists". Cerebrum's cache-rendering and silent-failure UX made the
  dhs defect invisible for hours, but the defect was ours. The entries
  below remain as observed behaviour of the client under a poisoned
  announce; treat their "never redials" claims as UNPROVEN against a
  healthy announce. dhs TODO: exclude loopback at the announce layer
  in code, not just host config.

  Refresh model measured against the HEALTHY announce (45 min of
  cumulative packet traces): Cerebrum read the catalogue in ONE burst
  right after the announce became clean, rendered it, and issued zero
  further requests — no periodic polling at all. Catalogue changes
  after its burst (a node registered minutes later) do not appear
  until its next event-driven read. In the earlier session the client
  had opened six Query-WS subscriptions (live updates); this device
  instance opened none — the difference is presumably the device's
  "Resource Query" option (see the device-panel knob table below).
  Without WS subscriptions, treat Cerebrum's view as a snapshot taken
  at connect time.

  FINAL MODEL (Servers tab, confirmed by screenshot + wire
  2026-08-28): the Network Media device keeps a **Query Servers list**
  (every discovered registry, each with address/port/versions and an
  **Active checkbox**) and a separate **Node Servers list** (nodes
  harvested directly from `_nmos-node` adverts — a Mode-D side channel
  that can deliver a node's details independently of any registry,
  which explains "full details" sightings while registry traffic was
  zero). THE attach lever is the Active checkbox: only the checked
  Query Server is consumed. Flipping Active to the dhs entry produced
  7 connections and all six Query-WS subscriptions within ~3 minutes —
  full live-update mode. Operational rule: after any registry change,
  verify which entry is Active before debugging anything else.
- **Delete + re-add serves cache, not network.** Measured 2026-08-28:
  across a full delete → re-add → nudge sequence of the Network Media
  device, an attach watcher saw zero TCP connections in 20 minutes and
  a 12-minute packet capture recorded zero HTTP requests — yet the
  "fresh" device rendered a node with 208 senders. The new device
  instance re-binds to the server-wide per-UUID cache (the Generic
  Device Data store) and displays it as live, including a "Fully
  Connected" event that refers to its own comms layer. Once the client
  has stopped dialling, no device-level action restores network reads;
  the remaining levers are a Cerebrum server-service restart or vendor
  support. Plan external-registry maintenance around this.
- **Auto-reconnect: RETIRED finding (was: "no auto-reconnect").** The
  original ~1 h-disconnected measurement was taken while our announce
  was loopback-poisoned (see root-cause section) — Cerebrum was
  retrying against 127.0.0.1. Re-measured 2026-08-28 with a clean
  announce: after a full Cerebrum server restart the Network Media
  client re-dialled the dhs registry **unaided** (conns 0 → 7 → 6
  with all six Query-WS subscriptions, ~17 min after the restart
  began) and did it again after the 2.8.17 upgrade reboot. Restart
  maintenance needs no manual nudge; only the poisoned-announce
  scenario ever did.

### Verification status

dhs codec is byte-exact-correct against the AMWA IS-04 v1.3 schemas
on every wire field listed above. To attribute the remaining
mismatches we require third-party evidence —
running the AMWA NMOS Testing tool's Mock Registry locally and
comparing the same 3 fields is the next step. If AMWA Mock Registry
also blanks `sender.version` etc., the bug is on dhs's side and we
revisit the codec. If it preserves them, Cerebrum is the deviator
and these become tracked compliance events.

Until then, status of the affected codec paths in the integration
plan stays **yellow** (codec landed, real-peer evidence ambiguous).

### Cerebrum 2.8.17 upgrade — measured 2026-08-28

The plant upgraded Cerebrum 2.8.11 → 2.8.17 (release notes claim NMOS
fixes, including the pagination bug). Same day, same registry process,
same store (dhs-test-node + Neuron `bm-n-nnbrg-c01`, 211 senders):

| Behaviour | 2.8.11 | 2.8.17 |
|---|---|---|
| Auto re-attach after restart | yes (measured, clean announce) | yes (6 conns + 6 WS subs, unaided) |
| Detail panes | dhs rendered; Neuron flipped per-attach (its triple-homed controls) | **blank for BOTH nodes** |
| Manual/forced IS-05 | dialled node Connection API | **nothing — zero connections to any Node API** |

dhs-side elimination, all measured while the panes were blank:

- Store correct: both nodes present, dhs controls all at
  `http://10.100.0.101:18080/...` (reachable; `GET /self` = 200).
- Registry flags active: `--page-limit-default 1000` (whole store fits
  page 1) `--priority 0` `--advertise-host 10.100.0.101:8235`.
- WS bootstrap proven end-to-end: replaying Cerebrum's exact attach
  from another host (POST non-persistent `/senders` subscription +
  WS connect) delivered **211/211 sender sync grains** cross-wire.
- `ss` on the registry host: Cerebrum holds exactly the 6 Query-WS
  connections and dials **no** Node API on any host (not ours on
  :18080, not the Neuron on :3000).

Resolution, ~40 min after attach: the panes populated on their own —
dhs node **full details + SDP**, with a live Cerebrum connection to
our node's :18080 visible in `ss`. So 2.8.17's behaviour is *delayed*
catalogue rendering after an attach (tens of minutes with zero Node
API dials, then normal reads), not a permanent regression. Operator
rule: after attach or upgrade, wait before debugging blank panes.

The Neuron pane stayed blank through the same window — consistent
with the pre-existing per-attach coin-flip on its triple-homed
controls. Precision (2026-08-28, after operator correction): the two
non-mgmt hrefs (`10.6.40.51` / `10.7.40.51`) are addresses of the
Neuron's media interfaces whose networks DO NOT EXIST in this lab —
nothing to route, no port to open; the device advertises its
interface config, not reachable endpoints. 2.8.17 does not change
that behaviour. Both items remain EVS ticket material: the
multi-minute render delay, and control-href selection that ignores
reachability (compounded by the Neuron advertising unconnected
interfaces).

### Registry-to-registry bridge — proven live 2026-08-29

Use case: `dhs Node ↔ dhs Registry ↔ [Cerebrum hosted Registry ↔
Cerebrum controller]` — each side reads its native catalogue, dhs is
the authoritative middle. Operational (script) version ran live:
snapshot our Query API, proxy-register every resource into Cerebrum's
Registration API v1.3 in dependency order (node → device → source →
flow → sender → receiver; 856/856 accepted with 201), then proxy one
`POST /health` per node every 4 s. Verified durable past their 12 s
GC; their query then serves our full catalogue including
`manifest_href` intact, and their controller renders it.

**Trap that cost the first attempt — HTTP 411 on heartbeats.**
Cerebrum's hosted registry rejects bodyless `POST /health/...`
without a `Content-Length` header (`411 Length Required` from its
HTTP layer). curl's empty POST omits the header; every heartbeat
bounced, their GC silently swept all 856 resources ~12 s after the
fill, and later re-POSTs failed `400 "device is not known"` (their
registration validates parentage). Fix: send an explicit empty body
(`curl -d ""`, or any client that sets `Content-Length: 0`). EVS
ticket item #5: heartbeat rejection is invisible to the operator —
the fill "succeeds" and the catalogue quietly evaporates.

Their registration face also validates parent references (sender
without known device → 400), so fill order is mandatory, and a full
node re-fill is required after any eviction.

Productization of this bridge (the `dhs registry nmos mirror` verb:
Query-WS-driven forwarding, deletion propagation, per-node heartbeat
proxying) is designed and awaits approval post-merge.

### Cerebrum device-panel knobs that affect interop (per
"Modify Device" UI screenshot 2026-05-01)

| Setting | Behaviour | dhs impact |
|---|---|---|
| Device Category: NMOS IS-04/5 | co-hosted IS-04 + IS-05 | both APIs target the same host:port |
| Fixed Server Version: (none) | "highest supported used" | confirms our multi-version `api_ver=v1.1,v1.2,v1.3` strategy is right |
| Send Empty (not null) SDP for Disconnect: ☐ | disconnect uses `transport_file.data: null` (spec) | our IS-05 codec already pointer-typed → renders null correctly |
| Ignore (Force) Active Receivers Master Enable: ☐ | honour `master_enable` gating | standard IS-05 behaviour |
| Ignore Sender Origin Line Changes: ☑ | tolerates static SDP `o=` | less strict than RFC 4566; we keep correct behaviour |
| Pre-defined IP Senders/Receivers ☑ | auto-creates placeholder Sender/Receiver | when our Node registers, Cerebrum may inject extras; dhs walk must filter on our UUIDs not fail on extras |
| Resource Query: ☑, **WebSocket Server Port: 8089** | IS-04 Query WS subs on `:8089` (not `:8080`) | when seq 6 `watch` verb tests run, target `ws://10.100.0.5:8089/`, not `:8080` |
| Return HTML formatted Resources: ☐ | pure JSON responses | our `application/json` decoder is happy |
