# Fabric — vision brainstorm

Captured conversation notes. Not a commitment, not a spec, not code. A reference so we don't re-derive these choices next month.

Naming is **parked**. Every concept below uses placeholder labels in backticks. Renaming is a separate exercise.

---

## 0. One-sentence product definition

A **broadcast facility control plane** that:

- discovers equipment via pluggable inbound connectors,
- publishes curated subsets to external consumers via pluggable outbound connectors,
- binds abstract templates to real resources,
- schedules time-gated productions with automatic failover,
- evaluates alarm policies on normalized state,
- propagates live metadata mutations as first-class events,
- is driven at the control plane by newsroom / automation protocols (MOS, GPI, LTC, OSC),
- times everything to external PTP.

---

## 1. The 5-layer tree pipeline

```
Equipment
  └─► Inbound connector              (discovers, decodes, subscribes)
        └─► `AdminTree`              (full facility, every device, every parameter — for supervision/sysop)
              └─► `CuratedTree`      (0..N per upstream — access masks, limits, enum masks, matrix remap+resize)
                    └─► `ConsumerTree`   (SI-composed: new root, nodes, Ember+-shaped, device-invisible)
                          └─► Outbound connector   (Ember+ provider, ACP2 provider, REST/WS, ...)
                                └─► External consumer
```

| Layer | Scope | Audience | Mutable by |
|---|---|---|---|
| `AdminTree` | Whole facility, every device/card, live values + schema + alarms | Admin / sysop | Discovered; refreshed on schema diff |
| `CuratedTree` | One upstream `AdminTree` slice; 0..N per upstream | SI / engineer | SI authors overlays (access, limits, enum mask, mtx remap) |
| `ConsumerTree` | Cross-device composed tree; device-invisible | One external consumer channel | SI authors structure; leaves bind to `CuratedTree` parameters |
| Device connector | Wire-format specific | — | Protocol plugin |
| Consumer connector | Wire-format specific | — | Protocol plugin |

**Key rule:** downstream layers **never mutate** upstream layers. Every overlay is a new object referencing the upstream by stable identity.

---

## 2. Separation of concerns — the 8 blocks

| # | Block | Owns | Consumes | Produces | Must NOT know |
|---|---|---|---|---|---|
| 1 | **Inbound device connector** | Wire codec, transport, session, discovery | Core set-downs | Normalized events upstream | Other connectors, UI, scheduler, alarms |
| 2 | **Outbound device connector** | Wire encode, server socket, fan-out to consumers | One `ConsumerTree` + its event stream | Consumer sets forwarded to Core | Which upstream connector originated a value |
| 3 | **Inbound control connector** (MOS / GPI / LTC / OSC) | Control-protocol codec, session, ack/status | Events from Scheduler/Instance | Intent events → Scheduler | Device wire formats, AdminTree values |
| 4 | **Core (engine)** | Event bus, subscription routing, state store, canonical-shape normalization, **schema diff engine** | All connector events + scheduler decisions | Routed events to subscribers, write-downs to inbound connectors | Wire formats, vendor details, UI layout |
| 5 | **Catalog** | Inventory of every device/card, category, metadata, online/offline state, availability flag, physical wiring topology | Heartbeat / presence from inbound; bindings from Templates | Resource lookup + online gate | Protocol details, event values |
| 6 | **Templates + Instances** | Abstract studio blueprints (Templates) + bound runtime trees (Instances) | Catalog + SI authoring | Live `ConsumerTree` projections | Wire protocols, which Instance is primary/backup |
| 7 | **Scheduler** | Time-gated plan, failover rules, production lifecycle (preroll / live / postroll) | Schedule + Catalog availability + PTP clock + control inputs (MOS/GPI/LTC) | Instance activate/promote/retire decisions | Wire protocols, event values |
| 8 | **Alarm engine** | Alarm template evaluation (multi-threshold, multi-severity, optional attachment) | Every parameter event + alarm template definitions + schema diffs | Alarm events (via Core event bus) | Which connector delivered a value, UI presentation |

