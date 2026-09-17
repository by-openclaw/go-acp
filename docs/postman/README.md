# dhs NMOS Postman collection

A committed Postman collection (Collection Format v2.1.0) covering every
HTTP surface of the dhs AMWA NMOS implementation. Every request URL and
method is derived from the actual Go route registrations — the Node
provider (`internal/amwa/provider/`), the Registry
(`internal/amwa/registry/`) and the mirror status endpoint — not from
the AMWA spec documents.

## What it covers

| Folder | Surface | Source |
| --- | --- | --- |
| IS-04 Node API | `/x-nmos/node/{ver}/…` + `PUT receivers/{id}/target` | `internal/amwa/provider/node.go` |
| IS-05 Connection API | `/x-nmos/connection/{ver}/…` single + bulk, stage/activate flow | `internal/amwa/provider/connection.go` |
| IS-07 Events | `/x-nmos/events/{ver}/…` REST side | `internal/amwa/provider/events.go` |
| IS-08 Channel Mapping | `/x-nmos/channelmapping/{ver}/…` incl. map activations | `internal/amwa/provider/channelmapping.go` |
| IS-09 System | `/x-nmos/system/{ver}/{,global}` (separate listener, `--role system`) | `internal/amwa/provider/system.go` |
| IS-11 Stream Compatibility | `/x-nmos/streamcompatibility/{ver}/…` incl. constraints + EDID | `internal/amwa/provider/streamcompat.go` |
| IS-14 Configuration | `/x-nmos/configuration/{ver}/rolePaths/…` | `internal/amwa/provider/configuration.go` |
| Registry - Registration API | `/x-nmos/registration/{ver}/…` resource + health | `internal/amwa/registry/registration.go` |
| Registry - Query API | `/x-nmos/query/{ver}/…` resources + subscriptions | `internal/amwa/registry/query.go` |
| Registry Mirror | `/status.json` on `--status-addr` | `internal/amwa/registry/mirror_audit.go` |

Plain listing/index GETs carry a small test script asserting HTTP 200 +
`Content-Type: application/json`. SDP (`…/transportfile`) and EDID
routes serve `application/sdp` / `application/octet-stream` and carry
no JSON assertion. Requests parameterised by a blank-by-default id
variable carry no tests either — fill the id first.

## How to import

1. Postman → **Import** → drop both files:
   - `dhs-nmos.postman_collection.json`
   - `dhs-nmos.postman_environment.json`
2. Select the **dhs NMOS lab (VLAN600 plant)** environment (top-right).
3. Fill the blank id variables (`node_id`, `sender_id`, …) from a
   listing GET (e.g. *IS-04 Node API → GET senders* or *Registry -
   Query API → GET nodes*), then run the per-id and mutating requests.

Newman works too:
`newman run dhs-nmos.postman_collection.json -e dhs-nmos.postman_environment.json`
(expect failures on requests whose id variables are still blank).

## Variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `node_url` | `http://10.6.250.101:8080` | Node API base (`dhs producer nmos serve`, default `--bind :8080`) |
| `registry_url` | `http://10.6.250.101:8235` | Registry base (`dhs registry nmos serve`, default `--bind :8235`) |
| `system_url` | `http://10.6.250.101:10641` | IS-09 System API base (`dhs producer nmos serve --role system`, default `--bind :10641` — a separate listener from the Node API) |
| `mirror_status_url` | `http://10.6.250.5:9101` | Mirror status (`dhs registry nmos mirror --status-addr :9101`, plant default in `ansible/roles/dhs_amwa_plant/defaults/main.yml`) |
| `is04_ver` | `v1.3` | IS-04 Node API wire minor |
| `is05_ver` | `v1.1` | IS-05 minor (v1.0/v1.1/v1.2 are all mounted in parallel) |
| `is07_ver` | `v1.0` | IS-07 minor |
| `is08_ver` | `v1.0` | IS-08 minor |
| `is09_ver` | `v1.0` | IS-09 minor |
| `is11_ver` | `v1.0` | IS-11 minor |
| `is14_ver` | `v1.0` | IS-14 minor |
| `query_ver` | `v1.3` | Query API minor (v1.0–v1.3 mounted in parallel) |
| `registration_ver` | `v1.3` | Registration API minor (v1.0–v1.3 mounted in parallel) |
| `node_id` / `device_id` / `source_id` / `flow_id` / `sender_id` / `receiver_id` | *(blank)* | IS-04 resource UUIDs |
| `input_id` / `output_id` | *(blank)* | IS-08 / IS-11 input and output ids |
| `activation_id` | *(blank)* | IS-08 map activation id |
| `subscription_id` | *(blank)* | Query API subscription id |
| `role_path` | `root` | IS-14 role path (dot-separated below `root`) |
| `property_id` / `method_id` | *(blank)* | IS-14 property/method ids (e.g. `1p6`, `1m1`) |

## WebSockets — not in the collection

This collection format carries HTTP requests only. The WebSocket
surfaces served by the same processes are:

- **IS-12 / MS-05 control protocol (ncp)** —
  `ws://10.6.250.101:8080/x-nmos/ncp/v1.0`
  (`internal/amwa/provider/ncp.go`; same listener as the Node API,
  `wss://` when TLS is armed).
- **IS-07 event WebSocket** —
  `ws://10.6.250.101:8080/x-nmos/events/v1.0/ws`
  (`internal/amwa/provider/events.go`).
- **Query API subscription sockets** —
  `ws://10.6.250.101:8235/x-nmos/query/{ver}/subscriptions/{id}/ws`;
  the concrete `ws_href` comes back from *POST subscription*
  (`internal/amwa/registry/registry.go` upgrade dispatcher).

Use Postman's separate WebSocket-request feature (or
`dhs consumer nmos events` / `dhs consumer nmos watch`) for those.
