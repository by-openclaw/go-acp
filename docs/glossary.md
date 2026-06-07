# Glossary — canonical terms

**Strict sourcing rule:** every definition below is taken from a reference doc and
cites it. Nothing is extrapolated. A term with **no** authoritative definition in a
reference doc is marked *(no formal definition — see note)* rather than invented.
Terms established only in 2026-06-07 session docs (not yet a binding ADR) are
quarantined in §I and marked **proposed**.

Authority order (ADR-0015): an **ADR** wins; `CLAUDE.md` / per-protocol `CLAUDE.md`
and `docs/*` are cited where they are the defining source.

---

## A. Roles

| Term | Definition | Source |
|---|---|---|
| **consumer** | Outbound role: connect to a device and query/control it (`dhs consumer <proto> <verb> <target>`). | CLAUDE.md "Project purpose"; ADR-0002 |
| **producer** | Inbound role: serve a canonical tree as a device (`dhs producer <proto> serve`). | CLAUDE.md "Project purpose"; ADR-0002 |
| **registry** | Dual-face middleware role: consumes registrations + provides catalogue (`dhs registry <plugin> serve`). NMOS only. | ADR-0002; internal/amwa/CLAUDE.md |

## B. Plugin architecture & code

| Term | Definition | Source |
|---|---|---|
| **codec** | Per-protocol `internal/<proto>/codec/` package that encodes/decodes wire bytes; **stdlib-only, lift-to-own-repo ready**. | CLAUDE.md "Folder layout"; ADR-0006 |
| **Protocol (interface)** | The neutral consumer interface (`Connect/Disconnect/GetDeviceInfo/GetSlotInfo/Walk/GetValue/SetValue/Subscribe/Unsubscribe`); all CLI/API talk to it only. | CLAUDE.md "IProtocol" |
| **Provider (interface)** | The neutral provider interface (`Serve/Stop/SetValue`) + `Factory.New(logger, *canonical.Export)`. | CLAUDE.md "Plugin tiers"; internal/provider |
| **Factory** | Per-protocol registration object; one `init()` calls `consumer.Register(&Factory{})` / `provider.Register`. | CLAUDE.md "Plugin tiers" |
| **compliance event / `compliance.Profile`** | Named, absorbed spec-deviation: the plugin keeps running, increments a counter, and fires a `compliance.Event`; never silently works around a deviation. | CLAUDE.md "Compliance pattern"; ADR-0008 |
| **announce** | A device-emitted value-change / status event (unsolicited inbound frame). Per-protocol wire shape: acp1 MTID=0, MTYPE=0/2; acp2 type=2. | internal/acp1/CLAUDE.md "Announcements"; internal/acp2/CLAUDE.md "Announces" |

## C. Data model (ADR-0022)

| Term | Definition | Source |
|---|---|---|
| **Device** | Physical unit (chassis / rack / standalone box). | ADR-0022 "Entity hierarchy" |
| **Frame** | Chassis instance per Device (usually one). | ADR-0022 |
| **Slot** | Card bay; sparse (empty bays omitted). | ADR-0022 |
| **Card** | Instance of a card model in a slot. | ADR-0022 |
| **DM (device model)** | The card's protocol surface (objects); keyed by `(Model, SwRev)` only; carries model/sw_rev/protocol/objects; never IP/slot/port. | ADR-0022 "DM is the schema" |
| **manifest** | Per-device wiring file (the **where**: endpoints, frames, slots → DM refs). DM answers **what**. | ADR-0022 "Manifest shape" |
| **addr** | Opaque, plugin-owned token resolving a slot entry to one DM (e.g. acp1 `{slot}`, Probel `{matrix,level}`). | ADR-0022 |

## D. Matrix (ADR-0023)

| Term | Definition | Source |
|---|---|---|
| **matrix** | Routing entity parallel to the Card hierarchy (not under it). | ADR-0023 "Matrix is a parallel entity" |
| **matrix_id** | Integer matrix identifier per device. | ADR-0023 "Matrix identity" |
| **level_id** | Level index (audio L/R / embed / multi-format / …). | ADR-0023 |
| **size** | `(destinations, sources)` tuple; scale floor 65 535². | ADR-0023; CLAUDE.md "Scale targets" |
| **behavior** | `1to1` / `1toN` / `NtoM` / `dynamic`. | ADR-0023 "Behavior values" |
| **crosspoint** | A routed (target, source) intersection; addressed by protocol-specific coordinates (Probel: matrix/level/dst/src). | ADR-0023 "Addressing a crosspoint" |

## E. Wire / per-protocol terms

