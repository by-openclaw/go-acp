# AMWA NMOS — the committed fixture (ADR-0025 deliverable 4)

ADR-0025 asks for a committed DM + manifest the producer builds its
frame from, with the repository alone. An NMOS Node has neither: it is
not a frame of cards with a device model, it is a resource graph — a
node, its devices, sources, flows, senders and receivers, and the seeds
of its per-API state. That graph, as one JSON bundle, is the fixture
form for this connector (recorded in ADR-0025's revisions).

| Role | Fixture | Built from the repo alone by |
|---|---|---|
| Node | [`tests/integration/nmos/amwa/amwa-test-node.json`](../../../../tests/integration/nmos/amwa/amwa-test-node.json) — the bundle `dhs producer nmos serve --config` takes; the sweep, the integration suite and the loopback rig all serve it | `internal/amwa/provider` `TestCommittedNodeFixtureBuildsTheNode` (never skipped) |
| Controller | [`../exports/`](../exports/) — the catalogues of the reference registry and node | `internal/amwa/consumer` `TestWalkReplaysTheCommittedExports` |
| Registry, mirror | none: a registry holds what Nodes register, and the mirror's cache is rebuilt from its source | the loopback rig, `internal/amwa/integration/loopback_test.go` |

The bundle stays where the plays and the container files already read
it; this page is the pointer ADR-0025's layout expects.
