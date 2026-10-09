# CLAUDE.md — rcp (EVS Cerebrum RCP)

Atomic per-protocol context. Read the root `CLAUDE.md` first.

## Sources

| File | What it is |
|---|---|
| `assets/Cerebrum RCP API-2_6_1.json` | OpenAPI 3.0.3, API **2.6.1** — the source for RouteMaster. Marked "initial beta release … subject to change" by EVS. |
| `assets/CerebrumRCPV2.html` | the same API rendered at **0.2.5** (33 paths). It has no RouteMaster and no routers section; it adds nothing to the JSON. |

The lab Cerebrum (`10.6.250.5`) answers API **2.5.3** — older than the
document. Where the two differ, the server is what a client meets; each
difference is listed below and, where the client absorbs it, counted.

## Scope

Built (`dhs consumer rcp …`): the session; RouteMaster sources / destinations
in the four collections; crosspoints (`routes`, `take`); the router mnemonic
tables; device discovery and device objects; and `ensure`, which converges
the RouteMaster on a plan for `ansible/playbooks/rcp-routemaster-provision.yml`.

Not built: the device-class areas (switchers, mixers, graphics, cameras,
clip players, lighting, multiviewer), locks, salvos, signal paths, `/batch`,
and the WebSocket back-channel.

Consumer-only: Cerebrum is the server; there is no "serve RCP" role.

The note sent to EVS about 2.5.3 against 2.6.1 is
[`docs/evs-rcp-2.5.3-vs-2.6.1.md`](docs/evs-rcp-2.5.3-vs-2.6.1.md).

## Transport

| | |
|---|---|
| Base | `http://<host>:<port>/v2` (`https` with "Use SSL") |
| Ports | set in Cerebrum: *Enable RCP* → HTTP Port, and Web-Socket Port = HTTP + 1. Lab: **9080 / 9081**. The document's 8080 / 443 are examples. |
| Bodies | JSON; every answer is an object echoing the request id as `reqid` |
| `reqid` | mandatory request **header**, an integer |
| Auth | `Authorization: Bearer <token>` from `POST /login`; `GET /api` needs none |
| Errors | `{"reqid":N,"error":{"code":C,"message":"…"}}` with a 4xx status |
| Writes | answer **202** — accepted, not applied |

The HTTP port is served by Windows HTTP.sys on Cerebrum's behalf (process
`System`), so the firewall allowance of `Cerebrum.exe` does not cover it:
it needs its own inbound rule. The WebSocket port is opened by
`Cerebrum.exe` itself.

## Session

| Call | Purpose |
|---|---|
| `GET /api` | connectivity + version |
| `POST /login` `{username,password}` | → `{token, websocketPort}`. The password goes in clear (the document says "will be encrypted", undecided). The account is a Cerebrum user — the northbound one works. |
| `GET /login` | the logged-in user |
| `POST /heartbeat` | keep-alive; no interval is documented |
| `DELETE /login` | logout |

## RouteMaster

Four collections, each its own id space:

| Collection | Holds |
|---|---|
| `sources`, `destinations` | local IOs — numeric ids allocated by Cerebrum |
| `federation-sources`, `federation-destinations` | federation IOs — ids assigned by the federation |

The same five calls on each: `GET` (ids, as strings), `POST` (create
`count` from one template), `GET /{id}`, `PATCH /{id}` (only the supplied
fields), `DELETE /{id}`.

**Virtual is not a collection.** It is a flag on an IO — and on the
server a virtual IO is a **pair**: creating a virtual source creates a
virtual destination with it, renaming one renames the other, deleting
one deletes the other.

A local IO is linked to a federation IO by `federationUid` (0 = not
linked). An IO is configured per level (`levels` → `level_<id>`): tags or
`tagsInherit`, a device binding (`name` + `typeId` + `deviceLevel` + `io`,
or an IP `senderReceiver` — never both), or `clear`.

## What Cerebrum 2.5.3 does that the document does not say

Measured on `10.6.250.5`, 2026-10-09.

