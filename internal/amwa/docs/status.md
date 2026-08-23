# AMWA NMOS — implementation status

**Single source of truth for where the NMOS plugin actually stands.** Verified
against the tree on 2026-08-24, not from memory. Version targets are set by
[`../CLAUDE.md`](../CLAUDE.md) §Versioning; this file records what is *built*
against them.

Binding rule from `../CLAUDE.md`: every minor AMWA has published is in scope.
A minor not implemented today is **missing**, never "deferred by design".

---

## 1. Coverage matrix — spec × role

Legend: **✅ done** · **🔵 in review** (unit written, PR open, not merged) ·
**🟡 codec only** (types + validation exist, nothing on the wire uses them) ·
**⬜ not started**

| Spec | Versions built | Node (device) | Registry | Controller |
|---|---|---|---|---|
| **IS-04** Discovery & Registration | v1.0 v1.1 v1.2 v1.3 | ✅ Node API + registration client + heartbeat + failover | ✅ Registration + Query + WS subscriptions + paging + GC | ✅ discover → resolve → walk all six collections; 🔵 WS watcher (#833) |
| **IS-05** Connection Management | v1.0 v1.1 | ⬜ **no `/x-nmos/connection/` route served** (#841) | n/a | 🔵 client landed (#834); `connect` verb not yet built |
| **IS-07** Event & Tally | v1.0 | ✅ WS publisher | n/a | ✅ WS subscriber |
| **IS-08** Channel Mapping | v1.0 | ⬜ not served | n/a | ⬜ no client |
| **IS-09** System API | v1.0 | ✅ server + `_nmos-system._tcp` announce | n/a | ✅ discover → select by `pri` → fetch → validate |
| **IS-12** Control Protocol | v1.0 | ⬜ no control endpoint | n/a | ⬜ no client |
| **MS-05-01 / -02** Control Framework | v1.0 | 🟡 datatypes + class descriptors | n/a | 🟡 |
| **BCP-002-01 / -02** Grouping & Asset | v1.0 | 🟡 validators exist, **never invoked** | 🟡 | 🔵 asserted by `audit` (#836) |
| **BCP-004-01 / -02** Receiver Caps | v1.0 | 🟡 same | 🟡 | 🔵 asserted by `audit` (#836) |
| **BCP-006-01 / -04** JPEG XS / MPEG TS | v1.0 | 🟡 same | 🟡 | 🟡 |
| **BCP-008-01 / -02** Status Monitoring | v1.0 | 🟡 same — **and blocked**: BCP-008 is built on MS-05 status classes over IS-12, so it needs the control endpoint first | 🟡 | 🟡 |
| **SDP** (RFC 4566, ST 2110 profile) | n/a | 🟡 parser landed (#828), not yet wired | 🟡 | 🟡 |

### Not in the tree at all

`IS-06` network control · `IS-10` authorization · `IS-11` stream compatibility ·
`IS-14` device configuration · `BCP-003-01` TLS · `BCP-005-01` EDID ·
`BCP-007-03`. AMWA ships test suites for these, so per the binding rule they
are **missing implementations**, not scope decisions —
see [`amwa-reality-check-2026-08-23.md`](amwa-reality-check-2026-08-23.md).

Genuinely WIP at AMWA, land when stable: IS-13, BCP-006-02 (H.264),
BCP-006-03 (H.265), BCP-007-01 (NDI).

---

## 2. The gap that matters most

**No IS-05 server means no routing.** A controller can discover our node, read
its senders and receivers, and cannot connect them. Our own audit reports it
against us, as CRITICAL:

```
NMOS-IS05-ABSENT   node publishes 1 sender(s) but serves no /x-nmos/connection API
                     -> the node is read-only to any controller
```

Tracked as #841. For a routing product that is *the* gap; the missing minor
specs are secondary.

Second: **the BCP validators are written, unit-tested, registered — and called
by nothing.** `bcp.Run` has no caller outside the codec tree, so no compliance
event is ever raised from a serve or walk path. The `audit` verb asserts the
same rules independently, which closes the operator-facing hole but not the
serve-path one.

---

## 3. Discovery — mDNS, unicast DNS-SD, and manual, per role

The three roles do **not** use discovery the same way. Who browses, who
advertises, and what happens when multicast is blocked differs per role, and
conflating them is the most common source of "it works in the lab" failures.

### What each role does

| Role | Advertises | Browses for | Falls back to |
|---|---|---|---|
| **Node** | `_nmos-node._tcp` — **only while unregistered**. Once it registers, it suspends the announce (IS-04 §3, and correct behaviour, not a fault) | `_nmos-register._tcp` **and** `_nmos-registration._tcp` (the legacy name, v1.0/v1.1) — plus `_nmos-system._tcp` for IS-09 bootstrap | `--registry <url>` (unicast, manual) |
| **Registry** | `_nmos-register._tcp` + `_nmos-query._tcp`, with `pri` in TXT | nothing | `--advertise-host` for the address it publishes |
| **Controller** | nothing | `_nmos-query._tcp` to find registries; `_nmos-node._tcp` for peer-to-peer plants | `--registry <url>` or `--peer-list <csv>` |

**Both registration service names must be browsed concurrently.** IS-04 v1.2
renamed `_nmos-registration._tcp` to `_nmos-register._tcp`. A node browsing only
the modern name never sees a v1.0/v1.1 registry on the same link. Constants:
`codec/dnssd.ServiceRegister` and `ServiceRegisterLegacy`.

### The four deployment modes

All four are spec-compliant. They name `(mDNS, Registry)` combinations, nothing
more — IS-04 §3 remains the source of truth.

| Mode | Flags | When |
|---|---|---|
| **A — mDNS + Registry** | default | greenfield, multicast allowed |
| **B — Unicast Registry** | `--no-mdns --registry <ip>:<port>` | multicast blocked by policy (common in broadcast plants) |
| **C — Direct-Node** | `--no-mdns --no-registry --peer-list peers.csv` | no registry exists at all — Lawo VSM has none |
| **D — mDNS direct-Node** | `--mdns --no-registry` | EVS Cerebrum peer-to-peer |

Unicast DNS-SD (Mode B) is *not* "manual": it is real DNS-SD over unicast DNS,
`--unicast --resolver <dns>`, resolving the same `_nmos-*._tcp` records from a
zone. Manual is Mode C — a static CSV, no discovery protocol at all.

### Which backend actually runs

Chosen at process start and logged at INFO. This matters because the choice
changes conformance, not just performance:

| Host | Backend | Today |
|---|---|---|
| Linux + avahi-daemon | Avahi via DBus, pure-Go | ✅ |
| Windows + Bonjour service | `dnssd.dll` via CGo | ⬜ **#195 — falls back to stdlib, so an installed Bonjour service is not used** |
| macOS | libSystem via CGo | ⬜ #196 — same |
| anything else | pure-Go stdlib multicast | ✅ never removed; the floor |

The stdlib path has a 500 ms read-deadline jitter, which degrades the AMWA
cascade-timing tests (test_05/15/16). It is a working fallback, not an
equivalent.

---

## 4. IS-05 — what a controller actually does with it

`export` only *reads* IS-05. The operations that change a plant are below.
Recorded here because the shape is not obvious and it is not a matrix.

### Making a route is a receiver-side operation

NMOS has no crosspoint. There is no "set destination D to source S" call.
A route is made by telling a **receiver** which stream to join:

```
PATCH /x-nmos/connection/v1.1/single/receivers/{rx}/staged
{
  "sender_id": "<sender uuid>",
  "transport_file": { "type": "application/sdp", "data": "<the sender's SDP>" },
  "master_enable": true,
  "activation": { "mode": "activate_immediate" }
}
```

The **sender is not touched**. It is already emitting to a multicast group, and
N receivers can join it without it knowing — the model is one-to-many by
construction, unlike a matrix crosspoint. IS-04 then reflects the result:
`receiver.subscription.sender_id` is set, and `sender.subscription.active`
becomes true.

Disconnect is the same PATCH with `master_enable: false` (and `sender_id: null`).

### Changing multicast address or port is a sender-side operation

This is the *only* thing IS-05 changes about a sender, and it is exactly two
fields per leg:

```
PATCH /x-nmos/connection/v1.1/single/senders/{tx}/staged
{
  "master_enable": true,
  "transport_params": [
    {"destination_ip":"239.4.1.27","destination_port":20000},
    {"destination_ip":"239.6.1.27","destination_port":20000}
  ],
  "activation": {"mode":"activate_immediate"}
}
```

One entry per ST 2022-7 leg, in SDP `m=` order. On activation the node
**regenerates its SDP** — a controller never edits an SDP, it sets the
transport parameters and the node re-emits. Labels, formats, `ts-refclk` and
everything else in the SDP are unreachable through IS-05.

Consequence worth stating: **every receiver already joined to that sender holds
the old SDP.** Re-homing a live sender orphans its receivers until they are
re-pointed, which is why a plant assigns groups once and routes by receiver
afterwards.

### Bulk vs single — and the cross-node question

| | `/single/{side}/{id}/staged` | `/bulk/{side}` |
|---|---|---|
| Version | v1.0 + v1.1 | **v1.1 only** |
| Scope | one endpoint | many endpoints **on one device** |
| Body | one staged object | array of `{id, params}` |

**Bulk is per-device.** It is an endpoint on *one* node's Connection API, so it
can only ever address endpoints belonging to that node. There is no cross-device
bulk in NMOS, and there is no NMOS call that touches two devices.

That answers the mixed-node case directly:

- **Same node, many endpoints** (re-homing 176 senders on one device) → one
  `POST /bulk/senders` on that device.
- **Sender on node A → receiver on node B** → **one** call, to node B. The
  sender is not involved (see above). Nothing is "mixed".
- **A salvo across many nodes** → one bulk call *per node*, fanned out in
  parallel. Atomicity across devices does **not** come from bulk — it comes from
  `activation.mode = "activate_scheduled_absolute"` with the **same TAI
  timestamp** in every call. That is the spec's only mechanism for "switch
  these 12 receivers on 4 nodes at the same instant".

Two field caveats already recorded in `../CLAUDE.md` and
[`matrix-compliance.md`](matrix-compliance.md), both of which break the clean
story:

- **Lawo VSM rejects scheduled activations** and silently coerces to immediate.
  A mixed plant therefore needs an immediate-activation fallback with parallel
  fan-out, accepting the jitter, and firing
  `nmos_scheduled_activation_unsupported`.
- **Lawo VSM has no bulk path.** Absence of `/bulk/*` is legal; the controller
  falls back to N single PATCHes and fires `nmos_bulk_unsupported`.

So a controller needs all three paths — bulk, single, and scheduled-absolute
fan-out — and has to choose per peer from what that peer advertises. None of
this is built yet; it is the `connect` verb plus #841.

---

## 5. Supporting layers

| Layer | State |
|---|---|
| **DNS-SD** | see §3 |
| **HTTP + WebSocket** | ✅ own stdlib server/client, hand-rolled RFC 6455 |
| **Wireshark dissector** | 🟡 `wireshark/dhs_nmos.lua` — 444 lines, **discovery layer only** (DNS-SD). The root `CLAUDE.md` rule requires every transport and every wire version: the HTTP APIs, the IS-07 WS envelope and the IS-12 WS envelope are not dissected yet |
| **Version negotiation** | ✅ `spec.SelectHighestMutual` + typed `ErrNoCommonVersion` — never silently downgrades |
| **Compliance events** | ✅ `spec.ComplianceEvent` + `Reporter` seam exists and is used by IS-04/IS-09 paths |

---

## 6. Test and CI state

| Gate | State |
|---|---|
| Unit tests | 35 files, all in-package |
| **Integration tests** | ⬜ **none for NMOS** — 24 exist across the other connectors; no `//go:build integration` file under `internal/amwa/`, no `NMOS_TEST_*` env var |
| **CI coverage floor** | ⬜ **amwa appears nowhere in `.github/workflows/ci.yml`** — every other connector holds a 100% per-package floor; NMOS is the only exempt one |
| Untested files | `session/dnssd/mdns.go` (466 lines) and `session/dnssd/avahi_linux.go` (565 lines) have no test files |
| Architecture tests | ✅ `internal/amwa/dependencies_test.go` — codec stdlib-only, session must not import plugins, no cross-plugin imports |
| CI on stacked PRs | ✅ fixed in #844 — `pull_request` no longer filtered to `base: main`, so a stacked unit is gated like any other |

New packages in review:

| Package | Coverage | PR |
|---|---|---|
| `codec/sdp` | 100.0% | #828 |
| `audit` | 95.9% | #836 |
| `session/export` | 90.8% | #838 |
| `probe` | 91.7% | #842 |
| `session/connection` (IS-05 client) | 94.2% | #834 |

---

## 7. CLI surface — and the canonical-verb gap

NMOS has its **own** dispatcher (`runNMOSConsumer`, `cmd/dhs/cmd_nmos.go`) and
never reaches the generic consumer router. ADR-0002 §Forbidden lists *"Skipping
a canonical verb because this protocol does not need it"*, so this is a
standing violation, not a to-do.

| Command | State |
|---|---|
| `dhs consumer nmos discover` | ✅ |
| `dhs consumer nmos system` | ✅ (IS-09; protocol-specific extension) |
| `dhs consumer nmos walk` | ✅ |
| `dhs consumer nmos events watch` | ✅ |
| `dhs producer nmos serve --role node\|system` | ✅ |
| `dhs producer nmos events serve` | ✅ |
| `dhs registry nmos serve` | ✅ |
| `dhs consumer nmos export` | 🔵 #838 — canonical verb, replaces the PowerShell harvester |
| `dhs consumer nmos audit` | 🔵 #836 — **new canonical verb**, see below |
| `dhs consumer nmos profile` | 🔵 #842 — canonical verb, live per-device conformance + latency |
| `dhs consumer nmos watch` | ⬜ named in `CLAUDE.md`, not dispatched (#830 adds it) |
| `dhs consumer nmos connect` | ⬜ the IS-05 verb — §4 above describes what it must do |
| `dhs consumer nmos ncp` | ⬜ IS-12 |

**Not dispatched, and mandated for every protocol by ADR-0002:**
`connect` · `disconnect` · `info` · `tree` · `get` · `set` · `watch` ·
`import` · `extract` · `status` · `health` · `ensure` · `validate` ·
`replay` · `diff` · `convert`

Sixteen verbs. Every other connector has them.

### `audit` is a new canonical verb, not an alias

`validate` is codec-level: `validate <frames.jsonl>` decodes every wire Trame
through the connector's codec (ADR-0021). `audit` is plant-level. Different
input, different layer, different question:

| verb | input | layer | question |
|---|---|---|---|
| `validate` | `frames.jsonl` | codec / wire | does every frame decode per spec? |
| `audit` | export directory | plant / cross-device | does this plant conform, across devices? |

An audit examines and reports and changes nothing, which is what the verb does.
It applies to any protocol — an Ember+ or Probel plant can be audited the same
way — so it belongs in ADR-0002's canonical table rather than as an NMOS
special case. **ADR-0002 amendment required; not yet raised.**

---

## 8. Ordered plan

Controller-first: the read path is low-risk, and it turns every reachable
device into a committed asset to build the node against. Validating our node
against our own provider proves nothing (oracle-per-tier, ADR-0025 step 5).

| # | Unit | State |
|---|---|---|
| 1 | **SDP parser** | 🔵 #828 |
| 2 | **IS-05 client** | 🔵 #834 |
| 3 | **`export`** — plant capture in Go | 🔵 #838 |
| 4 | **`audit`** — offline plant conformance | 🔵 #836 |
| 5 | **`profile`** — live conformance + latency | 🔵 #842 |
| 6 | **ADR-0002 amendment** — add `audit`; route NMOS through the generic dispatcher and fill the 16 missing verbs | ⬜ |
| 7 | **IS-05 server** on the node (#841) | ⬜ our node becomes connectable; unblocks end-to-end routing |
| 8 | **`connect` verb** — bulk + single + scheduled-absolute fan-out per §4 | ⬜ needs 7 to be testable against ourselves |
| 9 | **SDP correlation in `audit`** — sender SDP vs IS-05 active `destination_ip` | ⬜ needs 1 merged |
| 10 | **Invoke the BCP validators** in serve + walk paths | ⬜ code exists; wiring only |
| 11 | **IS-12 + MS-05 control endpoint** | ⬜ prerequisite for BCP-008 |
| 12 | **BCP-008 status monitoring** | ⬜ needs 11 |
| 13 | **IS-08 channel mapping** | ⬜ audio shuffles |
| 14 | **CI gate**: amwa coverage floors + a test asserting registered minors == the published list | ⬜ makes "cover all the spec" enforced, not promised |
| 15 | **Dissector**: extend `dhs_nmos.lua` to the HTTP APIs and the IS-07 / IS-12 WS envelopes | ⬜ root `CLAUDE.md` requirement |
| 16 | **Bonjour backends** (#195 Windows, #196 macOS) | ⬜ so dhs uses the daemon we deploy |
| 17 | Missing specs: IS-06, IS-10, IS-11, IS-14, BCP-003-01, BCP-005-01, BCP-007-03 | ⬜ binding rule — implement, never reframe |

---

## 9. Definition of done — per unit

Applies to every unit above, on top of ADR-0025's six deliverables:

- [ ] Every published minor of the spec implemented and selectable; DNS-SD
      `api_ver` advertises them all; server trees serve them in parallel
- [ ] Table-driven unit tests with expected values taken from the **spec text**
      or a **captured device**, never from our own encoder
- [ ] Deviations absorbed and reported as `spec.ComplianceEvent` — never
      silently patched
- [ ] Integration test against the vendor oracle (nmos-testing / a real node),
      gated by `//go:build integration` + env var — **not** against our own provider
- [ ] `dhs consumer nmos profile` passes against our own node and registry
- [ ] Per-package coverage floor added to `.github/workflows/ci.yml`
- [ ] Dissector extended to any new transport or wire version
- [ ] `go vet`, `golangci-lint`, `gofmt` clean; codec packages stdlib-only

---

## 10. Playbook

### Serve and query locally

```bash
dhs registry nmos serve --bind :8235 --advertise-host <ip>:8235
dhs producer nmos serve --role node --config <bundle>.json --bind :18080 \
    --advertise-host <ip>:18080 --registry http://<registry>:8235
dhs consumer nmos discover
dhs consumer nmos walk --registry http://<registry>:8235
```

A node in registered mode correctly **suspends its `_nmos-node._tcp`
announce** — that is IS-04 behaviour, not a fault (§3).

### Capture, then audit

```bash
dhs consumer nmos export --target <registry-or-node>:<port> --out ./plant
dhs consumer nmos audit  --dir ./plant --min-severity warn
dhs consumer nmos audit  --dir ./plant --format jsonl --out findings.jsonl
```

**Capture a registry, not a node, when you want the plant** — the export
follows every node the registry lists. But a registry alone is never enough:

| | registry export | node export |
|---|---|---|
| IS-04 resources | whole plant | that device only |
| **IS-05 `active` / `staged`** | **never** — not in the Query API | only source |
| multicast collisions, 2022-7 legs, destinations | ✗ | ✓ |
| registration state, version isolation, unreachable nodes | ✓ | ✗ |

The registry gives you the plant; the node gives you the truth. Direct-node
export stays mandatory for Mode C/D plants, which have no registry to ask.

### Live conformance

```bash
dhs consumer nmos profile --target <host>:<port> --format jsonl --out results.jsonl
```

Read-only: never PATCHes, activates, or registers, so it is safe against a
plant that is on air. Answers what a capture cannot — unknown-version
rejection, CORS, paging semantics, `query.downgrade`, heartbeat lifecycle —
plus per-endpoint p50/p95/p99.

`nmos-testing` remains the reference suite and runs on the tooling host.
Non-interactive mode, its tool API, IS-07 MQTT mode, BCP-003-01 TLS mode and
IS-10 auth mode are all still to be wired (#173). Only IS-04-01 `test_22`
(reboot persistence, #198) stays manual.

---

## 11. Known real-device facts

From live EVS Neuron captures and a 44-node customer plant
(`10_44_55_56_8080_20260823-233005`, 45 devices) — these shape what the
implementation must handle:

- A Neuron serves **IS-04 v1.0–v1.3, IS-05 v1.0/v1.1, IS-09 v1.0** and nothing
  else: no IS-07, IS-08 or IS-12.
- SDP lives at the sender's **`manifest_href`**; IS-04 defines no
  `/senders/{id}/transportfile` — that path is IS-05 only. Our node serves the
  non-spec route, which is worth revisiting.
- Every `urn:x-nmos:tag:grouphint/v1.0` group held **exactly one role**, so the
  node expressed no association between the video, audio and ANC of one signal.
  The device's own `NMOS Group Hint` objects were empty — a configuration gap,
  not a hardware limit.
- The Query API **pages**: taking only the first page returned 11 of 68 nodes.
- **Version isolation is real and silent.** The customer registry lists
  **45 nodes at v1.1 and 39 at v1.3** — six devices invisible to a controller
  speaking only the highest minor, with no error anywhere. Same registry:
  100 devices at v1.1/v1.2, 98 at v1.3.
- The registry's senders, flows and receivers walks all ended at **exactly 100**
  followed by an empty page. Either it holds one page, or its cursor stops
  advancing; a capture cannot tell the two apart, so nothing may reason from a
  missing resource in those collections.
- **PRISMON receivers publish `version: ""`.** Verified in the raw capture. A
  registry cannot order updates for a resource with no version.
- One node is **registered but unreachable** (`bm-n-aetac-001`,
  10.44.58.11:3212) — a stale registration controllers keep offering.
- One device has **12 of 12 senders master-enabled with every leg
  `rtp_enabled=false`** — reporting themselves on air while emitting nothing.
- **48 senders across the plant put both ST 2022-7 legs in one `/24`.**
  Redundancy assumes disjoint paths; one switch failure takes both.
- Multicast range encodes a bandwidth class the fabric controller reads, and
  the class must be chosen before a signal moves to UHD — so address allocation
  is policy, not a free pick.

---

*Update this file with every NMOS unit. If it disagrees with the tree, the tree
wins and this file is stale — fix it in the same PR.*
