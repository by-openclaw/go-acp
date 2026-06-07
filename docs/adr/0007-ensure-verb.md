# ADR-0007 — `ensure --state --check` verb contract

Status: accepted

## Context

Ansible / Puppet / Terraform expect a *declarative* convergence
contract: tell the tool the desired state, the tool figures out what
to do (no-op if already there). Imperative verbs (`serve`, `stop`)
fail when the system is already in the requested state, breaking
playbook idempotency.

## Decision

Every connector exposes an `ensure` verb across all three roles
(consumer / producer / registry — see ADR-0002).

### Flags

| Flag | Semantics |
|---|---|
| `--state <present\|absent>` | desired state |
| `--check` | dry-run; inspect only, apply nothing |
| `--diff` | always include structured `diff` array in output |

### `--state` semantics per role

| Role | `present` means | `absent` means |
|---|---|---|
| consumer | session connected + maintained against `--host`/`--port` | disconnected + cached state cleaned |
| producer | service running with this config | stopped + cached state cleaned |
| registry | service running with this config | stopped + cached state cleaned |

### JSON output (apply mode)

```json
{
  "changed": true,
  "previous": {"state": "absent", "config_hash": ""},
  "current":  {"state": "present", "config_hash": "sha256:abc...",
               "pid": 12345, "listen": "0.0.0.0:9000"},
  "diff": [
    {"field": "state",       "from": "absent",  "to": "present"},
    {"field": "listen_port", "from": null,      "to": 9000}
  ]
}
```

### JSON output (`--check` / dry-run)

```json
{
  "would_change": true,
  "current": {"state": "absent"},
  "target":  {"state": "present", "listen_port": 9000},
  "diff": [
    {"field": "state",       "from": "absent", "to": "present"},
    {"field": "listen_port", "from": null,     "to": 9000}
  ]
}
```

### Exit codes

Per `docs/protocols/error-codes.md` (the error contract): the exit code reflects
**outcome, not change**. Change is signalled only by the `changed` / `would_change`
JSON field — never by the exit code.

| Exit | Meaning |
|---|---|
| 0 | success — applied, or dry-run reported (parse `changed` / `would_change` from JSON) |
| 1 | runtime / wire error — failed to apply or to inspect |
| 2 | usage / validation error (bad `--state`, invalid config) |

### Idempotency rules

| Case | Result |
|---|---|
| running + same `config_hash` + `--state present` | `changed: false`, no-op |
| running + different `config_hash` + `--state present` | `changed: true`, graceful reload (or restart if reload not supported) |
| stopped + `--state present` | `changed: true`, start |
| running + `--state absent` | `changed: true`, stop |
| stopped + `--state absent` | `changed: false`, no-op |
| any + `--state absent` (with `--cleanup`) | tear down state, remove `~/.dhs/<proto>/` cache + license cache |

## Consequences

- Ansible playbooks call `dhs-<proto> ... ensure --state present` and
  parse `changed` for `register` / `notify` semantics.
- Operators can preview changes safely with `--check` before
  applying.
- One declarative verb covers start / stop / restart / no-op.

## Forbidden

- `ensure` mutating state when `--check` is passed.
- Returning `changed: true` when nothing actually changed.
- Skipping `diff` output (always emit, even if empty `[]`).

## Revisions

- 2026-06-07 — Exit-code table corrected (errata per ADR-0015 Amendment
  policy): removed the "exit 2 = changed" convention, which collided with the
  error contract in `docs/protocols/error-codes.md` (exit 2 = usage/validation).
  `ensure` signals change only via the `changed` / `would_change` JSON field;
  exit code reflects outcome (0 ok / 1 runtime / 2 validation). Resolves
  coherence-review C1. — by-rune