| # | Document 2.6.1 | Server 2.5.3 | Client |
|---|---|---|---|
| 1 | header names are case-insensitive (HTTP) | `reqid` must be lower-case; `Reqid` → 400 "reqid missing from message headers" | sends it lower-case |
| 2 | a single IO answers under `source` / `destination` | answers under `sources` / `destinations` | reads both; counts `rcp_single_io_key` |
| 3 | `GET /routemaster/levels`, `/levels/{id}` | 404 "Unknown RouteMaster collection" | not offered; level names are read from the IOs |
| 4 | `tieLineGroup` on a destination create | 400 "Unknown field in the create request body" | sent as asked; the field is read-only on this server (see 12) |
| 5 | `PATCH {"virtual":true}` makes an IO virtual | 202, and the IO is unchanged | none — the caller reads back |
| 6 | federation create | 202 on a standalone, and nothing is created | `create` fails: no new id appeared |
| 7 | `POST /heartbeat` has no body | a POST without `Content-Length` gets 411 from HTTP.sys, as HTML | declares length 0; counts `rcp_error_not_enveloped` if it happens |
| 8 | version parts are strings, two of them | numbers, three (`patchVersion`) | reads either |
| 9 | `typeId` examples are small positive ids | `-1073741824` for the SW-P-08 router | int64 |
| 10 | `alternateMnemonics` keys are free | a name not configured in Cerebrum → 400, and the whole PATCH is refused | none |

| 11 | ids are stable (the example list is sparse) | **an id is a position**: deleting an IO moves every IO after it down by one; routes follow | IOs are found by mnemonic; the collection is re-read after every delete |
| 12 | `tieLineGroup` on a destination update | 400 "Unknown field in the update request body" | sent as asked |
| 13 | mnemonic update body is the row | 400 "Error parsing request"; accepted keyed as `{"src_7":{…}}` | sends it keyed |
| 14 | route take, mnemonic update, object write answer 202 | 200 | any 2xx |
| 15 | object read answers 200, `current` a string | 202; a number, boolean or string; an enum as its index with `allowedValues` | `current` kept as sent |
| 16 | an object path that does not exist | `{"object":{}}`, the same as a group node | reported as "no value here" |

A virtual's source and destination do not share an id (source 9 with
destination 8 was seen). The routes table reads source 0 on a level a
destination does not exist on, and a large changing value (4294967284 …
4294967294) on the fourth level of a virtual: neither is a source.

A `clear` on a level the IO has no entry on leaves an entry carrying the
level's default device and no `io`.

## Routes, mnemonics, objects

| Call | Purpose |
|---|---|
| `GET /devices` | registered devices, their sub-devices (slots) and resources |
| `GET /devices/{name}/{index}/routers/routes[?DestinationId=N]` | crosspoints: `dest_<id>` → `destLevel_<id>` → source |
| `PATCH …/routers/routes` | take: `{"dest_6":{"source":{"id":7}}}` (all levels) or per level under `levels` |
| `GET / PATCH …/routers/{level,source,destination}-mnemonics[/{id}]` | the mnemonic tables |
| `GET / PUT /devices/{name}/{index}/object/{dotted.path}` | one device object; PUT body `{"value":"…"}`, always a string |

The RouteMaster is device `Cerebrum`, index 0. Its slots 1–4 are sub-devices
(`00:System` is index 0, `02:Services` is index 2). There is no endpoint to
list a device's objects: a path has to be known.

Categories are not in RCP; the provisioning play converges them over the
northbound connector (`cerebrum-nb import --cat-src/--cat-dst`), whose rows
name IOs by id — so it resolves ids from mnemonics after the ensure.

## The WebSocket (not built)

Measured, since the document says "details to follow": the first message
must be `{"headers":{"requestId":"N"},"token":"<token>"}`. Anything else
gets `400 Token missing from authentication request`; a wrong token gets
`400 Invalid access Token`. The token is the one from `POST /login` —
there is no login on the socket.

## What NOT to do

- Never keep an id across a delete: every IO after the deleted one has a
  new id. Find IOs by mnemonic.
- Never take a 202 as "done". Read the IO back.
- Never use the position of an id in a list as an index: the order is a
  display order only.
- Never assume a created IO's id: the create returns none. List before
  and after.
- Never create a virtual source *and* a virtual destination for one
  signal: one create makes both.
- Never send `virtual: true` beside other fields on a create — the
  document makes them mutually exclusive. Create, then update.
