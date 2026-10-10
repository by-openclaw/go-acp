# Cerebrum RCP — integration feedback: server 2.5.3 against API document 2.6.1

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-09, updated 2026-10-10
**Server tested:** Cerebrum 2.9.2 build 15432 (staging), RCP enabled, `GET /v2/api` → `2.5.3`
**Document used:** *EVS Cerebrum RCP*, OpenAPI 3.0.3, version **2.6.1** ("Cerebrum RCP API-2_6_1.json")

## 1. Why we are writing

We are building an automation client on top of RCP to provision and
operate RouteMaster — sources, destinations, virtuals, federation IOs,
mnemonics and crosspoints — from Ansible. RCP is a very good fit for
this: it is clean, consistent and easy to script, and almost everything
we needed worked on the first day.

Our server answers API 2.5.3 while the document we were given describes
2.6.1. This note lists, precisely, where the two differ, what we could
not reach yet, and a few points where a short clarification from you
would let us cover the whole scope. Every statement below was measured
on the server; nothing is assumed. We are happy to re-run any test, or to
test a newer build, at your convenience.

## 2. What works well today

All of this was run against the server, with each write read back:

| Area | Result |
|---|---|
| Session | `GET /api`, `POST /login`, `GET /login`, `POST /heartbeat`, `DELETE /login` |
| RouteMaster IOs | list, read, create (single and `count` with incrementing mnemonics), update, delete — local sources and destinations |
| Per-level configuration | device IO binding, tags, `clear` |
| Virtual IOs | create, read, rename, delete |
| Crosspoints | read the routes table (with the `DestinationId` filter), take on all levels |
| Mnemonic tables | read level / source / destination mnemonics; update a source mnemonic |
| Device objects | read values on any registered device and on the Cerebrum sub-devices (slots); write a value and read it back |
| Device discovery | `GET /devices`, `GET /deviceMap` |
| Locks, salvos | read (empty on our system) |

A full provisioning plan (4 IOs, a virtual, 3 crosspoints, 2 categories)
is applied, re-applied with no change, removed, and removed again with no
change — the behaviour an operator expects from infrastructure as code.

## 3. Coverage of the 2.6.1 document on 2.5.3

The document describes **172 paths and 268 operations**. We called every
one of its **147 `GET` operations** (read-only) on the server.

| Result | Count | Meaning |
|---|---:|---|
| Answered 2xx | 134 | the endpoint exists on 2.5.3 |
| 404 "not available yet" / "object was not found" | 7 | the endpoint exists; our system has no device of that class (audio mixer bands, signal paths) |
| 404 "RouteMaster IO not found" | 2 | the endpoint exists; there is no federation IO with id 1 |
| **404 "Unknown RouteMaster collection"** | **2** | **not available on 2.5.3** — see 4.1 |
| 404 with an empty body | 2 | `/views/signal-paths/…/{uuid}` — exists or not, we cannot tell |

The 121 write operations were exercised only where we have a use for
them and a safe target: RouteMaster, routes, mnemonics, device objects.
The device-class areas (switchers, audio mixers, graphics, cameras, clip
players, lighting, multiviewer) answer their reads on 2.5.3; we have not
written to them and make no statement about them.

## 4. Items of 2.6.1 we could not use on 2.5.3

These are the functions we would like to have enabled, in order of
importance to us.

### 4.1 RouteMaster levels — not available

| Request | Answer |
|---|---|
| `GET /routemaster/levels` | `404` `Unknown RouteMaster collection. Supported collections are [sources, destinations, federation-sources, federation-destinations]` |
| `GET /routemaster/levels/{levelId}` | same |

Levels carry `multipleDevices`, the level's own device, `ipFilter` and
the default source and destination tags. Without them a client cannot
know, before writing, whether a level accepts a device name on an IO or
only an input/output number. Today we read the level name from each IO
and the level mnemonics from `/routers/level-mnemonics`.

*Request:* the two `GET` endpoints of 2.6.1. And, for a complete
provisioning scope, we would welcome create / update / delete of levels,
which the document says is not supported through this API.

What we do today, through the Northbound API, and where it stops:

| # | Level operation | Northbound today |
|---|---|---|
| 1 | Create | works: `LEVEL_MNE` on an unused level id creates the level, empty |
| 2 | Rename | works |
| 3 | Delete | not available: an empty mnemonic is refused; we delete levels by hand in the UI |
| 4 | Set the level's device, "multiple devices", IP filter | not available |
| 5 | Set the default source and destination tags | not available |

*Request:* rows 3 to 5 on either API would close the level scope.

### 4.2 `tieLineGroup` — not writable

