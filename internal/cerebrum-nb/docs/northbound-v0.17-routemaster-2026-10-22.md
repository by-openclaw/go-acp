# Cerebrum — Northbound API v0.17: RouteMaster create, read, update, delete against RCP

**From:** RTBF — integration team
**Date:** 2026-10-22
**Status:** measured on Cerebrum 2.9.3 build 15692 on 2026-10-22; draft for review
**Owner:** yob@rtbf.be
**Related:** *Cerebrum — RCP and Northbound API: which one for which feature*, dated version 2026-10-21

## 1. Introduction

EVS sent the Northbound API document v0.17 (October 2026). The previous
version we worked from was 0v16. This note states, operation by
operation, what RCP 2.5.3 does on our server, what Northbound could do
before v0.17, what v0.17 documents, and what our Cerebrum 2.9.3 build
15692 answers to each v0.17 message. Every "proven" was run on the
server on 2026-10-22 and read back over the other interface; nothing is
taken from the document alone unless the table says so.

## 2. Context

| # | Item | Value |
|---|---|---|
| 1 | Cerebrum under test | 2.9.3 build 15692, web client 2.9.3 build 2222, standalone (no federation, no redundancy) |
| 2 | Northbound documents | 0v16 (33 pages), **v0.17** (35 pages, October 2026, "NEURON BRIDGE" header) |
| 3 | Northbound served | the login reply reports `api_ver 0.1` on 2.9.2 and on 2.9.3 alike; the document version is not reported on the wire; the v0.17 messages are accepted and applied by 2.9.3 |
| 4 | RCP served | **2.5.3** (`GET /v2/api`); document 2.6.1; re-tested on 2.9.3 on 2026-10-21, unchanged |
| 5 | Our automation | Ansible scripts; the Northbound connector gained the v0.17 messages on 2026-10-21 (`rm-add`, `rm-delete`, `--federation` on association and tags); the measurement is one play, run twice on 2026-10-22 with the same answers |

## 3. Scope of work

| # | In scope | State |
|---|---|---|
| 1 | Difference 0v16 → v0.17, message by message | done, from the two documents (section 6) |
| 2 | Create, read, update, delete of local, virtual and federation sources and destinations: RCP against Northbound v0.17 | done, measured (section 5) |
| 3 | Levels: create, read, update, delete, properties, on both interfaces | done, measured: create and rename proven, delete refused; the level created for the test (id 5, "ZZ-PROBE-L2") stays on the server until it is deleted in the user interface, no interface deletes a level |
| 4 | Level bindings, tags, mnemonics, categories, alternate mnemonics | done; the v0.17 FEDERATION variants measured on a standalone server |
| 5 | Implementation in our scripts | done: codec, consumer, CLI verbs, unit tests from the v0.17 examples |
| 6 | Measurement on the lab Cerebrum: each message on temporary IOs, read back over RCP, undone | done on 2026-10-22; collections the same size before and after |

## 4. Reading the tables

| Mark | Meaning |
|---|---|
| **proven** | run on the Cerebrum of section 2, result read back over the other interface |
| document | described in the document; not run by us, or not provable on a standalone system |
| **no** | not offered by that interface or that version, or refused by the server |
| accepted, not applied | the server answers accepted and the read-back shows no change (a standalone server for the federation cases) |

The RCP column carries what was measured on 2.9.2 and re-measured on
2.9.3 on 2026-10-21 (statement of the same date). "Use" names the
interface our Ansible scripts use today.

## 5. Create, read, update, delete — entity by entity

### 5.1 Local sources and destinations

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 1 | Create, one or N | proven (`POST`, `count`); no id returned | **no** | `RM_SRCE_ADD` / `RM_DEST_ADD`, `COUNT` 1–1000 (§4.2.3); no id returned | proven: `RESULT ACCEPTED`, the IO is there within one second, named "Src 16" / "Dest 17" by the server | RCP; Northbound as the second path |
| 2 | Read, list | proven (`GET`) | proven (`OBTAIN SRCE_MNE` / `DEST_MNE`) | unchanged | proven | RCP |
| 3 | Update: rename | proven (`PATCH mnemonic`) | proven (`SRCE_MNE` / `DEST_MNE`) | unchanged | proven | RCP |
| 4 | Update: tie line inhibit | proven | **no** | **no** | — | RCP |
| 5 | Update: tie line group | **no** (400, not writable; re-tested 2026-10-21) | **no** | **no** | — | none |
| 6 | Delete, one or N | proven (`DELETE /{id}`, one per call) | **no** | `RM_SRCE_DELETE` / `RM_DEST_DELETE`, comma list of ids (§4.2.4) | proven: two ids in one message, gone within one second | RCP; Northbound as the second path |