Cross-cutting concerns (not owned by any single block):

| Concern | Location | Note |
|---|---|---|
| Event bus | Inside Core | No block owns bus publication semantics outside Core |
| Persistence | Dedicated storage adapter | One on-disk format per concept (connector config, Catalog, Template, Instance, Schedule, Alarm) |
| Identity + auth | Dedicated auth layer | Outbound connectors validate consumers; admin UI validates operators |
| Audit log | Dedicated sink | Every set / schedule-change / failover / alarm-ack |
| Config reload | Dedicated reconciler | Edits propagate without restart |
| Diff emission | Diff connector (see §5) | First-class schema-mutation broadcast channel |

Invariants:

1. No block imports another block's internals — typed interfaces at boundaries only.
2. Inbound and outbound connectors never address each other — only through Core.
3. Core is protocol-agnostic — zero imports of `acp1/`, `acp2/`, `emberplus/`, etc.
4. Alarms are evaluated on the **canonical shape**, not raw wire — same template fires identically regardless of vendor.
5. A Resource is **write-exclusive, read-shared** — at most one writing Instance at a time; unlimited observers.
6. `ConsumerTree` is a **projection**, not the authoritative state. Authoritative state lives in Core's state store.
7. Templates are **immutable** while Instances reference them — editing produces a new version; Instances pin a version.

---

## 3. Connectors — two distinct categories

| Axis | **Device connector** | **Control connector** |
|---|---|---|
| Examples | ACP1, ACP2, Ember+, Probel SW-P-02/08+, TSL UMD v3.1/4/5, RollCall | MOS (OpenMedia / iNews / ENPS), GPI, LTC, OSC, MIDI Show Control, SMPTE 2059 |
| Carries | **State** — values, announces, alarms | **Intent** — activate / advance / cue / trigger |
| Target block | Core state store | Scheduler + Instance manager |
| Direction | Read device state + write sets | Receive commands; emit ack + status |
| Registry | Separate factory per category — independently pluggable |

Each category has its own interface contract and its own lifecycle (hot-register / hot-unregister). The two converge at the Scheduler + Core pair.

---

## 4. Live schema — `AdminTree` is mutable

The hardest commitment in this vision: **the schema itself is a live stream of events**. No restart, no recompile, no reconnect to absorb a metadata change.

### Detected change → event class

| Upstream change | Event class | Severity |
|---|---|---|
| Parameter access R/W → R | `schema.access_changed` | Breaking for writers |
| Enum items mutated | `schema.enum_changed` | Breaking for consumers holding old values |
| min / max / step changed | `schema.range_changed` | Breaking for alarm thresholds |
| Matrix resized | `schema.matrix_shape_changed` | Breaking for crosspoint-bound consumers |
| Parameter added | `schema.parameter_added` | Compatible — additive |
| Parameter removed | `schema.parameter_removed` | Breaking — every binding to it |
| Unit / format changed | `schema.format_changed` | Cosmetic (usually) |

### Reaction matrix

| Event | Scheduler | Catalog | Alarm engine | Instance | Outbound |
|---|---|---|---|---|---|
| Resource online/offline | maybe promote backup | update state | re-evaluate active alarms | rebind if role-affecting | emit connection-state |
| Parameter value change | — | — | evaluate thresholds | project to bound role | forward to consumer |
| Schema diff (breaking) | optionally freeze Production | — | revalidate / invalidate template | rebind or invalidate Instance | forward as diff, or flatten per protocol capability |
| PTP tick / scheduled boundary | fire cue | — | — | activate / retire Instance | emit state change |
| MOS running-order update | mutate schedule | — | — | activate / advance Instance | — |
| Alarm fires | optionally trigger failover | — | — | — | notify subscribers |

### Consequences

