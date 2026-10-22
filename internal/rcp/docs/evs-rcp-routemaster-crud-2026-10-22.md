# Cerebrum RCP 2.5.3 — RouteMaster create, read, update, delete measured on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone, no federation, `GET /v2/api` → `2.5.3`
**Document used:** *EVS Cerebrum RCP*, OpenAPI 3.0.3, version 2.6.1
**Interface used:** RCP only, for every write and every read-back (`GET` after each write)
**Method:** automated and repeatable; every case runs on a temporary source `ZZ-PROBE-S` or destination `ZZ-PROBE-D` created first and deleted last; the collections are counted before and after
**Previous notes:** 2026-10-09 / 2026-10-10 (server 2.5.3 against document 2.6.1) and 2026-10-21 (re-test on 2.9.3: the 147 `GET` operations and the six write paths, unchanged)

## 1. Why we are writing

This note isolates one subject of our previous notes: the RouteMaster
sources and destinations, local, virtual and federation, and the
levels, as created, read, updated and deleted over RCP on 2.9.3. Each
line was run on the server and read back with a `GET`; the server
answers are quoted as received.

## 2. Result in one line

Local and virtual sources and destinations are created, read, renamed
and deleted over RCP on 2.9.3; the binding of a level to an IP sender or
receiver is accepted and stored but no longer in effect on 2.9.3, where
it was on 2.9.2 build 15432; federation IOs and links are accepted and
not applied on our standalone server; levels can be read and renamed,
not created, deleted or configured; `tieLineGroup` is refused on create
and update, and an alternate mnemonic can only be written under a name
defined in the user interface.

## 3. Sources and destinations

| # | Operation | Request | Server answer | `GET` read-back | Status |
|---|---|---|---|---|---|
| 1 | Create, one or N | `POST /routemaster/sources` `{"count": 1, "mnemonic": "ZZ-PROBE-S"}` | `202 Accepted`, no id returned | one more source, the mnemonic as sent | proven |
| 2 | Read, list | `GET /routemaster/sources`, `GET …/{id}` | `200` | — | proven |
| 3 | Rename | `PATCH …/{id}` `{"mnemonic": "ZZ-PROBE-S2"}` | `202` | `mnemonic.originalMnemonic = "ZZ-PROBE-S2"` | proven |
| 4 | Tie line inhibit | `PATCH /routemaster/destinations/{id}` `{"tieLineInhibit": true}` | `202` | `tieLineInhibit: true` | proven |
| 5 | Tie line group | `POST /routemaster/destinations` with `tieLineGroup` | `400` "Unknown field in the create request body. Supported fields are [count, mnemonic, alternateMnemonics, tieLineInhibit, levels, federationUid, virtual]" | — | **refused** — documented as writable in 2.6.1 |
| 6 | Tie line group on update | `PATCH …/{id}` `{"tieLineGroup": 1}` | `400` "Unknown field in the update request body. Supported fields are [mnemonic, alternateMnemonics, tieLineInhibit, levels, federationUid, virtual]" | `tieLineGroup: 0` | **refused** — documented as writable in 2.6.1 |
| 7 | Delete | `DELETE …/{id}` | `202` | gone within one second | proven |
| 8 | Create virtual | `POST /routemaster/sources` `{"virtual": true}`, no other field | `202` | a source and a destination, both `"Virtual 1"`, both `virtual: true`, the source with `levels: {}` : the pair | proven |
| 9 | Rename one side of the pair | `PATCH /routemaster/sources/{id}` `{"mnemonic": "ZZ-PROBE-V"}` on the virtual source | `202` | the source **and** the destination read `"ZZ-PROBE-V"` | proven |
| 10 | Delete virtual | `DELETE /routemaster/sources/{id}` on the virtual source | `202` | both IOs of the pair gone: sources 17 → 16, destinations 17 → 16 (the destination side, measured on 2026-10-21 on this build: the same) | proven |
| 11 | Make an existing IO virtual | `PATCH …/{id}` `{"virtual": true}` | `202` | `virtual: false` after 5 s | accepted, **not applied** |
| 12 | Create a federation IO | `POST /routemaster/federation-sources` `{"count": 1, "mnemonic": "ZZ-PROBE-F"}` | `202` | the collection stays at 0 | accepted, not applied — standalone server |
| 13 | Link a local IO to a federation one | `PATCH …/{id}` `{"federationUid": 1}` | `202` | `federationUid: 0` | accepted, not applied — standalone server |
| 14 | Bind a level to an IP sender or receiver | `PATCH …/{id}` with `levels.level_N.device.senderReceiver`, on all four levels of a source (`VTX-01`, `ATX-01`, `ATX-02`, `DTX-01`) and of a destination (`VRX-01`, `ARX-01`, `ARX-02`, `DRX-01`), as the bare name stored for the plant's own IOs and as the device-qualified `NMOS\|1\|VTX-01` | `202` every time | every binding stored and read back as sent | stored, **not in effect on 2.9.3**: the RouteMaster user interface shows "Level not configured" on every level of these IOs, as it does for every source and destination of the plant bound before 2.9.3, whose bindings are still stored; in effect on 2.9.2 build 15432 last week |
| 15 | Tags on a level | `PATCH …/{id}` `{"levels": {"level_1": {"tags": ["ZZ-PROBE"]}}}` | `202` | `levels.level_1.tags = ["ZZ-PROBE"]`, `tagsInherited: false` | proven |
| 16 | Alternate mnemonic, mnemonic resource | `PATCH /devices/Cerebrum/0/routers/source-mnemonics/{id}` with `alternateMnemonics: {"Eng": "ZZ"}` | `400` "Error parsing request" | — | **refused** |
| 17 | Alternate mnemonic, IO body | `PATCH /routemaster/sources/{id}` `{"alternateMnemonics": {"Eng": "ZZ-PROBE"}}` | `400` "Unknown alternate mnemonic name in 'alternateMnemonics'" | unchanged; no alternate name exists on our server | **refused** for a name not defined; the names are defined in the user interface, and a write under a defined name is untested here |

