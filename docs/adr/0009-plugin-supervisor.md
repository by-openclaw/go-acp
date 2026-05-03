# ADR-0009 — Plugin supervisor: `hashicorp/go-plugin`

Status: accepted

## Context

dhs v1 (C#) supported hot un/register of connectors at runtime via
`Assembly.LoadFrom` into the default `AppDomain`. This UX is
mandatory for the Go rewrite: fleets must add / remove / upgrade /
rollback connectors without restarting the orchestrator process. The
chosen mechanism must work on Windows + Linux + macOS (per ADR-0016)
and be commercially clean.

## Decision

The plugin supervisor uses **`github.com/hashicorp/go-plugin`** for
hot un/register, version coexistence, drain, and rollback.

Each connector binary (per ADR-0001) is a standalone process. The
optional `dhs-core` supervisor spawns connector children, communicates
via gRPC over stdio, and manages lifecycle.

### Properties

| Property | go-plugin guarantee |
|---|---|
| Process isolation | each child is a separate OS process |
| Cross-OS | Windows + Linux + macOS, all native Go |
| Hot register | spawn child + handshake + register |
| Hot unregister | drain in-flight calls + grace timeout + kill |
| Version coexistence | v1.0 + v1.1 children running side-by-side as separate processes |
| Rollback | activate prior version + drain new + kill new |
| Crash isolation | child panic does not kill core |
| License | MPL-2.0 — commercially clean; bundled distribution OK |

### Implementation-test requirements (must be green in the
implementation PR; failures are bugs, not signals to abandon
go-plugin)

| # | Test |
|---|---|
| 1 | Spawn child + RPC call succeeds |
| 2 | Drain — in-flight call completes before kill (no torn requests) |
| 3 | Multi-version coexistence — v1.0 + v1.1 child processes side-by-side |
| 4 | Rollback — register v1.0, register v1.1, drain v1.1, kill v1.1; v1.0 still running |
| 5 | Crash isolation — child panic does not kill core |
| 6 | Cross-OS — same tests pass on Windows + Linux + macOS (CI matrix) |
| 7 | `govulncheck` + `osv-scanner` clean on `go-plugin` and its transitive deps |
| 8 | `go-licenses` confirms MPL-2.0 and compatible chain |

### CLI surface (per ADR-0002 admin verbs)

| Verb | Effect |
|---|---|
| `dhs core plugin list` | every registered plugin: `(proto, version, instances, status)` |
| `dhs core plugin register <path>` | spawn child + register; idempotent on duplicate hash |
| `dhs core plugin unregister <proto> <version>` | drain + kill; idempotent on unknown |
| `dhs core plugin migrate <proto> --from <v> --to <v>` | drain old, activate new |
| `dhs core plugin rollback <proto>` | reverse of migrate |
| `dhs core plugin status <proto>` | running children + bound peers + metrics |
| `dhs core plugin logs <proto> --follow` | stream per-plugin log buffer |

## Consequences

- Hot un/register works without writing custom IPC.
- Process isolation gives crash containment (a buggy connector
  cannot take down core).
- Versioning is per child process; rollback is `kill new + spawn
  old`.
- One additional external dep (`hashicorp/go-plugin`), tracked in
  ADR-0005 manifest.

## Forbidden

- Using the Go `plugin` package (Linux/macOS only — fails ADR-0016
  multi-OS).
- Custom hand-rolled IPC bypassing go-plugin's gRPC contract.
- Letting children crash the core (no shared address space).