- Discovered `AdminTree` is **never frozen** to a firmware revision. Revision becomes a **version label**, not an immutability guarantee.
- Subscriptions are **versioned** — consumer announces schema version V, receives diffs as schema advances, never a forced disconnect.
- Alarm templates must tolerate schema drift — re-evaluate on `schema.range_changed`, re-fire only when a transition crosses a live threshold.
- Running Instances may become invalid mid-show when a bound role loses a parameter. Instance manager reacts: freeze / alarm / auto-rebind per policy.
- Today's compile-time plugin registry needs to revise to **hot-registerable** — Q4's requirement effectively mandates this.

---

## 5. Diff connector — schema mutations as a first-class channel

Not device-inbound, not consumer-outbound. A dedicated subscription channel:

| Side | Role |
|---|---|
| Inbound side | Internal — subscribes to every upstream connector's schema; feeds Core's diff engine |
| Outbound side | External — WebSocket / Ember+ channel that broadcasts schema diffs to any consumer (compliance audit, UI schema watcher, bridge monitors) |

Makes schema mutations observable, auditable, replayable — not hidden inside individual connectors.

---

## 6. Time — external PTP

| Property | Value |
|---|---|
| Time source | IEEE 1588 PTP grandmaster on facility network |
| Fabric role | PTP slave |
| Accuracy target | ns (hardware-timestamping NIC) or μs (software) |
| Used by | Scheduler, every event timestamp, audit log, diff emission ordering |
| Fallback | Wall-clock mode, flagged **degraded** — operators may choose to refuse running in degraded mode (policy) |
| UI / human time | Rendered at display; internal timestamps stay PTP |

PTP timestamps give **exact ordering** of events across devices with different software delays — essential for replay and for failover correctness.

---

## 7. Resources and Catalog

### Granularity

**Resource = device or card**, not sub-parameter.

| Level | Catalog row? | Access pattern |
|---|---|---|
| Frame (DDB08 chassis) | Optional, if treated as one unit | — |
| **Card (slot 3 of DDB08)** | **Yes — primary granularity** | One entry, typed category |
| Parameter (gain / phantom / proc) | No — lives inside the card's Resource | Addressed from Template as `role.param_name` |

### Category (closed vocabulary — to be ratified)

Draft: `mic` · `earphone` · `speaker_monitor` · `video_monitor` · `multiviewer` · `audio_processor` · `video_processor` · `lighting` · `audio_console` · `video_mixer` · `router` · `ptz_camera` · `recorder` · `playout` · `graphics_cg` · `prompter` · `tally` · `psu` · `generic`.

Closed vocabulary = validation + UI affordances. Free tags deferred.

### Identity

Open question — candidates:
- Serial number (if reliably reported by device)
- MAC address (for IP-addressable equipment)
- `(chassis_id, slot, card_type)` tuple (slot-swap stable within chassis)
- SI-assigned stable UUID (authoritative regardless of hardware)

SI-assigned is the only one that survives all hardware mutations and is proposed as the anchor. Other identifiers are secondary cross-references.

### Availability states

`online` · `offline` · `flapping` · `reserved` · `in_use` · `quarantined`.

Online detection requires active keepalives (not passive pings) to meet sub-second failover SLAs.

---

## 8. Templates, Instances, Productions, Failover

### Concept

```
Template                    (abstract blueprint — roles with category + required params + alarm policies + default values)
  └─► Instance              (Template × Catalog-resource bindings = live tree streaming values)
       └─► Production       (one Instance can host many concurrent Productions)
            └─► Schedule slot    (time-gated entry: preroll / live / postroll)
```

### Template content

- Typed roles (each role pins a category)
- Parameter metadata per role (labels, units, limits)
- Alarm template attachments per role / per param / per matrix element
- Default preset values per role / per param
- Validation constraints ("must have ≥1 `mic` role")
- Failover policy (which backup, preset-carry semantics)

### Instance lifecycle