| Request | Answer |
|---|---|
| `POST /routemaster/destinations` with `tieLineGroup` | `400` `Unknown field in the create request body. Supported fields are [count, mnemonic, alternateMnemonics, tieLineInhibit, levels, federationUid, virtual]` |
| `PATCH /routemaster/destinations/{id}` with `tieLineGroup` | `400` `Unknown field in the update request body. Supported fields are [mnemonic, alternateMnemonics, tieLineInhibit, levels, federationUid, virtual]` |

The field is returned by `GET` (value 0) and documented as writable in
2.6.1. *Request:* accept it on create and on update.

### 4.3 Making an existing IO virtual — accepted, not applied

`PATCH /routemaster/sources/{id}` with `{"virtual": true}` answers `202`
and the IO is unchanged when read back (also after several seconds).
Creating an IO as virtual works. *Question:* is the update supported on
2.5.3? If not, a `400` would let a client report it.

### 4.4 Federation collections on a system without federation

Our server has no federation yet (it is being installed). On it:

- `GET /routemaster/federation-sources` and `-destinations` → `200`, `[]` — as expected.
- `POST /routemaster/federation-sources` → **`202`**, and no IO is created.

*Request:* an explicit error (for example `409` "federation not
available") instead of `202`, so that automation can tell "accepted and
in progress" from "cannot be done here". We will test the four
federation calls as soon as our federation is up and report back.

### 4.5 `/batch`

`PUT /batch` with the documented body

```json
[{"method":"get","relativeUrl":"/routemaster/sources","body":{}},
 {"method":"get","relativeUrl":"/routemaster/destinations","body":{}}]
```

answers `200` with `{"responses":[{"status":404},{"status":404}]}` (the
document names the member `batch`). *Question:* what is the expected form
of `relativeUrl`, and do the items need their own `reqid` or token? Batch
matters to us for reading large RouteMasters: today reading N IOs costs
N + 1 requests.

### 4.6 Discovering device object paths

`GET /devices/{deviceName}/{deviceIndex}/object/{objectPath}` works well
once the path is known. We found no way to *list* the objects of a
device: 0.2.5 of the document had `GET /devices/{deviceName}/{deviceIndex}/schema`,
which is neither in 2.6.1 nor on the server (`404`). *Question:* is there
an endpoint to browse a device's object tree, or is one planned?

One path form took us a while to find and may deserve a line in the
document: an object inside a table row is addressed with the row key in
square brackets, for example `Nodes.[<node uuid>].SubID` on the `NMOS`
device. Without the brackets the server answers `{"object":{}}`, the same
as for an unknown path.

### 4.7 Categories

RCP has no category endpoint, so we manage categories through the
Northbound API alongside RCP. That works; a category resource in RCP
would let one protocol cover the routing configuration end to end.

## 5. Where the server and the document differ

None of these blocks us — we adapted the client to the server. We list
them so the document and the server can converge, and so that a future
server change does not surprise an integrator.

| # | Topic | Document 2.6.1 | Server 2.5.3 |
|---|---|---|---|
| 1 | `reqid` header | header (HTTP header names are case-insensitive) | matched case-sensitively: `Reqid: 1` → `400` `reqid missing from message headers`. Some HTTP libraries send `Reqid` (Go's standard one does). |
| 2 | Single RouteMaster IO | `{"source": {…}}` / `{"destination": {…}}` | `{"sources": {…}}` / `{"destinations": {…}}` |
| 3 | `GET /api` | `majorVersion`, `minorVersion` as strings | numbers, plus `patchVersion` |
| 4 | Status of a route take, a mnemonic update and an object write | `202` | `200` |
| 5 | Status of `GET …/object/…` | `200` | `202` |
| 6 | Mnemonic update body | the row: `{"originalMnemonic":"…"}` | `400` `Error parsing request`; accepted when keyed like the `GET` answer: `{"src_7":{"originalMnemonic":"…"}}` |
| 7 | Single mnemonic row (`GET …/source-mnemonics/{id}`) | the row | the row under its key: `{"sourceMnemonics":{"src_7":{…}}}` |
| 8 | `originalMnemonic` query filter on `…/source-mnemonics` | returns the matching row | returns every row |
| 9 | Object value | `current` is a string | a number, a boolean or a string; an enum comes as its index (or a boolean) with `allowedValues` |
| 10 | Object path that does not exist | not specified | `202` `{"object":{}}` — the same answer as a group node, so a typing error cannot be told from a folder |
| 11 | Device that does not exist | error object | `404` with `{"reqid":N}` only |
| 12 | `typeId` | examples `1024`, `2048` | `-1073741824` for a Snell SW-P-08 router (a signed 32-bit reading of `0xC0000000`?) |
| 13 | A destination's device block | `disconnect` | shows `ignore` (the source field) and no `disconnect` |
| 14 | `POST` without a body (`/heartbeat`) | no body | a request without `Content-Length` gets `411` from the HTTP layer, as an HTML page rather than the JSON error object |

## 6. Points where your guidance would help us most

### 6.1 IO ids change when an IO is deleted

Deleting source 7 moves source 8 to id 7, source 9 to id 8, and so on.
Routes follow correctly (the route table shows the new id), so the system
stays consistent — but an id read before a delete is no longer valid
after it, for any client.

The document's example id list (`["1","2","3","7","8","9","17"]`)
suggests ids that stay sparse and stable. *Question:* is renumbering the
intended behaviour in 2.5.3, and is it the same in 2.6.1? A stable
identifier per IO (as federation IOs have with their uuid) would make
automation and audit trails much simpler. Meanwhile our client identifies
an IO by its mnemonic and re-reads the collection after every delete.

### 6.2 A create does not return what it created

`POST` answers `202` with the `reqid` only, and the document says to
re-query the id list. With two clients creating at the same time, the new
ids cannot be attributed. *Request:* return the created ids in the
answer (or a `Location` header), or accept a client-supplied reference.

### 6.3 `202 Accepted` — when is it done?

Writes are accepted and applied a moment later; some are accepted and not
applied (4.3, 4.4). A client can only poll. *Question:* is there, or will
there be, a completion signal — an operation status resource, or an event
on the WebSocket back-channel carrying the `reqid`?

### 6.4 Virtual IOs are a source and destination pair

Creating one virtual source also creates a virtual destination with the
same mnemonic; renaming or deleting one does the same to the other. That
is a sensible model. Two details would be worth a line in the document:

- the pair does not share an id (we saw source 9 with destination 8);
- nothing in either IO names its partner — we match them by mnemonic.

*Request:* a field linking the two halves.

### 6.5 Alternate mnemonics

A `PATCH` carrying an alternate mnemonic name that is not configured in
Cerebrum is refused (`400` `Unknown alternate mnemonic name`), and the
whole update with it. That is a good safeguard. We found no endpoint that
lists, creates or removes the alternate mnemonic *names*. *Question:* is
there one, or should they be created in the Cerebrum UI first?

### 6.6 The WebSocket back-channel

The document says the token "will need to be provided (details to
follow)" and that subscriptions are created on the back-channel in
version 2. By experiment we found that the first message must be

```json
{"headers": {"requestId": "1"}, "token": "<token from POST /login>"}
```

and that anything else is answered `400` `Token missing from
authentication request`. *Request:* the message formats for
authentication, subscribe, unsubscribe and events — in particular whether
RouteMaster IOs and routes can be subscribed to.

### 6.7 Session lifetime and security

- `POST /heartbeat`: what interval do you recommend, and after how long
  without one is a session closed?
- The password is sent in clear in `POST /login`, and the document notes
  encryption is still to be decided. We will use "Use SSL" in production;
  is there anything else you recommend?

### 6.8 Reserved source ids in the routes table

On a level a destination does not exist on, the route source reads `0`.
For virtual destinations the fourth level reads a large value that
changes between reads — we saw `4294967292`, `4294967291`, `4294967294`,
`4294967287`, `4294967284`. *Question:* what do these values mean, and
can a client treat every value above the highest source id as "no
source"?

### 6.9 Installation note

The HTTP port is served by the Windows HTTP layer, so the firewall
allowance of `Cerebrum.exe` does not cover it: on our server the
WebSocket port answered and the HTTP port did not, until an inbound rule
for the HTTP port was added by hand. A line in the installation guide, or
a rule added by the installer, would save the next integrator an hour.

### 6.10 Default names on a create without mnemonics

A `POST` that creates an IO without a mnemonic gives it a default name
(`Src 6`, `Dst 6`). That is convenient. For automation it means two
clients can no longer tell their own IO from another's by name.
*Question:* is the default name pattern stable, and can a client rely on
it?

### 6.11 One server stop after a level was created

Once, Cerebrum stopped a few seconds after we created a level through
Northbound (`LEVEL_MNE` on a new id) and then read `GET /routers/routes`
through RCP. We could not reproduce it: with the level present, the same
read works every time, and our playbook now waits 10 s after creating a
level. We mention it in case the log of that moment is useful to you; we
can send the time of the event.

### 6.12 Giving an NMOS node its SubID

We give each NMOS node its SubID from our inventory by writing
`Nodes.[<node uuid>].SubID` on the Network Media Server (device `NMOS`).
The object answers over RCP and over the Northbound API alike, and this
is exactly what we need: thank you.

We first got this wrong on our side, and we mention it so the next
integrator does not: our automation wrote four SubIDs within a few
seconds. A SubID moves the node from slot 00 to its own slot with all
its devices, senders and receivers, and that takes Cerebrum some time.
Writing the next one in the middle of a move made nodes leave and
register again, each coming back with SubID 0.

What works, and has been stable since (2026-10-10), also thanks to your
advice to keep the registry page closed during the operation:

| # | Rule we now follow |
|---|---|
| 1 | one node at a time; one write per node |
| 2 | the next node only when the move is finished: the node's entry on slot 00 lists no device any more, and slot N lists every device with all its senders and receivers |
| 3 | then a quiet period with no request, and one more read |

Times we measured for a move, read from the object tables: about 5 s
for a node with 8 senders and 144 receivers or with 176 and 176, about
15 s for 1 544 senders and 1 544 receivers, about 25 s for a node with
eight devices.

*Questions:*

- Is there a value or an event that tells a client "the move is
  finished" — the counterpart of the busy indicator of the user
  interface? We deduce it from the two tables of rule 2.
- A node that leaves and registers again comes back with SubID 0. Is
  that intended, and can a SubID be kept for a node that returns?
- While the node with 1 544 senders and 1 544 receivers was being moved,
  the registry's Query API once gave no answer for about 10 s and one
  other node left and came back. Is there a sizing guide for the number
  of streams of one node?

### 6.13 Removing a node: "Forget"

Removing a node works well and we could automate it over Northbound:
SubID back to 0, NMOS off on the device, wait until the node's line has
no address (`HRef` no longer available), then `Nodes.[<node uuid>].Forget`
= `1`; the line is gone a few seconds later. *Question:* is `1` the
intended value, and is "the line has no address" the right condition to
wait for?

### 6.14 A node behind an external registry (Network Media)

For one test we registered the devices with our own IS-04 registry and
pointed a *Network Media* device at it. Cerebrum found the registry by
DNS-SD and read it over WebSocket subscriptions at v1.3, and the three
nodes registered at v1.3 appeared at once.

One node (IS-04 v1.2 only) did not appear: a v1.3 query does not return
a v1.2 resource unless the client adds `query.downgrade`. With "Force
Fixed server" ticked and "Fixed Server Version" `v1.2`, the Servers tab
listed no query server and no node server at all, although the registry
announces `api_ver=v1.0,v1.1,v1.2,v1.3`. With Cerebrum's own registry
(*Network Media Server*) the same node shows, as before.

*Questions:* can Network Media ask for older nodes (`query.downgrade`),
or read a registry at a chosen version while still discovering it? And
where is the address of the fixed server entered?

### 6.15 A node that announces a second, unreachable address

One node (Neuron VIEW) at first registered with `href`
`http://10.41.40.160:3000/`, an address on none of its interfaces, and
earlier with `127.0.0.1`; after its NMOS service was switched off and on
it registers with its management address, and Cerebrum reaches it. Its
node document still lists a second endpoint, `172.0.0.1`, and its DNS-SD
name is the library default (`nmos-cpp_node_<address>`), where the other
Neurons announce `neuron-<serial>`. This is on the node side, not on
Cerebrum; we report it here because both are EVS products. *Question:*
how does the node choose the addresses it announces?

On the same unit, after a reboot, the second media port took no DHCP
lease although the switch showed the link up; it recovered after the
port was set to static and back to DHCP.

## 7. Summary of what we ask

| # | Request | Why |
|---|---|---|
| 1 | Access to a server build that implements 2.6.1, or the date it is planned | to validate against the document we were given |
| 2 | `GET /routemaster/levels` (4.1), and level create / update / delete if possible | complete provisioning scope |
| 3 | `tieLineGroup` writable (4.2) | complete destination configuration |
| 4 | Stable IO identifiers, and created ids returned by `POST` (6.1, 6.2) | safe automation with several clients |
| 5 | A completion signal for accepted writes, and an error instead of `202` when a write cannot be applied (6.3, 4.3, 4.4) | reliable error reporting |
| 6 | The WebSocket message formats (6.6) | live state without polling |
| 7 | The `/batch` request form (4.5) | performance on large systems |
| 8 | An object browse endpoint (4.6) | device parameters without prior knowledge of paths |
| 9 | Confirmation of the points in section 5, so we know which side will change | a client that keeps working across versions |
| 10 | Level delete, level device, IP filter and default tags on either API (4.1) | levels provisioned without the UI |
| 11 | A "move finished" signal for a SubID change, a SubID kept for a node that returns, and sizing guidance for large nodes (6.12) | provisioning one device at a time without guessing delays |
| 12 | Confirmation of the "Forget" procedure (6.13) | devices removed as cleanly as they are added |
| 13 | `query.downgrade`, or a chosen version with discovery, in Network Media (6.14) | every node visible behind an external registry |
| 14 | The way a Neuron node chooses the addresses it announces (6.15) | every node reachable from Cerebrum |

Thank you for RCP, and for your time on this. We can share the exact
requests and answers behind every line of this note, and we are glad to
act as a test site for the next build.
