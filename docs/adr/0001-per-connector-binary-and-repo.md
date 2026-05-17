# ADR-0001 — Per-connector binary and own repo

Status: accepted

## Context

dhs ships connectors for many protocols. Two questions had to be locked
before any further architecture:

1. Is dhs one binary that hosts all connectors, or one binary per
   connector?
2. Does each connector live in the umbrella repo, or in its own?

Customers run dhs in mixed environments — Linux + Windows + macOS,
on-premise + container — and integrate via Ansible / systemd /
Kubernetes. They expect to install only the connectors they need,
upgrade them independently, and rollback without touching unrelated
connectors.

## Decision

- One Go module **and one binary per connector**: `dhs-acp1`,
  `dhs-acp2`, `dhs-emberplus`, `dhs-probel-sw08p`,
  `dhs-blackmagic-hyperdeck`, etc.
- Each connector lifts to **its own GitHub repository** (one repo per
  module).
- Connectors are independent: a change in `dhs-emberplus` cannot
  recompile or affect `dhs-acp1`.
- A future `dhs-core` supervisor binary may orchestrate multiple
  connector binaries (see ADR-0009), but each connector still ships and
  runs as a standalone binary.

## Consequences

- Customers install only what they need (`dhs-emberplus` binary
  alone, no others).
- Versioning is per-connector — `dhs-emberplus v1.1.0` ships
  independently of `dhs-acp1 v0.7.0`.
- Ansible / systemd manage each connector as its own service unit.
- Repo extraction is trivial: codec packages already live in
  `internal/<proto>/codec/` (per ADR-0006); the rest of
  `internal/<proto>/` lifts cleanly.
- Cross-connector imports are forbidden (`internal/<proto>/*` may
  not import `internal/<other-proto>/*`); only neutral packages
  (`internal/transport/*`, `internal/consumer`, `internal/provider`)
  cross the seam.
- Ops teams orchestrate hot-add / hot-remove via standard process
  management (systemd / Ansible / Kubernetes); see ADR-0009 for the
  optional supervisor pattern.

## Forbidden

- Building a single fat binary that bundles every connector.
- Sharing runtime state across connector processes via shared memory
  or files outside well-defined IPC.
- Cross-connector source imports outside the neutral seam packages.