- SI or Scheduler instantiates Template → binds roles → Instance is live
- Instance carries **preset + value** per parameter (§9) — continuously persisted
- Instance hosts N concurrent Productions — today's answer to Q "one Instance multiplex"
- Instance is retired when no Production references it AND it's outside all Schedule windows

### Failover pattern (locked)

Two-step:

```
1. Detect primary Resource offline (Catalog heartbeat / keepalive).
2. Scheduler promotes backup:
   a. For each role in the Instance Template:
      - Read last-known Preset for that role on primary.
      - Push Preset to backup Resource via its device connector.
   b. For every active matrix crosspoint routed through primary:
      - Reconfigure downstream matrix to route via backup.
3. Consumer sees no interruption — Fabric Instance identity preserved.
```

**Mandatory enabler:** Fabric persists Preset per role, per parameter, **continuously** (not just on change). Otherwise failover starts from Template defaults, not show-current state.

### Preset / Value split (DHS pattern, carried forward)

Each parameter carries two independent values:

| Value | Meaning | Updated by | Drives |
|---|---|---|---|
| **Preset** | Client-requested / intended | Consumer set | Equipment-bound event → write to device |
| **Value** | Equipment-confirmed | Announce / reply from device | Consumer-bound event → notify subscribers |

Both raise events independently. Divergence = in-flight state. Converge on device confirmation.

---

## 9. Alarm templates

### Policy shape

Multi-threshold, multi-severity, optional attachment:

```
AlarmTemplate "audio_overdrive":
  thresholds:
    - value: -3 dB   severity: info       dwell: 500 ms
    - value:  0 dB   severity: warn       dwell: 200 ms
    - value: +6 dB   severity: critical   dwell: 0 ms
  hysteresis: 1 dB
  auto_ack: no
  scope: "peak"
```

### Attachment

| Attachable at | Example |
|---|---|
| Device / card (one Resource) | PSU output threshold on card `psu_1` |
| Parameter path | `mic_1.gain` on every Instance using the template |
| Matrix target | `router.target[5]` output level |
| Matrix source | `router.source[12]` input level |
| Matrix crosspoint | `router.src[12]→tgt[5]` specific cross |
| Category-wide | All Resources of category `mic_input` |

Silent-by-default: no template attached = no alarm. Engineer explicitly opts in per element.

Same template, different threshold values per attachment — policy is the shape, attachment provides values.

### Schema-drift handling (Q4 intersection)

On `schema.range_changed` for a monitored parameter, alarm engine revalidates:
- New max < configured threshold → mark `template_threshold_unreachable`, notify SI
- Current value crossed a threshold by virtue of the range change → fire the alarm at the transition

---

## 10. Control-plane input — MOS / OpenMedia

### Role

MOS connects to Fabric as a **control connector** (category §3). Newsroom system drives Scheduler; Fabric drives device connectors downstream.

```
OpenMedia (NCS) ── MOS XML/TCP ──► [MOS control connector]
                                         │
                                 running-order events
                                         ▼
                                 Scheduler mutates Schedule
                                         ▼
                                 Instance activate / advance / cue
                                         ▼
                                 Device connectors execute
                                         ▼
                                 Status up the chain → MOS connector → NCS
```

### MOS ↔ Fabric concept mapping

| MOS | Fabric |
|---|---|
| Running Order (RO) | Active Schedule timeline |
| Story | Logical segment / mini-Production group |
| Item | Schedule cue binding to an Instance action |
| mosObj | Resource on a MOS server (playout clip, CG template, prompter page) — maps to Catalog entry |
| mosID | `(MOS-server-id, objSlug, objID)` → Fabric Catalog identity via SI-authored or auto-discovered mapping table |
| roElementAction | Live Schedule edit during show |
| roAck / mosAck | Fabric's reply |
| `readyToAir` / `cue` / `onAir` / `offAir` | Fabric status emitted back once Instance fires on device |
| Heartbeat | Catalog online/offline input |

### Likely deployment profile (OpenMedia)