### 5.2 Virtual sources and destinations

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 7 | Create | proven (`virtual: true` on create) | **no** | `RM_*_ADD` with `VIRTUAL="1"` (§4.2.3) | proven: one `RM_SRCE_ADD VIRTUAL="1"` creates the pair, a virtual source and a virtual destination, both named "Virtual 1" | RCP; Northbound as the second path |
| 8 | Read | proven | proven | unchanged | proven | RCP |
| 9 | Update: make an existing IO virtual, and back | accepted, **not applied** (re-tested 2026-10-21) | **no** | **no** | — | none |
| 10 | Delete | proven: deleting either side removes the pair | **no** | `RM_*_DELETE` (§4.2.4) | proven: deleting either side removes the pair, measured in the four combinations (Northbound source, Northbound destination, RCP source, RCP destination) | RCP; Northbound as the second path |

### 5.3 Federation sources and destinations

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 11 | Create | accepted, nothing created (standalone) | **no** | `RM_*_ADD` with `FEDERATION="1"` (§4.2.3) | accepted, not applied: `RESULT ACCEPTED`, the federation collection stays empty (standalone) | RCP; both to prove on a federation |
| 12 | Read | proven (`GET /routemaster/federation-sources`, empty) | **no** | **no** | — | RCP |
| 13 | Update: bind a level | document (2.6.1) | **no** | `SRCE_ASSOC_IP` with `FEDERATION="1"` (§4.1.9) | accepted, not applied: ACK, the source's levels unchanged (standalone) | RCP; to prove on a federation |
| 14 | Update: tags | document (2.6.1) | **no** | `RM_SRCE_TAGS` with `FEDERATION="1"` (§4.2.1) | accepted (ACK); no read-back on a standalone server | RCP; to prove on a federation |
| 15 | Delete | document (2.6.1) | **no** | `RM_*_DELETE` with `FEDERATION="1"` (§4.2.4) | not run: nothing to delete on a standalone server | RCP; to prove on a federation |
| 16 | Link an existing local IO to a federation one, and unlink | accepted, not applied (standalone; `federationUid`, 0 unlinks) | **no** | **no** — `FEDERATION="1"` addresses the federation IO, it does not link a local one | — | RCP; to prove on a federation |

### 5.4 Levels

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 17 | Create | **no** (`/routemaster/levels` absent; re-tested 2026-10-21) | proven (`LEVEL_MNE` on an unused id creates the level, empty) | unchanged | proven: `LEVEL_MNE` on id 5 created "ZZ-PROBE-L", listed by Northbound and by RCP; no server stop this time | Northbound |
| 18 | Read: names | proven (`/routers/level-mnemonics`) | proven (`OBTAIN LEVEL_MNE`) | unchanged | proven | RCP |
| 19 | Read: properties (device, multiple devices, IP filter, default tags) | **no** (404) | **no** | **no** | — | none |
| 20 | Update: rename | proven | proven | unchanged | proven ("ZZ-PROBE-L" → "ZZ-PROBE-L2") | RCP |
| 21 | Update: properties | **no** | **no** | **no** | — | none — by hand |
| 22 | Delete | **no** | **no** | unchanged | **no**: an empty `LEVEL_MNE` is refused, NACK 8 ONE_OR_MORE_ACTIONS_INVALID; level 5 "ZZ-PROBE-L2" stays | none — by hand in the user interface |

