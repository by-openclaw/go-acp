# AMWA NMOS testdata

What the connector is tested against without a host (ADR-0025
deliverables 4 and 6). Captured on the lab fleet on 2026-10-04 with the
released dhs v0.36.2, against the nmos-cpp reference registry and node.

| Folder | What it is |
|---|---|
| `protocol_types/<kind>/` | one captured conversation per message kind, with its tree through our dissector |
| `fixtures/node-registers.pcapng` | the golden scenario: an nmos-cpp Node registering its node, device, sources and flows into the nmos-cpp registry, 120 frames, with its tree |
| `exports/` | the catalogues `dhs consumer nmos walk --json` printed for the reference registry and the reference node — the Controller's committed plant |
| `integration-test/` | where the Node's committed fixture is, and why it is not a DM + manifest |
| `reference-nodes/` | a minimal Node bundle |

## The message kinds

| Kind | API | What the capture holds |
|---|---|---|
| `dnssd-mdns` | DNS-SD | mDNS browse and answers for the NMOS service types, on the lab link |
| `registration-resource` | IS-04 Registration API | a resource registered: `POST /resource` and its 201 |
| `registration-heartbeat` | IS-04 Registration API | heartbeats: `POST /health/nodes/{id}` and the health the registry answers |
| `registration-delete` | IS-04 Registration API | resources deregistered: `DELETE /resource/{type}/{id}` |
| `query-list` | IS-04 Query API | collections listed with `paging.limit` |
| `query-subscription` | IS-04 Query API | a subscription created: `POST /subscriptions` and its `ws_href` |
| `query-grain-ws` | IS-04 Query API, WebSocket | the upgrade and the grains that follow: topic, rows, added / removed / modified |
| `node-api` | IS-04 Node API | a Node read from its own API |
| `connection-single` | IS-05 Connection API | a Receiver staged and activated: `PATCH …/staged`, `GET …/active` — immediate and scheduled |
| `connection-bulk` | IS-05 Connection API | a salvo: `POST /bulk/receivers` |
| `events-ws` | IS-07 Events API, WebSocket | the subscription command and the state and health messages |
| `events-mqtt` | IS-07 Events API, MQTT | CONNECT, then the retained connection-status and state PUBLISHes on `x-nmos/events/v1.0/…` |
| `channelmapping` | IS-08 Channel Mapping API | the map read and a channel routed: `POST /map/activations` |
| `system` | IS-09 System API | `GET /global` |
| `streamcompatibility` | IS-11 Stream Compatibility Management API | a Sender constrained: `PUT …/constraints/active`, its status and active constraints read back |
| `control-ws` | IS-12 Control Protocol, WebSocket | commands and their responses, a subscription and a property-changed notification |
| `configuration` | IS-14 Configuration API | a property read and set: `GET` / `PUT …/properties/{id}/value/` |
| `configuration-bulk` | IS-14 Configuration API | a backup and a validated restore: `GET` / `PATCH …/bulkProperties/` |

Not here, and why: **IS-11** (no third-party IS-11 peer on the fleet —
the nmos-cpp image does not serve it); **IS-10 / BCP-003-01** (the
authorization and TLS windows are encrypted on the wire — the AMWA
tool's suites are their evidence).

## What reads it

- `internal/amwa/testdata_test.go` — every kind has its capture, its
  tree and its page, and the tree carries the dissector's NMOS layer.
- `internal/amwa/integration/dissector_replay_test.go` — replays every
  capture through `wireshark/dhs_nmos.lua` and compares with the
  committed tree (needs a tshark that loads Lua).
- `internal/amwa/consumer/replay_export_test.go` — a walk of the
  exports served back is the exports.

## Re-capture

```bash
# on the control node and the tooling host, while the two plays run
dumpcap -i any -w nmos.pcapng -f 'tcp port 8110 or tcp port 8111 or tcp portrange 8120-8122 or udp port 5353'
# one conversation, and its tree
tshark -r nmos.pcapng -Y 'tcp.stream == N' -w protocol_types/<kind>/capture.pcapng
tshark -X lua_script:../wireshark/dhs_nmos.lua -r protocol_types/<kind>/capture.pcapng \
    -O dhs_nmos,dhs_nmos_http > protocol_types/<kind>/tshark.tree
```
