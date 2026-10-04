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
| `walk` | IS-04 Query API, Node API | yes | v0.35.0: nmos-cpp registry and the Neuron CONVERT — the same ids as the peer's own API | lower-minor Nodes in one view (#1330) is not released |
| `watch` | IS-04 Query WebSocket | yes | v0.35.0: nmos-cpp registry — subscription opened, first grain | a change observed live |
| `connect` | IS-05 | yes | v0.35.0: dry-run on the CONVERT (device untouched); connect + disconnect on the nmos-cpp node, read back from its IS-05 | scheduled modes and a salvo on a device |
| `set` | IS-05 (Sender) | yes | — | not in the integration suite |
| `events` | IS-07 | yes | — | not in the integration suite |
| `map` | IS-08 | yes | — | not released; not run against a device |
| `compat` | IS-11 | yes | — | not released; not run against a device |
| `config` | IS-14 | yes | — | not released; not run against a device |
| — | IS-12 / MS-05-02 | — | — | **no client yet** |
| `facade` | AMWA testing façade | yes | v0.35.0: the AMWA tool's controller suites (see the sweep) | — |
| `export`, `audit`, `probe`, `registers` | plant tooling | yes | — | not in the integration suite |

### Node (`dhs producer nmos serve`)

| What | Evidence |
|---|---|
| Every API the node serves, scored by the AMWA NMOS Testing Tool | [`tests/integration/nmos/amwa/results-fleet/`](../../../tests/integration/nmos/amwa/results-fleet/README.md) |
| Registers into the nmos-cpp registry, is held by its heartbeats, deregisters on stop | v0.35.0, `internal/amwa/integration/node_test.go` |

### Registry and mirror (`dhs registry nmos serve`, `… mirror`)

| What | Evidence |
|---|---|
| Registration + Query API, plain and with authorization, scored by the AMWA tool | the sweep, IS-04-02 entries |
| The same exam through the mirror's served face | the sweep, mirror entry |
| An nmos-cpp node registering into our registry | **missing** (audit R1 / R2) |

### How to run it

```bash
# the conformance sweep (AMWA tool), every scope
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-validate.yml

# the integration suite (released CLI against nmos-cpp and a device)
GOOS=linux GOARCH=amd64 go test -c -tags integration -o <dir>/amwa-integration.test ./internal/amwa/integration/
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-integration.yml -e amwa_suite_dir=<dir>
```

What is still missing for ADR-0025 is listed, gap by gap, in the audit.
