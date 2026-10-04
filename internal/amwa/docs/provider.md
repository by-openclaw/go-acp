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

Not yet proven against a third-party Node registering into it (audit
gaps R1 / R2).

## Mirror

```
dhs registry nmos mirror --source http://<plant-registry> --target http://<other-registry> \
    --audit-log mirror-audit.jsonl --status-addr :9101
```

`/status.json` carries the counters to watch. `failures` and `skipped`
stand still on a healthy mirror. `resyncs` counts ordered passes: from
the release after v0.35.0 one is expected when a Node registers (its
children are held until the target holds their parents, then sent in
order), and more than that says something was refused or evicted. On
the plant after the v0.35.0 converge: 7 275 forwarded, 0 failures,
0 resyncs; on v0.35.0 a Node registering afterwards still cost refused
POSTs before a repair (#1340, fixed, not released).
Reading the audit trail: [`runbook.md`](runbook.md), "Reading the
mirror's audit trail".
