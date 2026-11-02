# CLAUDE.md — virtuoso (Nevion Virtuoso MI)

**Never run on a real Virtuoso by this connector so far.** Built and proven
on a fake only. The first real contact is the operator's, at a site, by
[`docs/runbook.md`](docs/runbook.md).

## Authoritative spec

- *Nevion Virtuoso MI User Guide*, chapter "Guide to the REST API" (printed
  pages 83–105), in `assets/` (not in git, stays out of git).
- The device's own `schema.json` is the data dictionary. "The API is subject
  to change with new software releases": nothing here lists fields by hand.
- `assets/` also holds customer captures and scripts. **Nothing from it is
  ever committed**: no IP, device name, serial, UUID or user name. Every
  fixture in `testdata/` is synthetic. The file `assets/ps1/nevion-restapi.md`
  is never opened: it holds a credential.

## What the device is

A chassis with an Element Manager and up to 8 extension cards. Each module has
its own API: `/api/...` on the manager, `/slot/N/api/...` (N 1..8) on a card,
"identical in operation" with a different document. Here slot 0 is the manager
and slots 1..8 the cards. HTTP 80, HTTPS 443.

## Session

- `POST /api/login` with HTTP Basic; the answer sets cookie `NEVION_SESID`.
- The cards refuse direct login: the cookie is the only way to a card.
- A session is removed "after 3 minutes since last HTTP request".
- `POST /api/logout` with the cookie.
- The connector logs in once at Connect, re-logs in once on a 401, and logs
  out at Disconnect. The login exchange never goes through the capture
  recorder: a capture must not hold the Authorization header.

## Wire model

- A URL path maps 1:1 to the JSON document; the answer nests from `data` down
  every level of the path. GET at any level; `depth` limits it (not used).
- Every answer is `{"data":…,"info":{"status","errors","warnings","notes"}}`.
  `status` is `ok` or `failed`; `failed` has at least one error; warnings and
  notes may come with `ok`. "Anything except 200 means the operation was
  unsuccessful." An entry is `{"status":-1,"source":"","detail":"…"}`.
- `schema.json` has no envelope. Top `properties` + `definitions`, `$ref`
  `#/definitions/<name>` (the guide shows `#/<name>`).
- Field: `type` (integer, boolean, string, number), `title`, `description`,
  `readOnly`, `minimum`, `maximum`, `enum` + `labels`, `format`, `pattern`,
  `unit`, `default`.
- Collection: a map. The device serves `{"type":"object","additionalProperties":…,
  "allowInsert","autoKey"}`; the guide describes `{"type":"array","items":…}`.
  The codec takes both. Keys are the item's `required` properties, joined with
  `:` (`0.0.0.0:0:10`). The guide says keys are not among the value fields;
  a real capture repeats some of them.
- RPC: an object with `"rpc":true`, a `result` property for what it returns,
  invoked by POST. **The connector lists RPCs and invokes none.**

## Writes

- One field: `PATCH <object path>?<field>=<value>` ("Changing a single
  value"). Examples fix the text: `dstudpp=12000`, `enable=true`,
  `label=office%20network`.
- The answer's `data` is a change document (`_e`: `i`, `u`, `d`). The
  connector does not rely on it: it reads the field back.
- Not used: POST (insert), PUT, DELETE, RPC. The guide's `&level=2` appears
  only on a PUT of network settings: open question in the runbook.

## Watch

No push channel is documented, so `watch` polls through
`internal/consumer/pollwatch`: at most one read per 200 ms, one request in
flight, default interval 30 s, stretched to what the gap can serve.

## Time limits

One limit: the wait for one answer, 20 s, the value in the operator's capture
scripts. A request keeps the caller's cancel and drops the caller's deadline,
because a whole-command deadline would let one dead card cost the cards after
it. `--timeout` therefore cuts nothing on this connector.

## Folder layout

```
codec/      stdlib only: envelope, product info, schema + join, paths, PATCH attributes
consumer/   package virtuoso: Plugin, registry name "virtuoso"
docs/       README.md, consumer.md, runbook.md
testdata/   synthetic schema + data
assets/     the vendor guide and customer captures (not in git)
```

## Status against ADR-0025

| Deliverable | State |
|---|---|
| Consumer | done against the guide on a fake: info, walk, get, set, watch, export |
| Producer | not done, not asked for |
| Integration | **missing**: no vendor emulator, no real device, no Ansible play |
| DM + manifest fixture | partial: synthetic schema + data in `testdata/fixtures` |
| Wireshark dissector | not done |
| Replay fixtures | missing: needs a capture from a site, scrubbed |

## What NOT to do

- Never send a request to a real device from an agent session.
- Never hand-write a list of fields: the object list is the served schema.
- Never treat an `ok` PATCH as applied: read it back.
- Never invoke an RPC from `set`: `dev/system/do_reset` reboots the chassis.
- Never put a credential in a flag, a log line or a capture.
- Never commit anything from `assets/`.
- Never poll faster than the gap: this runs on production chassis.
