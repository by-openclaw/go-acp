# Cerebrum RCP 2.5.3 — RouteMaster create, read, update, delete measured on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone, no federation, `GET /v2/api` → `2.5.3`
**Document used:** *EVS Cerebrum RCP*, OpenAPI 3.0.3, version 2.6.1
**Interface used:** RCP only, for every write and every read-back (`GET` after each write). Where a cell says "user interface", we looked at the RouteMaster pages of the Cerebrum client. No other API is quoted in this note
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
receiver works once the level carries its device and the name is written
as `NMOS|<slot>|<node>|<name>`, the form the user interface stores, while
the bare names 2.9.2 stored were not migrated by the 2.9.3 installation
and the server answers `202` to any string; federation IOs and links are accepted and
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
| 14 | Bind a level to an IP sender or receiver | `PATCH …/{id}` with `levels.level_N.device.senderReceiver`: the bare name 2.9.2 stored for the whole plant (`VTX-01`), the `NMOS\|1\|VTX-01` form, and `NMOS\|1\|lab-convert-01\|VTX-01`, the form the user interface writes | `202` every time, whatever the name | every string stored as sent; with the level carrying its device in the user interface, only `NMOS\|<slot>\|<node>\|<name>` resolves (blue), the two others stay unresolved (red); with the level carrying no device, the state the 2.9.3 installation left all five levels in, nothing resolves | proven in the `NMOS\|<slot>\|<node>\|<name>` form with the level configured; **`202` on an unresolvable name**; **2.9.2 bindings not migrated**: the plant's 24 bindings had to be rewritten |
| 15 | Tags on a level | `PATCH …/{id}` `{"levels": {"level_1": {"tags": ["ZZ-PROBE"]}}}` | `202` | `levels.level_1.tags = ["ZZ-PROBE"]`, `tagsInherited: false` | proven |
| 16 | Alternate mnemonic, mnemonic resource | `PATCH /devices/Cerebrum/0/routers/source-mnemonics/{id}` with `alternateMnemonics: {"Eng": "ZZ"}` | `400` "Error parsing request" | — | **refused** |
| 17 | Alternate mnemonic, IO body | `PATCH /routemaster/sources/{id}` `{"alternateMnemonics": {"Eng": "ZZ-PROBE"}}` | `400` "Unknown alternate mnemonic name in 'alternateMnemonics'" | unchanged; no alternate name exists on our server | **refused** for a name not defined; the names are defined in the user interface, and a write under a defined name is untested here |

## 4. Levels

| # | Operation | Request | Server answer | Status |
|---|---|---|---|---|
| 1 | Read the names | `GET /devices/Cerebrum/0/routers/level-mnemonics` | `200`, five levels with their names | proven |
| 2 | Rename | `PATCH` on the level mnemonic, level 5 `"ZZ-PROBE-L2"` → `"ZZ-PROBE-L3"` and back | `202`, each name read back | proven |
| 3 | Read a level's properties | `GET /routemaster/levels`, `GET /routemaster/levels/{levelId}` | `404` "Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]" (run of 2026-10-21 on this build) | **not on 2.5.3** — documented in 2.6.1 |
| 4 | Create | — | not in the document | **no** |
| 5 | Set the properties of a level: default device, "Multiple Devices", IP filter | — | not in the document: the only documented write on a level is `PATCH …/routers/level-mnemonics/{levelId}` with `originalMnemonic` and `alternateMnemonics` (row 2) | **no** — the UI is the only place |
| 6 | Delete | — | not in the document; the UI only | **no** |

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
| 2 | `GET /routemaster/levels` and `/routemaster/levels/{levelId}`, then level create, delete and properties (default device, "Multiple Devices", IP filter): today the documented read answers `404` and no documented request writes them | levels provisioned without the user interface, which today decide whether any IP binding resolves |
| 3 | `tieLineGroup` accepted on create and update, as documented | tie line groups without the user interface |
| 4 | Making an existing IO virtual, and back | today only at creation |
| 5 | The created ids returned by `POST` | safe automation with several clients |
| 6 | An error instead of `202` when a write cannot be applied, or a completion signal | reliable reporting |
| 7 | Create, list and remove the alternate mnemonic names | today in the user interface only |
| 8 | A category resource | categories cannot be provisioned over RCP |
| 9 | An error instead of `202` when `levels.level_N.device.senderReceiver` does not resolve or the level has no device; the migration of the 2.9.2 bindings at installation; the `NMOS\|<slot>\|<node>\|<name>` form documented for `senderReceiver` | no silent `202`; an upgrade that keeps the plant's IP routing |

## 7. Open points for provisioning the RouteMaster over RCP without the user interface

Federation is left out until our licence is installed; tie line groups and "make an existing IO virtual" are not needed by us. Each line was run on 2.9.3 build 15692 (RCP 2.5.3) on 2026-10-22 and read back with a `GET`; "user interface" means the RouteMaster pages of the Cerebrum client.

| # | Topic | What we need | Measured over RCP | Status |
|---|---|---|---|---|
| 1 | Level: default device, "Multiple Devices", IP filter | a resource that sets them, and their read | no documented request takes them: the only documented write on a level is `PATCH …/routers/level-mnemonics/{levelId}` with `originalMnemonic` and `alternateMnemonics`; the documented read `GET /routemaster/levels` answers `404`. A level shows no device in the user interface until set there, and on such a level a binding is stored and shown "Level not configured" | **open — blocks every provisioning** |
| 2 | Level properties read | `GET /routemaster/levels` and `/{levelId}` as 2.6.1 documents | `404` on the 2.5.3 that 2.9.3 serves | **open** |
| 3 | IP binding name | the form of `levels.level_N.device.senderReceiver` written in the document, and an error on a name that does not resolve | on a configured level, `"NMOS\|1\|lab-convert-01\|VTX-01"` is stored and resolved in the user interface; `"VTX-01"` and `"NMOS\|1\|VTX-01"` are stored as sent and shown unresolved; all answer `202` | **open — silent**: the destination of an unresolved binding never routes |
| 4 | Bindings after the 2.9.3 installation | the bindings of the previous build migrated, or a release note | the plant's 24 bindings read back in the bare form (`"VTX-01"`) and were shown "Level not configured" in the user interface; written again in the form of point 3 (`202` ×24), the second run changes nothing | **open**: an upgrade silently stops the plant's IP routing |
| 5 | `202` means applied | an error when a write cannot be applied, or a completion signal | `202` to a binding that never resolves, to `virtual: true` on an existing IO that stays `false`, to a federation create on a standalone server | **open**: a client cannot trust an answer without a read-back |
| 6 | Created ids | the ids in the answer of `POST /routemaster/sources` / `destinations` | `202`, no id; lists before and after | **open**: unsafe with two clients |
| 7 | Level create and delete | a resource | none in the document; the documented `GET /routemaster/levels` answers `404` | **open** |
| 8 | Alternate mnemonic names | list, create and rename the names | values proven under a name that exists; `400` Unknown alternate mnemonic name otherwise; the names only in the user interface | **open**: the names are a user-interface step |
| 9 | Categories | a resource | none in the document | **open — low** for us |
| 10 | Routes | `PATCH /devices/Cerebrum/0/routers/routes` measured on 2.9.3 | not measured in this campaign | to measure |
| 11 | Register a router or an IP device on the server | `POST /devices` measured on 2.9.3 | not measured in this campaign | to measure once point 1 exists |

Thank you for RCP; it is our default interface and everything in
section 3 but four rows works on the first try. The measurement behind
this note is automated; we are glad to run it again on any build.
