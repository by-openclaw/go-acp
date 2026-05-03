# ADR-0016 — Multi-OS support per connector

Status: accepted

## Context

Customer fleets run Windows Server + Debian + Ubuntu + RHEL + Rocky +
macOS. A connector that works on only one OS is unshippable.
Per-OS dependency variations (Avahi vs Bonjour vs stdlib) need
explicit identification, fallback paths, and runbooks.

## Decision

Every connector MUST build green and pass tests on Windows + Linux +
macOS in CI. Per-OS dep variations are explicit and documented.

### Build matrix

| OS | CI runner | Required to be green |
|---|---|---|
| Linux | `ubuntu-latest` | yes |
| macOS | `macos-latest` | yes |
| Windows | `windows-latest` | yes |

A red CI on any OS blocks the PR. Skipping one OS to "simplify" is
forbidden.

### Per-OS dep variations

Use Go build tags to express platform-specific implementations:

```go
//go:build linux
// session_linux.go    — Avahi DBus implementation

//go:build darwin || windows
// session_bonjour.go  — Bonjour CGo implementation (subject to wrapper conflict; see below)

//go:build !linux && !darwin && !windows
// session_stdlib.go   — pure-Go stdlib floor (universal fallback)
```

### Stdlib floor — non-negotiable

Every connector keeps a pure-Go stdlib path that works on every OS,
even when no OS-native daemon is reachable (slim containers, Windows
hosts without Bonjour, systemd-less Linux). Daemon delegation is an
upgrade for performance + spec-perfect timing, never a hard
requirement.

### Per-OS dep identification

Every per-OS dep entry in `0005-deps.json` (per ADR-0005) carries:

- OS, lib name, version
- Pure Go vs CGo flag
- Fallback path
- CVE history + transitive dep count
- Per-OS install command

### Per-connector deliverables

| File | Required content |
|---|---|
| `internal/<proto>/COMPLIANCE.md` | per-OS dep matrix + per-OS firewall rules (per ADR-0008) |
| `internal/<proto>/runbook-multi-os.md` | per-OS install + uninstall + validate steps |
| Build-tag files | per-OS implementations |
| CI workflow | matrix entry for ubuntu / macos / windows |

### CGo wrapper conflict

ADR-0005 forbids CGo wrappers. Bonjour on macOS / Windows requires
CGo (`-framework CoreServices` / `dnssd.dll`). Two consistent
resolutions:

**Path A — strict no-wrapper**:
- macOS + Windows fall back to the stdlib mDNS floor
- slower than Bonjour-delegated, but pure Go everywhere

**Path B — Bonjour CGo as explicit ADR-0005 exception**:
- Bonjour CGo entries documented in `0005-deps.json` with full audit
- ship full daemon-delegated path on every OS

The conflict is unresolved as of this ADR's acceptance. The
implementation PR for each connector that needs DNS-SD must pick one
path and document it in that connector's `runbook-multi-os.md` +
`COMPLIANCE.md` per-OS dep matrix. The choice may differ per
connector.

### `info` verb reports current backend

`dhs-<proto> info --json` includes:

```json
{
  "os": "linux",
  "arch": "amd64",
  "dnssd_backend": "avahi-dbus"   // or "bonjour-cgo" / "stdlib-mdns"
}
```

So operators see at a glance which backend is active.

## Consequences

- Every connector ships on every customer OS.
- Backend selection is explicit and observable via `info`.
- Stdlib floor is the safety net — no host configuration can
  break the connector.
- CI matrix catches regressions on any OS.

## Forbidden

- Dropping one-OS support to "simplify" — that's a regression.
- Silent platform divergence (no implicit `runtime.GOOS` checks
  outside per-OS-tagged files).
- Removing the stdlib floor.
- Failing CI on one OS while merging on the others.
