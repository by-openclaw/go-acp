# Cerebrum — RCP and Northbound API: which one for which feature

**From:** RTBF — integration team
**Date:** 2026-10-12
**Status:** draft for review

## 1. Introduction

We automate the provisioning and the operation of EVS Cerebrum with
Ansible scripts. Cerebrum offers two programming
interfaces for this: the Northbound API, which we have used for some
time, and RCP, which EVS now develops as the main interface.

This note states, feature by feature, which of the two interfaces we use
today, which one we intend to use, and what is still missing on either
side. Every "proven" below was run against a real Cerebrum and read back;
nothing is taken from a document alone unless the table says so.

## 2. Context

| # | Item | Value |
|---|---|---|
| 1 | Cerebrum under test | 2.9.2 build 15432, standalone (no federation, no redundancy); re-tested on 2.9.3 build 15692 on 2026-10-21 with the same RCP answers |
| 2 | RCP served by that Cerebrum | **2.5.3** (`GET /v2/api`) |
| 3 | RCP document we received | **2.6.1**, OpenAPI 3.0.3 — 172 paths, 268 operations |
| 4 | RCP back-channel document (live events) | "RCP Backchannel API" **1.0.0** |
| 5 | Northbound API specification | 0v16 (XML over WebSocket) |
| 6 | Devices behind it | Neuron CONVERT Hybrid, Neuron Shuffle, Neuron VIEW, FusioN 6, all registered as NMOS nodes |
| 7 | Our automation | Ansible scripts |

The gap between rows 2 and 3 matters: the document describes a newer
RCP than the server answers. Of the 147 read operations of the document,
134 are answered by the server; the rest are listed in section 6.

## 3. Scope of work

| # | In scope | State |
|---|---|---|
| 1 | NMOS nodes: SubID, move to a slot, removal (Forget) | done, over RCP |
| 2 | RouteMaster levels | done, over Northbound |
| 3 | RouteMaster local sources and destinations, bound to NMOS senders and receivers | done, over RCP |
| 4 | Virtual sources and destinations | create and delete proven earlier, over RCP |
| 5 | Federation sources and destinations, and linking a local resource to them | written to the document; not provable on a standalone Cerebrum |
| 6 | Mnemonics, alternate mnemonics, categories, crosspoints | partly proven; see section 5 |

Out of scope for now, by decision: the device classes of RCP we have no
device for (audio mixers, cameras, lighting, switchers, graphics, clip
players, multiviewers), video servers, batch, salvos over RCP, and the
producer side.

## 4. Reading the tables

| Mark | Meaning |
|---|---|
| **proven** | run on the Cerebrum of section 2, result read back |
| document | described in the document or specification; not run by us, or not provable on our system |
| **no** | not offered by that interface |

For Northbound, "proven" includes what was proven when our Northbound
connector was completed, before this campaign; the rows run again during
this campaign are levels, categories, bindings, tags and device objects.

"Use" names the interface our Ansible scripts use today. RCP is the default: Northbound
is named only where RCP cannot do it.

## 5. Feature by feature

### 5.1 Session and events

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 1 | Login, keep-alive, logout | proven | yes | proven | RCP |
| 2 | Live events on a router: crosspoints, mnemonics, level names | proven (2026-10-12): subscribed to the level names, received the current state, then one event per change | back-channel document 1.0.0 | proven | RCP |
| 2a | Live events on a device object | subscription accepted and current state received; a change not yet observed | back-channel document 1.0.0 | proven | RCP, to complete |
| 2b | Live events on RouteMaster sources and destinations | **no** — refused: "Invalid path routemaster" | not stated | proven for routing; categories and salvos too | Northbound |

### 5.2 Devices and their objects

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 3 | List the devices and their slots | proven | yes | proven | RCP |
| 4 | Add, modify, remove a device | not tried | `POST /devices`, `PUT` and `DELETE /devices/{name}` | available; not re-tested in this campaign | to decide after a test |
| 5 | Read and write one object of a device | proven | yes | proven | RCP |
| 6 | Read several objects in one session | proven | yes | one object per call | RCP |
| 7 | Browse the object tree of a device | **no** | **no** (the `/schema` of 0.2.5 is gone) | only for an object whose path is already known; **no wildcard** | none usable — a browse with a wildcard is missing on both |