| Item | Typical |
|---|---|
| MOS version | 2.8.5 |
| Profiles | 0 + 1 + 2 (basic + RO + item mutability). Profile 3 if playlist used. |
| Transport | TCP 10540 (lower) + 10541 (upper), two sockets |
| Authentication | IP allowlist; no in-band auth in 2.8.5 |
| CGI supplement | OpenMedia HTTP endpoints for mosObj queries + NCS status |
| Unicode | UTF-8, needs encoding declaration per deployment |

### Scope levels

| Level | Content | Effort ballpark |
|---|---|---|
| Minimum viable | MOS Server profile 0+1: receive RO, parse, emit ack, no Instance wiring | 2 weeks |
| Integrated | RO drives Schedule → Instance activation → cue down → status up | 4-6 weeks |
| Full (profiles 0-4 + CGI) | Live item mutability, mosObj CRUD from NCS, replay recovery | 8-12 weeks |

---

## 11. Gap vs DHS v1

| DHS v1 | Fabric | Notes |
|---|---|---|
| MasterView per device | `AdminTree` for whole facility | Scope reframed — one facility-wide live tree, not per-device |
| ClientView per MasterView | `CuratedTree` (0..N per upstream) | Same concept, renamed, cardinality explicit |
| LogicalView (aggregation) | `ConsumerTree` (composition) | Device-invisible composition, not aggregation of visible devices |
| Device Driver | Inbound device connector | Same role |
| Client Driver | Outbound device connector | Same role |
| — | **Inbound control connector (MOS/GPI/LTC/OSC)** | New category |
| Preset / Value split | ✅ kept | Continuously persisted per role for failover |
| Hot-plug registry (DLL drop-in) | ✅ revived | Reverses acp's current compile-time Tier-1 — needs explicit policy revision |
| SI Topology Manager | UI (deferred) | Not in core scope yet |
| Guid-based stable identity | SI-assigned UUID anchor | More explicit than DHS's CommandID |
| — | **Catalog with categories + online state + availability** | New first-class block |
| — | **Templates + Instances** | New — DHS had DeviceModel, not abstract Templates |
| — | **Scheduler + Productions + Failover** | New — DHS was state-only, no time-gating |
| — | **Alarm templates (multi-threshold, multi-severity)** | New — DHS had alarms on ACP1 only, no policy shape |
| — | **Live schema events (§4, §5)** | New — DHS MasterView was frozen per revision |
| — | **External PTP** | New — DHS ran on OS wall-clock |
| — | **Diff connector** | New — first-class schema mutation channel |

Retired from DHS v1:
- XML persistence → JSON/YAML files
- C#/.NET Windows → Go cross-platform + container
- Closed-NDA protocols (RollCall, IControl) → not brought forward

---

## 12. Open questions (A–H and new)

From the conversation, not yet decided:

