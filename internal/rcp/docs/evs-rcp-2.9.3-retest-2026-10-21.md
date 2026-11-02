# Cerebrum RCP — re-test of the integration feedback on 2.9.3 build 15692

**From:** BY-SYSTEMS SRL — integration team
**To:** EVS Cerebrum support
**Date:** 2026-10-21
**Server tested:** Cerebrum 2.9.3 build 15692, web client 2.9.3 build 2222 (staging VM, 10.6.250.5), RCP enabled, `GET /v2/api` → `2.5.3`
**Document used:** *EVS Cerebrum RCP*, OpenAPI 3.0.3, version **2.6.1** ("Cerebrum RCP API-2_6_1.json")
**Previous note:** *Cerebrum RCP — integration feedback: server 2.5.3 against API document 2.6.1*, 2026-10-09 / 2026-10-10, tested on 2.9.2 build 15432

## 1. Why we are writing

We installed Cerebrum 2.9.3 on the same staging server and re-ran every
measurement of our note of 2026-10-09, with the same client, the same
document and the same parameters, to see which of the points we raised
had moved. Every statement below was measured on the server on
2026-10-21; nothing is assumed.

## 2. Result in one line

The RCP API served by 2.9.3 is the one 2.9.2 served: version 2.5.3,
the same 147 read answers endpoint by endpoint, and the same six write
answers. None of the points of our note has changed. The licence of
this server has Route Master, TieLines, IP-Shuffle (128), Salvos,
Tally Manager and Federation Systems enabled, so none of the refusals
below is a licence matter.

## 3. Read coverage of the 2.6.1 document

The 147 `GET` operations of the document were called once each, with
an id of 1, the Cerebrum device for routers and signal paths, our NMOS
device for device objects, and a nil uuid.

| Answer | 2.9.2 b15432 | 2.9.3 b15692 |
|---|---:|---:|
| 2xx — the endpoint exists | 134 | 134 |
| 404 "not available yet" / "object was not found" — exists, no such object on our system | 7 | 7 |
| 404 "RouteMaster IO not found" — federation IOs, none on this system | 2 | 2 |
| 404 "Unknown RouteMaster collection" — `/routemaster/levels`, `/routemaster/levels/{levelId}` | 2 | 2 |
| 404 on `/views/signal-paths/…/{uuid}` — exists or not, we cannot tell | 2 | 2 |

The two level endpoints of 2.6.1 are still absent; the server still
lists its collections as `[sources, destinations, federation-sources,
federation-destinations]`.

## 4. Write paths

Each case was run on a temporary source and destination created for
it and deleted afterwards; the collections were the same size before
and after.

| # | Case (section of the note) | Request | 2.9.3 answer | Read back | Status |
|---|---|---|---|---|---|
| 1 | `tieLineGroup` on create (§4.2) | `POST /routemaster/destinations` with `tieLineGroup` | `400` Unknown field in the create request body; supported: count, mnemonic, alternateMnemonics, tieLineInhibit, levels, federationUid, virtual | — | unchanged |
| 2 | `tieLineGroup` on update (§4.2) | `PATCH /routemaster/destinations/{id}` with `tieLineGroup` | `400` Unknown field in the update request body | `tieLineGroup` = 0 | unchanged |
| 3 | Make an existing IO virtual (§4.3) | `PATCH /routemaster/sources/{id}` `{"virtual": true}` | `202` Accepted | `virtual` = false after 5 s | unchanged |
| 4 | Federation IO on a standalone server (§4.4) | `POST /routemaster/federation-sources` | accepted | the collection stays empty | unchanged |
| 5 | Federation link on a local IO (§4.4) | `PATCH /routemaster/sources/{id}` `{"federationUid": 1}` | `202` Accepted | `federationUid` = 0 | unchanged |
| 6 | Alternate mnemonic field (§6.5) | `PATCH /devices/Cerebrum/0/routers/source-mnemonics/{id}` with an alternate mnemonic | `400` Error parsing request | — | unchanged |

Case 6 is the same answer as before for a field that does not exist on
the server; the fields are still created only in the UI (Tally and
Router Config View → Alternate Mnemonics).

Levels (§4.1) were not re-tested by writing: a level created over the
Northbound API cannot be deleted by any API, and a create stopped the
server once on 2.9.2 (§6.11 of the note).

## 5. Northbound

The Northbound API answers as before on everything we use: 4 levels,
15 sources, 15 destinations, 7 categories, the same device list, the
IP routing of our four NMOS nodes read twice with no difference, and
our RouteMaster plan converges with no change. The login reply reports
`api_ver 0.1`. We have not received a Northbound document newer than
0v16, so we cannot say whether anything was added there.

## 6. Status of the fifteen requests of our note

| # | Request | 2.9.3 |
|---|---|---|
| 1 | A server build that implements 2.6.1 | open — 2.9.3 b15692 serves 2.5.3 |
| 2 | `GET /routemaster/levels`, level create / update / delete | open |
| 3 | `tieLineGroup` writable | open |
| 4 | Stable IO identifiers; created ids returned by `POST` | open — ids are still positions, a create still returns no id |
| 5 | A completion signal for accepted writes; an error instead of `202` when a write is not applied | open — cases 3, 4 and 5 above |
| 6 | The WebSocket message formats | open |
| 7 | The `/batch` request form | open |
| 8 | An object browse endpoint | open |
| 9 | Confirmation of the server / document differences | open |
| 10 | Level delete, device, IP filter, default tags on either API | open |
| 11 | A "move finished" signal for a SubID change | open |
| 12 | Confirmation of the "Forget" procedure | open |
| 13 | `query.downgrade` in Network Media | open |
| 14 | How a Neuron node chooses its announced addresses | open |
| 15 | Create, list, remove the alternate mnemonic fields | open |

## 7. What we ask

The same fifteen points, unchanged. The measurement behind this note
is automated on our side and takes a few minutes; we are glad to run it
again on any build you point us at, and to share every request and
answer behind each line.