## 4. Levels

| # | Operation | Request | Server answer | Status |
|---|---|---|---|---|
| 1 | Read the names | `GET /devices/Cerebrum/0/routers/level-mnemonics` | `200`, five levels with their names | proven |
| 2 | Rename | `PATCH` on the level mnemonic, level 5 `"ZZ-PROBE-L2"` → `"ZZ-PROBE-L3"` and back | `202`, each name read back | proven |
| 3 | Read a level's properties | `GET /routemaster/levels`, `GET /routemaster/levels/{levelId}` | `404` "Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]" (run of 2026-10-21 on this build) | **not on 2.5.3** — documented in 2.6.1 |
| 4 | Create, delete, set properties | — | not in the document | **no** |

## 5. Behaviour to know when automating RCP

| # | Behaviour | Consequence | What our scripts do |
|---|---|---|---|
| 1 | The id of a source or destination is its position; a delete renumbers those after it, and a virtual pair deletion renumbers both collections | an id is not a stable reference | IOs are matched by mnemonic, ids re-read before each delete |
| 2 | A create does not return the id it created | the caller cannot know it | lists before and after |
| 3 | `202 Accepted` is answered before the change is done, and also when it will never be done (rows 11–13) | "accepted" proves nothing | every write is read back |
| 4 | A virtual IO is created with `{"virtual": true}` alone; the server names the pair; the mnemonic is set afterwards and applies to both sides | one name for the pair | the pair is renamed once, on the source |
| 5 | The `reqid` header is matched case-sensitively: `Reqid` is refused with "reqid missing from message headers" | a client library that normalises header names cannot talk to the server | the header is sent as `reqid` |
| 6 | A `400` names the supported fields of the body (rows 5–6) and the unknown alternate name (row 17) | the error is enough to correct the client | — |

## 6. What we ask

| # | Request | Why |
|---|---|---|
| 1 | A server build that implements 2.6.1, or its date; 2.9.3 build 15692 serves 2.5.3 | to validate against the document we were given |
| 2 | `GET /routemaster/levels` and `/routemaster/levels/{levelId}`, then level create, delete and properties | levels provisioned without the user interface |
| 3 | `tieLineGroup` accepted on create and update, as documented | tie line groups without the user interface |
| 4 | Making an existing IO virtual, and back | today only at creation |
| 5 | The created ids returned by `POST` | safe automation with several clients |
| 6 | An error instead of `202` when a write cannot be applied, or a completion signal | reliable reporting |
| 7 | Create, list and remove the alternate mnemonic names | today in the user interface only |
| 8 | A category resource | today Northbound only |
| 9 | The IP binding (`levels.level_N.device.senderReceiver`) in effect again on 2.9.3 as on 2.9.2 build 15432, or an error instead of `202` while it is not | the plant's IP routing through the RouteMaster |

Thank you for RCP; it is our default interface and everything in
section 3 but four rows works on the first try. The measurement behind
this note is automated; we are glad to run it again on any build.