| # | Question | Impacts |
|---|---|---|
| A | Revise compile-time plugin registry to hot-pluggable? | Foundation — Q4 effectively mandates this |
| B | Resource stable identity anchor — serial / MAC / SI-UUID? | Catalog schema + migration behaviour |
| C | PTP fallback policy — degraded wall-clock or hard-fail? | Deployment configurability |
| D | Failover granularity — whole Instance or per-role partial? | Failover complexity + state coherence |
| E | Schema diff delivery granularity — every change vs batched windows? | Outbound connector chatter tolerance |
| F | Diff connector — same channel internal+external or split? | Serialization cost |
| G | Alarm templates on schema drift — snap thresholds vs fire `template_invalid`? | SI intervention model |
| H | MOS-driven activation + schema diff at same PTP tick — freeze vs arrival-order? | Race conditions need rules |
| I | One Instance multiplexes many Productions — how are they addressed? (Today's answer raises this) | Instance and Production schemas |
| J | `ConsumerTree` leaf fan-out/fan-in — 1:1 ClientView reference vs N:1 synthetic/derived parameters? | ConsumerTree semantics |
| K | `ConsumerTree` matrix across devices — federated crosspoints vs confined per physical matrix? | Scope ceiling — federated = real-time signal-flow engine |
| L | Template version pinning by Instance vs live-adapt mode? | Show-safety vs flexibility |

---

## 13. Relationship to `acp` today

This vision is **a destination**. `acp` today is **Part A-complete**: three consumer plugins (ACP1, ACP2, Ember+) emitting canonical trees, with a compliance profile, offline CSV round-trip, and a CLI.

Parts that already exist in some form:

- Canonical tree shape (→ `AdminTree` baseline)
- Protocol plugin interface (→ inbound device connector contract)
- Event model (→ Core event bus baseline)
- Disk persistence (→ storage adapter baseline)
- `--capture` frame recording (→ basis for replay + Diff connector)
- `compliance.Profile` (→ alarm-engine-adjacent counter infrastructure)

Parts that do not yet exist and are this vision's new scope:

- Outbound device connectors (Part B starts this)
- Control inbound connectors (MOS + GPI + LTC + OSC)
- Catalog + Resource category + online state
- Template + Instance + Production + Schedule
- Preset/Value split
- Alarm engine + Alarm templates
- Diff connector
- PTP time integration
- Hot-pluggable connector registry
- Cross-device `ConsumerTree` composer
- `CuratedTree` overlay resolver (access + limits + enum mask + matrix remap)

Roadmap ordering stays as documented in the memory `project_scope_sequencing`:

1. Part A (done)
2. Part B — Ember+ provider (outbound)
3. #36 — DM library umbrella (extract, diff, scenario harness, selective import, dissector, fixture layout)
4. Part C — bus bridge
5. Part D/E — Probel
6. Part F — TSL
7. Cross-protocol mapper

Fabric-level features (Catalog / Templates / Alarms / Schedule / MOS / Diff / PTP) layer on top once ≥2 provider-capable plugins exist. No need to freeze their design today.

---

## 14. Naming parking lot

**Do not reopen** (parked per memory `project_naming`): Manifold · Patchbay · Conduit · Atrium · Nexus · Pivot · Axon · Sinew · Keel · Strata · Prism · Fulcrum · DHS.

Current working labels (all placeholders):

| Concept | Working label |
|---|---|
| Whole product | `Fabric` |
| Core engine | `Core` |
| Facility-wide live tree | `AdminTree` |
| Per-upstream overlay | `CuratedTree` |
| Cross-device composed tree | `ConsumerTree` |
| Inbound device plugin | `Collector` |
| Outbound device plugin | `Emitter` |
| Control input plugin | `ControlConnector` |
| Resource registry | `Catalog` |
| Abstract blueprint | `Template` |
| Bound runtime tree | `Instance` |
| Scheduled event | `Production` |
| Timeline | `Schedule` |
| Alarm policy shape | `AlarmTemplate` |

Rename once. Don't debate in-flight.

---

## 15. One-page decision summary

- **Live metadata** — schema mutations are first-class events, propagate without restart. (Q4)
- **PTP time source** — external grandmaster, ns accuracy, degraded fallback policy TBD. (Q6)
- **Failover = matrix reroute + preset reload**, two-step, consumer-transparent. (Q3)
- **Resource granularity = device or card**, not sub-parameter. (Q1)
- **Alarm templates** — multi-threshold multi-severity, optional attachment at device/card/param/matrix-element, silent-by-default. (Q2)
- **ClientView cardinality** — 0..N per MasterView.
- **MOS** — control connector, distinct category from device connectors; drives Scheduler.
- **Instance multiplexing** — one Instance hosts many concurrent Productions.
- **Naming parked** — working labels only; rename exercise later.
- **DHS concepts kept** — MasterView/ClientView/LogicalView intent + Preset/Value split + hot-plug + one abstract DM + event-driven reactive + plugin template.
- **DHS concepts extended** — live schema, PTP, Catalog, Templates, Instances, Schedule, Alarms, Diff connector, control connectors.