### 5.3 NMOS nodes on the Network Media Server

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 8 | Give a node its SubID | proven | device object | proven | RCP |
| 9 | Read a slot: devices, senders, receivers | proven | device object | proven | RCP |
| 10 | Remove a node (Forget) | proven | device object | proven | RCP |

### 5.4 RouteMaster: sources and destinations

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 11 | List and read sources and destinations | proven | yes | proven | RCP |
| 12 | Create and delete a source or a destination | proven | yes | **no** | RCP |
| 13 | Create and delete a virtual source or destination | proven | yes | **no** | RCP |
| 14 | Make an existing source or destination virtual | tested 2026-10-12: accepted, **not applied** (the source still reads `virtual: false`) | yes | **no** | none |
| 15 | Federation sources and destinations: create, read, update, delete | accepted, nothing created (standalone) | yes | **no** | RCP; to be tested on a federation on 2026-10-13 |
| 16 | Link a local source or destination to a federation one, and unlink it | accepted, not applied (standalone) | `federationUid`, 0 unlinks | **no** | RCP; to be tested on a federation on 2026-10-13 |
| 17 | Tie line inhibit | proven | yes | **no** | RCP |
| 18 | Tie line group | **not writable** | writable | **no** | none |

### 5.5 RouteMaster: levels

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 19 | Read the level names | proven | yes | proven | RCP |
| 20 | Create a level | **no** | **no** | proven | Northbound |
| 21 | Rename a level | proven (2026-10-12) | yes | proven | RCP |
| 22 | Delete a level | **no** | **no** | **no** | none — by hand in the user interface |
| 23 | Read a level's properties (its device, multiple devices, IP filter, default tags) | **no** (404) | `GET /routemaster/levels` | **no** | none on 2.5.3 |
| 24 | Set a level's properties | **no** | **no** | **no** | none — by hand |

### 5.6 RouteMaster: what a source or destination holds on a level

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 25 | Bind a level to an input or output of a router device | proven | yes | proven | RCP |
| 26 | Bind a level to an IP sender or receiver | proven | yes | proven | RCP |
| 27 | Bind a level to a sub-channel of a sender or receiver | stored as written; effect not verified | yes | not tried | RCP, to verify |
| 28 | Tags on a level | proven | yes | proven | RCP |

### 5.7 Names and categories

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 29 | Read source, destination and level mnemonics | proven | yes | proven | RCP |
| 30 | Change a source or destination mnemonic | proven for a source | yes | proven | RCP |
| 31 | Set an alternate mnemonic | only when its name already exists in Cerebrum (400 otherwise) | yes | acknowledged; reads back as not available on our system | RCP |
| 32 | Create, list, remove the alternate mnemonic names | **no** | **no** | **no** | none — by hand |
| 33 | Categories: create, modify, delete, members | **no** | **no** | proven | Northbound |

### 5.8 Routing

| # | Feature | RCP 2.5.3 (server) | RCP 2.6.1 (document) | Northbound 0v16 | Use |
|---|---|---|---|---|---|
| 34 | Read the crosspoints | proven | yes | proven | RCP |
| 35 | Make a crosspoint | proven | yes | proven | RCP |
| 36 | Locks on sources and destinations | proven (2026-10-12): a destination locked on one level, read back with its owner, unlocked | yes | proven | RCP |
| 37 | Salvos: list, run | list answered (empty); run not tried | yes | proven | Northbound |
| 38 | Salvos: save, rename, delete | **no** | **no** | proven | Northbound |
| 38a | Salvos: add or remove a crosspoint or an object in a salvo | **no** | **no** | **no** | none — by hand |
| 39 | Several commands in one request (`/batch`) | request form not documented | one line | several crosspoints in one message; nothing else | Northbound, for crosspoints only |
| 40 | Shuffle rules, tie lines of the IP Audio Shuffling option | **no** | **no** | **no** | none — by hand |

## 6. What the 2.5.3 server does not have of the 2.6.1 document

