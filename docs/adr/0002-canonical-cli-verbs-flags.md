# ADR-0002 — Canonical CLI verbs and flags per role

Status: accepted

## Context

Operators learn one connector's CLI and expect every other connector
of the same role to follow the same shape. Different verbs or flag
names per protocol break Ansible playbook reuse, scripting muscle
memory, and operator training.

## Decision

Every connector binary exposes the **same canonical verb set per
role**. Per-protocol verbs are allowed only as **additions**, never
as replacements or aliases of canonical verbs.

### Canonical verbs

| Role | Verbs |
|---|---|
| consumer | `discover` · `connect` · `disconnect` · `info` · `walk` · `get <path>` · `set <path> <value>` · `subscribe <path>` · `unsubscribe <path>` · `status` · `ensure` · `replay <frames.jsonl>` |
| producer | `serve` · `stop` · `status` · `peers` · `tree` · `ensure` · `replay <frames.jsonl>` |
| registry | `serve` · `stop` · `status` · `peers` · `dump` · `ensure` |
| admin (every binary) | `version` · `--help` · `license install` · `license show` · `license verify` · `license features` · `completion <shell>` |

### Canonical flags (same semantics every connector)

| Flag | Meaning |
|---|---|
| `--host <addr>` | peer host |
| `--port <int>` | peer or local port |
| `--listen <addr>` | server bind addr |
| `--registry <url>` | NMOS-style registry origin (where applicable) |
| `--api-ver <v>` | wire protocol version (where applicable) |
| `--config <path>` | config file (overrides flag defaults) |
| `--output <fmt>` | `text` (default) / `json` (Ansible-friendly) / `yaml` |
| `--log-format <fmt>` | `text` / `json` (Loki/Promtail) |
| `--metrics-addr <addr>` | Prometheus scrape endpoint bind |
| `--license <path>` | override default license location |
| `--timeout <dur>` | per-call deadline |
| `--verbose` / `-v` | log level escalation |
| `--state <present\|absent>` | desired state for `ensure` (see ADR-0007) |
| `--check` | dry-run for `ensure` (see ADR-0007) |
| `--as-client` / `--as-server` / `--validate-only` | `replay` mode (see ADR-0021) |
| `--realtime` / `--delay <dur>` | `replay` timing (see ADR-0021) |
| `--continue-on-mismatch` / `--stop-at <note>` | `replay` mismatch handling (see ADR-0021) |
| `--capture <path>` | live wire-trace capture (consumer + producer); writes `frames.jsonl` per ADR-0021 |

## Consequences

- One Ansible role template per role; protocol slot is a variable.
- Operators carry skill across protocols (`dhs-emberplus walk` ↔
  `dhs-acp1 walk` ↔ `dhs-probel-sw08p walk`).
- Help text follows the same layout in every binary.
- Scripts parse JSON output the same way regardless of which
  connector.

## Forbidden

- Aliasing a canonical verb under a protocol-specific name
  (e.g. `dhs-emberplus browse` instead of `walk`).
- Renaming canonical flags per protocol.
- Skipping a canonical verb because "this protocol does not need it"
  — implement as a stub returning a documented `not_supported` error
  if literally inapplicable.
