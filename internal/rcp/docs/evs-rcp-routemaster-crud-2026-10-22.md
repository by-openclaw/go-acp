# Cerebrum RCP 2.5.3 — RouteMaster create, read, update, delete measured on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-22
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), standalone, no federation, `GET /v2/api` → `2.5.3`
**Document used:** *EVS Cerebrum RCP*, OpenAPI 3.0.3, version 2.6.1
**Interface used:** RCP only, for every write and every read-back (`GET` after each write)
**Previous notes:** 2026-10-09 / 2026-10-10 (server 2.5.3 against document 2.6.1) and 2026-10-21 (re-test on 2.9.3: the 147 `GET` operations and the six write paths, unchanged)

## 1. Why we are writing

This note isolates one subject of our previous notes: the RouteMaster
sources and destinations, local, virtual and federation, and the
levels, as created, read, updated and deleted over RCP on 2.9.3. Each
line was run on the server and read back with a `GET`; nothing comes
from the document alone unless the table says so.

## 2. Result in one line

Local and virtual sources and destinations are fully managed over RCP
on 2.9.3; federation IOs and links are accepted and not applied on our
standalone server; levels can be read and renamed, not created,
deleted or configured; two documented fields are refused.

## 3. Sources and destinations

| # | Operation | Request | Server answer | `GET` read-back | Status |
|---|---|---|---|---|---|
| 1 | Create, one or N | `POST /routemaster/sources` `{"count": 1}` | `202 Accepted`, no id returned | one more IO, named by the server | proven |
| 2 | Read, list | `GET /routemaster/sources`, `GET …/{id}` | `200` | — | proven |
| 3 | Rename | `PATCH …/{id}` `{"mnemonic": …}` | `202` | new mnemonic | proven |
| 4 | Tie line inhibit | `PATCH …/{id}` `{"tieLineInhibit": true}` | `202` | `true` | proven |
| 5 | Tie line group | `POST` / `PATCH` with `tieLineGroup` | `400` Unknown field in the create / update request body | `tieLineGroup` 0 | **refused** — documented as writable in 2.6.1 |
| 6 | Delete | `DELETE …/{id}` | `202` | gone within one second | proven |
| 7 | Create virtual | `POST` `{"virtual": true}` | `202` | a virtual source and a virtual destination of the same name: the pair | proven |
| 8 | Delete virtual | `DELETE /routemaster/sources/{id}` or `DELETE /routemaster/destinations/{id}` | `202` | both IOs of the pair gone, whichever side was deleted | proven |
| 9 | Make an existing IO virtual | `PATCH …/{id}` `{"virtual": true}` | `202` | `virtual` still false after 5 s | accepted, **not applied** |
| 10 | Create a federation IO | `POST /routemaster/federation-sources` | `202` | the collection stays empty | accepted, not applied — standalone server |
| 11 | Link a local IO to a federation one | `PATCH …/{id}` `{"federationUid": 1}` | `202` | `federationUid` 0 | accepted, not applied — standalone server |
| 12 | Bind a level to a router input / output or to an IP sender / receiver | `PATCH …/{id}` with `levels.level_1.device` | `202` | the binding, e.g. `"senderReceiver": "NMOS\|1\|VTX-01"` | proven |
| 13 | Tags on a level | `PATCH …/{id}` with `levels.level_1.tags` | `202` | the tags | proven |
| 14 | Set an alternate mnemonic | `PATCH /devices/Cerebrum/0/routers/source-mnemonics/{id}` | `400` Error parsing request for a name that does not exist on the server | — | **refused** — the names are created in the user interface only |

## 4. Levels

| # | Operation | Request | Server answer | Status |
|---|---|---|---|---|
| 1 | Read the names | `GET /devices/Cerebrum/0/routers/level-mnemonics` | `200` | proven |
| 2 | Rename | `PATCH` on the level mnemonic | `202`, read back | proven |
| 3 | Read a level's properties | `GET /routemaster/levels`, `GET /routemaster/levels/{levelId}` | `404` Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations] | **not on 2.5.3** — documented in 2.6.1 |
| 4 | Create, delete, set properties | — | not in the document | **no** |

## 5. Behaviour to know when automating RCP

| # | Behaviour | Consequence | What our scripts do |
|---|---|---|---|
| 1 | The id of a source or destination is its position; a delete renumbers those after it, and a virtual pair deletion renumbers both collections | an id is not a stable reference | IOs are matched by mnemonic, ids re-read before each delete |
| 2 | A create does not return the id it created | the caller cannot know it | lists before and after |
| 3 | `202 Accepted` is answered before the change is done, and also when it will never be done (rows 9–11) | "accepted" proves nothing | every write is read back |
| 4 | The `reqid` header is matched case-sensitively: `Reqid` is refused with "reqid missing from message headers" | a client library that normalises header names cannot talk to the server | the header is sent as `reqid` |

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

Thank you for RCP; it is our default interface and everything in
section 3 but three rows works on the first try. The measurement behind
this note is automated; we are glad to run it again on any build.