| Term | Definition | Source |
|---|---|---|
| **AN2** | Axon transport framer: 8-byte big-endian header, magic `0xC635`, `proto` field (1=ACP1, 2=ACP2). Required for ACP2. | internal/acp2/CLAUDE.md "AN2 frame header" |
| **EnableProtocolEvents** | AN2 call a consumer must issue (`[2]`) after connect, or ACP2 announces never arrive. | internal/acp2/CLAUDE.md "Key invariants"; CLAUDE.md "What NOT to do" |
| **MTID** | ACP1 message transaction id (u32); 0 = broadcast/announcement; never reuse in-flight. | internal/acp1/CLAUDE.md "Wire header" |
| **MCODE / ObjGrp / ObjId** | ACP1 method-or-error code / object group / object index within group. | internal/acp1/CLAUDE.md |
| **S101** | Ember+ framing layer over TCP: BOF `0xFE` / EOF `0xFF` / escape `0xFD`, CRC-16/CCITT, keep-alive. | internal/emberplus/CLAUDE.md "Transport" |
| **Glow / BER** | Ember+ semantic types (GlowDTD, APPLICATION tags 0–25) carried in ASN.1 BER. | internal/emberplus/CLAUDE.md "Stack", "GlowDTD tag numbers" |
| **tally** | Probel crosspoint-state broadcast (cmd 3 Crosspoint Tally) / TSL on-air indicator. | internal/probel-sw08p/CLAUDE.md (cmd 3); internal/tsl/CLAUDE.md |
| **salvo** | Probel grouped multi-crosspoint operation (src/dst/level triplets applied together). | internal/probel-sw08p/CLAUDE.md "Data model" |
| **Broadcasts** | *(no formal doc definition)* — an acp1 device control object (slot 0) observed to gate the device's announce emission; our acp1 **provider** implements a "Broadcasts gate". | CHANGELOG (acp1 provider Broadcasts gate, issue #257); device walk. **Not specified in a reference doc** beyond the provider gate. |

## F. Discovery (ADR-0012)

> Note: ADR-0012's scope is **under revision** (adr-coherence-2026-06-07 §2/§4): the
> mDNS/DNS-SD/peer-list framework is NMOS-grade; for simple protocols mDNS is an
> optional locator only. Definitions below are quoted from ADR-0012 as it stands.

| Term | Definition | Source |
|---|---|---|
| **discovery** | Locating a peer's endpoint (IP:port) via one of: mDNS, unicast DNS-SD, or peer-list. | ADR-0012 |
| **mDNS** | Multicast DNS zeroconf service discovery (link-local). | ADR-0012 |
| **DNS-SD (unicast)** | Unicast service discovery for routed/sealed networks. | ADR-0012 |
| **peer-list** | Static `peers.csv`/`peers.yaml` of host:port — no discovery infrastructure; consumer must know the address. | ADR-0012 |

## G. Errors (docs/protocols/error-codes.md)

| Term | Definition | Source |
|---|---|---|
| **exit code** | `0` success · `1` runtime/wire/protocol · `2` usage/validation/state. Unix-standard, never 3+, cross-OS uniform. | docs/protocols/error-codes.md |
| **error message format** | `<layer>:<code>: <human message>` (stable, grep-able). | docs/protocols/error-codes.md |
| **errcode** | `internal/errcode/` — typed `Code`+`Layer`+`Class` sentinels; `errors.Is` dispatch. | docs/protocols/error-codes.md |

## H. Process & governance

| Term | Definition | Source |
|---|---|---|
| **ADR** | Architecture Decision Record in `docs/adr/`; binding, permanent once `accepted`; one concern each. | docs/adr/README.md |
| **single source of truth** | Each rule has exactly one ADR; other docs link, never restate. | ADR-0015 |
| **DOD (definition of done)** | The six per-connector deliverables required before a connector is done. | ADR-0025 |
| **DOD window** | Period when ≥1 DOD deliverable is missing/partial; overrides only PR-timing of ADR-0014. | ADR-0027 |
| **ensure** | Declarative-convergence verb (`--state`/`--check`): read→compare→set-if-different, idempotent. | ADR-0007 |
| **idempotency** | Property that re-running an operation produces no further change once the desired state holds (the `ensure` contract). | ADR-0007 |
| **codeowner / approval** | `@yboujraf` approves merges/issue-closes; agent acts only on explicit "go"/"approuved"/"ok". | docs/user.md |

## I. Session-proposed (2026-06-07 — NOT yet a binding ADR)

These are defined only in this session's working docs; treat as **proposed** until
folded into an ADR (per adr-coherence §8).

| Term | Definition | Source (proposed) |
|---|---|---|
| **protocol model** (Tree/DM · Matrix · Push · Bridge) | Grouping of protocols by object model; verbs are meaningful only within a model. | docs/protocols/verbs.md §1 |
| **oracle-per-tier** (tier 1 unit / 2 consumer-integration / 3 provider-integration / 4 loopback-regression) | Test taxonomy: a connector's consumer is validated against vendor emulator + real device, never against our own provider (loopback is regression only). | docs/protocols/audits/repo-review-2026-06-07.md §7 |