| # | Item | Answer of the server | Effect for us |
|---|---|---|---|
| 1 | `GET /routemaster/levels`, `GET /routemaster/levels/{id}` | 404, "Unknown RouteMaster collection" | a client cannot know whether a level accepts a device before writing |
| 2 | `tieLineGroup` on a destination | accepted, not stored | tie line groups stay manual |
| 3 | `virtual` on an existing source or destination | accepted, not applied | a resource is made virtual at creation only |
| 4 | Federation collections | accepted, nothing created | expected on a standalone system; to prove on a federation |
| 5 | `/views/signal-paths/…/{uuid}` | 404 with an empty body | exists or not, we cannot tell |
| 6 | `/batch` | no request form in the document | reading a large RouteMaster costs one request per resource |

## 7. Behaviour to know when automating RCP

| # | Behaviour | Consequence | What our scripts do |
|---|---|---|---|
| 1 | The id of a source or destination is its position: deleting one renumbers those after it | an id is not a stable reference | resources are matched by mnemonic |
| 2 | A create does not return the id it created | the caller cannot know it | lists before and after, and reports the new ids |
| 3 | A write is answered "accepted" before it is done, and also when it will never be done | "accepted" proves nothing | every write is read back |
| 4 | Giving a node its SubID moves it to a slot, which takes seconds to tens of seconds | a second change during the move disturbs the registry | one node at a time; the next waits until the move is read as finished |

## 8. Missing endpoints: what we would ask EVS for

| # | Request | Interface | Why |
|---|---|---|---|
| 1 | A server that implements RCP 2.6.1, or its planned date | RCP | to validate against the document we were given |
| 2 | Create and delete a level | RCP | today create is Northbound only, delete is by hand |
| 3 | Set a level's properties: device, multiple devices, IP filter, default tags | RCP | levels fully provisioned without the user interface |
| 4 | A category resource | RCP | today Northbound only |
| 5 | Create, list and remove alternate mnemonic names | RCP | today by hand; a write to an unknown name is refused |
| 6 | Tie lines: `tieLineGroup` writable, and the tie line configuration of the IP Audio Shuffling option | RCP | today by hand; it decides whether audio shuffling can be automated |
| 7 | Make an existing source or destination virtual, and back | RCP | today only at creation |
| 8 | Stable identifiers, and the created id returned by a create | RCP | safe automation with several clients |
| 9 | A completion signal for an accepted write, and an error when a write cannot be applied | RCP | reliable reporting |
| 10 | Live events on RouteMaster sources and destinations, categories and salvos | RCP back-channel | today refused on the back-channel; Northbound only |
| 10a | The back-channel as documented: an event arrives as a list and carries the device, where the document shows a single object; one refusal is not valid JSON (quotes inside the message are not escaped) | RCP back-channel | a client written from the document alone does not decode it |
| 11 | An endpoint to browse the object tree of a device, with a wildcard | RCP | today only Northbound, and only for a path already known |
| 12 | The request form of `/batch` | RCP | performance on large systems |
| 13 | Salvos: save, rename, delete; add and remove a crosspoint or an object in a salvo; the tally of its crosspoints | RCP | today save, rename and delete are Northbound only; the content of a salvo is edited by hand |
| 14 | A "move finished" signal when a node gets a SubID | either | provisioning without guessed delays |

## 9. Conclusions

| # | Statement |
|---|---|
| 1 | RCP is our default interface. Everything we do on NMOS nodes and on RouteMaster sources and destinations — local and virtual — goes over RCP and is proven on a real Cerebrum. |
| 2 | Northbound is still required today for four things: creating a level, categories, salvo management (save, rename, delete), and live events on RouteMaster resources, categories and salvos. Live events on routers and device objects work over the RCP back-channel. |
| 3 | Six things are possible through neither interface and remain manual: deleting a level, setting a level's properties, managing alternate mnemonic names, configuring tie lines, editing the content of a salvo, and browsing a device's objects without knowing their path. |
| 4 | Federation is implemented to the 2.6.1 document, including moving a resource between local and federated with one update. It cannot be proven until a federation is available. |
| 5 | The server we have answers RCP 2.5.3 while the document is 2.6.1. Six items of the document are not usable on it (section 6). Making an existing resource virtual was tested again on 2026-10-12: accepted, not applied. |
| 6 | With the requests of section 8 fulfilled, Northbound would no longer be needed for provisioning. |
