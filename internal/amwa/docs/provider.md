# AMWA NMOS — the node, the registry, the mirror

Three served roles, one binary. How to operate each is in
[`runbook.md`](runbook.md); every flag is in
[`docs/cli.md`](../../../docs/cli.md); what each serves on the wire is
in [`../CLAUDE.md`](../CLAUDE.md). This page says which command is
which role and where its proof is.

| Role | Command | Serves | Proof |
|---|---|---|---|
| Node | `dhs producer nmos serve --config <bundle.json>` | IS-04 Node API, IS-05, IS-07, IS-08, IS-11, IS-12 / MS-05-02, IS-14; registers into a Registry or announces peer-to-peer | the AMWA tool's node suites ([sweep](../../../tests/integration/nmos/amwa/results-fleet/README.md)); `internal/amwa/integration/node_test.go` against the nmos-cpp registry |
| Registry | `dhs registry nmos serve` | IS-04 Registration API and Query API (REST + WebSocket), every minor | the AMWA tool's IS-04-02, plain and with authorization |
| Mirror | `dhs registry nmos mirror --source <registry> --target <registry>` | copies one registry into another and keeps it level; optionally serves a read-only Query face | IS-04-02 through the mirror; the start-up and eviction measurements in issue #1311 |

## Node — the bundle

The node serves what its bundle declares: one JSON file with `node`,
`devices`, `sources`, `flows`, `senders`, `receivers` and the per-API
sections (`connection`, `channel_mapping`, `events`,
`stream_compatibility`). The committed fixture is
[`tests/integration/nmos/amwa/amwa-test-node.json`](../../../tests/integration/nmos/amwa/amwa-test-node.json).

```
dhs producer nmos serve --bind 0.0.0.0:18080 --advertise-host <ip>:18080 \
    --registry http://<registry>:<port> --config amwa-test-node.json
```

Captured with the released v0.35.0 against the nmos-cpp registry
(2026-10-04): the Node and every resource of the bundle are listed by
that registry's Query API; the Node is still there 15 s on, past the
registry's 12 s expiry; it is gone within 5 s of stopping.

## Registry

```
dhs registry nmos serve --bind :8235 --advertise-host <ip>:8235 --priority 0
```

Scored by a third party's Node on v0.40.2 (`amwa-interop-nmos-cpp.yml`):
an nmos-cpp Node registers into it — its 91 resources are in our Query
API document for document, it is held by its heartbeats, paged Query and
the WebSocket's first grain carry its resources, it registers again 5 s
after a 404, and it expires 10 s after a kill.

Up to v0.36.0 the Query API did not show a resource registered at a
higher minor on its lower endpoints (#1337), and returned a re-encoding
of a resource rather than the registered document — a key the Node did
not send could appear with its zero value (#1338). Both are fixed since
v0.36.1: the registry keeps the document a Node sent and serves it,
and an earlier minor is shown it with the keys IS-04 "Upgrade Path"
lists removed. On the plant a v1.2 query now lists all 25 Nodes.

## Mirror

```
dhs registry nmos mirror --source http://<plant-registry> --target http://<other-registry> \
    --audit-log mirror-audit.jsonl --status-addr :9101
```

`/status.json` carries the counters to watch. `failures` and `skipped`
stand still on a healthy mirror. `resyncs` counts ordered passes: from
v0.36.0 one is expected when a Node registers (its children are held
until the target holds their parents, then sent in order), and more
than that says something was refused or evicted. On the plant after
the v0.37.0 converge: 8 101 forwarded for 7 266 resources, 0 failures,
0 skipped — the registry restarts with the mirror and every node
registers again, which costs ordered passes, not refusals (11 929
requests on v0.36.2, before a pass learned to wait for a parent, #1346).
During a full sweep, with about twenty plant-node restarts, the target
still refused 2 requests (100 on v0.35.0).

Paired with nmos-cpp on v0.40.2, both ways: our registry mirrored into
the nmos-cpp registry is level at the fill and after a live
registration with nothing refused, and the nmos-cpp registry mirrored
into ours is level document for document (92 resources).

v0.36.1 is not to be run as a mirror behind a dhs registry: with the
registry translating (#1337) its catalogue read could claim a resource
registered mid-read at a lower minor (#1351 — 81 sources on the plant,
for twenty minutes). v0.36.2 reads the minors lowest first.
Reading the audit trail: [`runbook.md`](runbook.md), "Reading the
mirror's audit trail".

A source registry that guards its Query API (IS-10) is read with a
token: `--source-auth-url <authorization server> --source-auth-client-id
<id> --source-auth-client-secret <secret>` makes the mirror an OAuth
client (client_credentials, scope `query`), and the subscription
requests, their sockets and the REST reads all carry the Bearer token.
`--auth-url` (with `--serve`) is the other side: it guards the Query
face the mirror serves. The legs to the target carry no token.