### 5.5 What a source or destination holds on a level

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 23 | Bind a level to a router input or output | proven | proven (`SRCE_ASSOC` / `DEST_ASSOC`) | unchanged; `FEDERATION="1"` for a federation IO (§4.1.7–§4.1.8) | local: proven earlier; federation variant: not run (no router device bound on this system) | RCP |
| 24 | Bind a level to an IP sender or receiver | proven | proven (`SRCE_ASSOC_IP` / `DEST_ASSOC_IP`) | unchanged; `FEDERATION="1"` (§4.1.9–§4.2.0) | proven on 2.9.3: a source bound to sender VTX-01 and a destination to receiver VRX-01 of the NMOS slot 1, read back over RCP as `NMOS\|1\|VTX-01` and `NMOS\|1\|VRX-01`; federation variant: accepted, not applied (row 13) | RCP |
| 25 | Tags on a level | proven | proven (`RM_SRCE_TAGS` / `RM_DEST_TAGS`) | unchanged; `FEDERATION="1"` (§4.2.1–§4.2.2) | local: proven earlier; federation variant: accepted (row 14) | RCP |
| 26 | Route a source to a destination (crosspoint), IP level | proven | proven (`ROUTE`) | unchanged | proven on 2.9.3: a route between two IOs bound to VTX-01 and VRX-01 was executed on the device at once (IS-05 activation on the receiver), while the RouteMaster routes table still showed no crosspoint two seconds later | RCP; Northbound for batches |

### 5.6 Names and categories

| # | Operation | RCP 2.5.3 (server) | Northbound 0v16 | Northbound v0.17 (document) | Northbound v0.17 (server 2.9.3) | Use |
|---|---|---|---|---|---|---|
| 26 | Alternate mnemonic: set a value | only for a name that exists in Cerebrum (400 otherwise; re-tested 2026-10-21) | acknowledged; reads back as not available | unchanged | — | RCP |
| 27 | Alternate mnemonic: create, list, remove the names | **no** | **no** | **no** | — | none — in the user interface |
| 28 | Categories: create, read, update, delete, members | **no** | proven | unchanged | proven | Northbound |

## 6. What v0.17 adds, message by message

| # | Message (v0.17 section) | Attributes | RCP equivalent | Same semantics? |
|---|---|---|---|---|
| 1 | `<ROUTING TYPE="RM_SRCE_ADD \| RM_DEST_ADD" COUNT VIRTUAL FEDERATION/>` (§4.2.3) | `COUNT` 1–1000 (default 1), `VIRTUAL` 0/1, `FEDERATION` 0/1 | `POST /routemaster/sources` / `destinations` with `count`, `virtual`; `POST /routemaster/federation-sources` / `federation-destinations` | yes: append N IOs; neither returns the created ids; a virtual add creates the source and destination pair on both |
| 2 | `<ROUTING TYPE="RM_SRCE_DELETE \| RM_DEST_DELETE" SRCE_ID \| DEST_ID="21,22,23" FEDERATION/>` (§4.2.4) | comma list of existing ids, no empty element, no 0 | `DELETE /routemaster/…/{id}`, one id per call | yes, with a batch of ids on Northbound; a virtual pair is removed by either of its ids on both interfaces |
| 3 | `<ROUTING TYPE="RM_SRCE_ADD" FEDERATION="0"><RESULT VALUE="ACCEPTED \| FAILED"/></ROUTING>` (§4.2.5) | `ACCEPTED` = validated and queued as a configuration change, not applied | `202 Accepted` | yes, the same "accepted proves nothing"; on this server the change is visible within one second |
| 4 | `FEDERATION="1"` on `SRCE_ASSOC`, `DEST_ASSOC`, `SRCE_ASSOC_IP`, `DEST_ASSOC_IP` (§4.1.7–§4.2.0) | sends the change to the federation IO instead of the local one; defaults to 0 | the `levels` block of a federation IO | yes |
| 5 | `FEDERATION="1"` on `RM_SRCE_TAGS`, `RM_DEST_TAGS` (§4.2.1–§4.2.2) | same | the `tags` of a federation IO's level | yes |

Nothing in v0.17 concerns levels: `LEVEL_MNE` is unchanged, there is
still no level delete and no level properties. Not in v0.17 and not in
RCP 2.5.3: making an existing IO virtual, linking a local IO to a
federation one, level delete and properties, tie line groups, creating
the alternate mnemonic names. For sources and destinations the two
interfaces now offer the same create, delete, virtual, federation,
binding and tag operations, and 2.9.3 applies them.

