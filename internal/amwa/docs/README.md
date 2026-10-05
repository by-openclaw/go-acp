# AMWA NMOS — docs

| Doc | What it is for |
|---|---|
| [`consumer.md`](consumer.md) | the controller verbs (`dhs consumer nmos …`), each with a captured run |
| [`provider.md`](provider.md) | the node, the registry and the mirror (`dhs producer nmos serve`, `dhs registry nmos serve / mirror`) |
| [`runbook.md`](runbook.md) | task-oriented: look at a device, route, make a sender emit, run a registry, discovery modes, when something is wrong |
| [`runbook-multi-os.md`](runbook-multi-os.md) | the same on Windows / macOS / Linux |
| [`dod-audit-2026-10-03.md`](dod-audit-2026-10-03.md) | the ADR-0025 audit: what is done, partial, missing — the gap list |
| [`architecture.md`](architecture.md), [`dependencies.md`](dependencies.md) | layers and what may import what |
| [`matrix-compliance.md`](matrix-compliance.md), [`neuron-interop-matrix.md`](neuron-interop-matrix.md), [`cerebrum-interop.md`](cerebrum-interop.md) | what real peers do, measured |
| [`use-cases.md`](use-cases.md), [`ha.md`](ha.md), [`dns-sd-unbound.md`](dns-sd-unbound.md), [`firewall-recipes.md`](firewall-recipes.md), [`vlan600-migration.md`](vlan600-migration.md) | plant set-ups |

Wire format, versions in scope and "what NOT to do": [`../CLAUDE.md`](../CLAUDE.md).
The full flag reference of every verb: [`docs/cli.md`](../../../docs/cli.md).

## Coverage — what is tested, on which binary, against whom

"Released" means the binary the fleet runs. A verb that has only run in
unit tests, or only on a build nobody runs, says so.

### Controller (`dhs consumer nmos`)

| Verb | Spec | Unit | On the released binary, against an oracle | Not yet |
|---|---|---|---|---|
| `discover` | IS-04 DNS-SD | yes | — | not in the integration suite |
| `walk` | IS-04 Query API, Node API | yes | v0.37.0: nmos-cpp registry and the Neuron CONVERT — the same ids as the peer's own API; a Registry walk reads the lower minors too (#1330) | — |
| `watch` | IS-04 Query WebSocket | yes | v0.37.0: nmos-cpp registry — subscription opened, first grain; a Node registering and leaving printed as added and removed | — |
| `connect` | IS-05 | yes | v0.37.0: dry-run on the CONVERT (device untouched); connect + disconnect, a scheduled connect (active at its time, not before) and a two-route salvo on the nmos-cpp node, each read back from its IS-05 | a scheduled connect and a salvo on a device |
| `set` | IS-05 (Sender) | yes | v0.37.0: a Sender's two legs moved on the nmos-cpp node and moved back, each read from its IS-05 | — |
| `events` | IS-07 | yes | v0.37.0: a subscription on the nmos-cpp node delivers the Source's state, of the type its Events API reports | MQTT |
| `map` | IS-08 | yes | v0.37.0: a channel routed and unrouted on the nmos-cpp node, read from its own active map | a scheduled activation; a device |
| `compat` | IS-11 | yes | v0.37.1: read, dry-run, constrain, refuse, release against the NMOS-Reference Node (`amwa-interop-is11.yml`); refused by name on the nmos-cpp node, which has no IS-11 | its conversation is not in the replay set yet |
| `config` | IS-14 | yes | v0.37.0: get, set and back on the nmos-cpp node; a backup the node validates for a restore | a restore applied; a device |
| `control` | IS-12 / MS-05-02 | yes | v0.37.0: the model listed is the nmos-cpp node's IS-14 role paths (38 objects); a set read back through its IS-14; a watch prints a change made through its IS-14 | invoke; a device |
| `facade` | AMWA testing façade | yes | v0.37.0: the AMWA tool's controller suites (see the sweep) | — |
| `export`, `audit`, `probe`, `registers` | plant tooling | yes | — | not in the integration suite |

### Node (`dhs producer nmos serve`)

| What | Evidence |
|---|---|
| Every API the node serves, scored by the AMWA NMOS Testing Tool | [`tests/integration/nmos/amwa/results-fleet/`](../../../tests/integration/nmos/amwa/results-fleet/README.md) |
| Registers into the nmos-cpp registry, is held by its heartbeats, deregisters on stop | v0.37.0, `internal/amwa/integration/node_test.go` |

### Registry and mirror (`dhs registry nmos serve`, `… mirror`)

| What | Evidence |
|---|---|
| Registration + Query API, plain and with authorization, scored by the AMWA tool | the sweep, IS-04-02 entries |
| The same exam through the mirror's served face | the sweep, mirror entry |
| An nmos-cpp Node registering into our registry: 91 resources document for document, held by its heartbeats, paged Query, WebSocket grain, registered again after a 404, expired 10 s after a kill | v0.37.0, `internal/amwa/integration/peer_test.go` |
| Our mirror carrying that Node into the nmos-cpp registry: level at the fill and after a live registration, 0 refused, announced on that registry's WebSocket, gone when the mirror stops | v0.37.0, same test |
| Our mirror copying the nmos-cpp registry into ours: 92 resources, document for document, nothing refused | v0.37.0, `TestMirrorCopiesTheOracleRegistry` (failed on one document up to v0.36.0, #1338) |

The verdict lines of both plays on v0.37.0:
[`tests/integration/nmos/amwa/results-fleet/integration-v0.37.0.md`](../../../tests/integration/nmos/amwa/results-fleet/integration-v0.37.0.md).

### How to run it

```bash
# the conformance sweep (AMWA tool), every scope
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-validate.yml

# the integration suite (released CLI against nmos-cpp and a device)
GOOS=linux GOARCH=amd64 go test -c -tags integration -o <dir>/amwa-integration.test ./internal/amwa/integration/
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-integration.yml -e amwa_suite_dir=<dir>

# the registry and the mirror scored by an nmos-cpp Node and Registry
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-interop-nmos-cpp.yml -e amwa_suite_dir=<dir>
```

### Fixtures, replay set, dissector

[`../testdata/README.md`](../testdata/README.md): the committed Node fixture and Controller plant, one captured conversation per message kind with its tree through [`../wireshark/dhs_nmos.lua`](../wireshark/dhs_nmos.lua), and the tests that replay them.

What is still open is listed, gap by gap, in the audit.