## 7. Behaviour to know when automating Northbound v0.17

| # | Behaviour | Consequence | What our scripts do |
|---|---|---|---|
| 1 | `RESULT VALUE="ACCEPTED"` means validated and queued, not applied (§4.2.5); on 2.9.3 the change is visible within one second | "accepted" proves nothing by itself | every add and delete is read back over RCP |
| 2 | A create does not return the ids it created; the server names them "Src N", "Dest N", "Virtual N" | the caller cannot know them | lists before and after; a new IO is the last id |
| 3 | `VIRTUAL="1"` creates a pair: a virtual source and a virtual destination of the same name; deleting either side, on either interface, removes both | one message, two IOs, and one delete for the two | the pair is deleted by one id |
| 4 | Ids are positions; a delete renumbers the IOs after it, and a pair deletion renumbers both collections | an id noted before a delete may point at another IO after it: in our first run the destination noted as 17 had become 16 and the delete of 17 hit nothing | one delete at a time, ids re-read before each; IOs matched by mnemonic |
| 5 | The reply to an `RM_*` action is a `<ROUTING>` envelope, not the plain `<ACK>` of every other action | a client that waits for an ACK waits forever | the connector accepts both; an older server's ACK is reported as accepted |
| 6 | `FEDERATION` is absent from the mnemonic and lock actions | a federation IO's mnemonic is not set over Northbound | mnemonics stay on RCP |
| 7 | `COUNT` is capped at 1000 and ids at 2147483647; empty list elements and 0 are rejected | a script must validate before sending | the verbs refuse those values before the wire |
| 8 | A level created over Northbound cannot be deleted by any interface (NACK 8 on an empty name); a level create stopped the server once on 2.9.2, not on 2.9.3 | a level test leaves a level behind | the level case runs last, on an unused id; the level is named for deletion in the user interface |
| 9 | A route on IOs bound to IP senders and receivers acts on the devices at once (IS-05), even on IOs created seconds before; the routes table lags | a test route on a bound IO is a real connection | the receiver's state is read before any route and restored after; test routes use unbound IOs |

## 8. What we would ask EVS for

| # | Request | Interface | Why |
|---|---|---|---|
| 1 | The created ids in the `RESULT` of `RM_*_ADD` | Northbound | safe automation with several clients |
| 2 | A completion signal, or `FAILED` when a queued change is not applied | Northbound | reliable reporting, the same request as for RCP |
| 3 | Level delete and level properties | either | levels provisioned and removed without the user interface |
| 4 | `FEDERATION` on `SRCE_MNE` / `DEST_MNE` | Northbound | the mnemonic of a federation IO without RCP |
| 5 | A link / unlink of an existing local IO to a federation IO | either | moving a resource between local and federated without delete and re-create |
| 6 | The document version on the wire (`api_ver` still reports 0.1) | Northbound | a client that knows which messages the server accepts |

## 9. Conclusions

| # | Statement |
|---|---|
| 1 | v0.17 brings Northbound to the same RouteMaster surface as RCP 2.5.3 for sources and destinations: create (with count), delete, virtual, federation, level binding and tags; Cerebrum 2.9.3 applies the local and virtual operations within one second. |
| 2 | For levels nothing changes: create and rename on Northbound (proven on 2.9.3), rename on RCP, no delete and no properties on either; the delete attempt is refused with NACK 8. |
| 3 | The "accepted, not applied" semantics are the same on both interfaces; our scripts read every write back over RCP, whichever interface wrote it. |
| 4 | The federation variants are accepted and not applied on a standalone server, on both interfaces; they are proven only on a federation. |
| 5 | What neither interface offers is unchanged: making an existing IO virtual, linking a local IO to a federation one, level delete and properties, tie line groups, alternate mnemonic names. |
| 6 | RCP stays our default; Northbound v0.17 is the second path for create and delete, and the only one for level create and categories. |
| 7 | The measurement is one Ansible play on temporary IOs, run twice with the same answers; it is run again on every Cerebrum release and the result is a new dated version of this note. |
