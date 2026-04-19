# Scenario test catalogue

Declarative error / edge-case replay tests. Each `.json` file describes one scenario; the harness at `tests/unit/scenario/` runs every file automatically.

Full package docs: [`internal/scenario/`](../../internal/scenario/).

## Layout

```
tests/scenarios/
├── README.md
├── <protocol>/
│   └── <scenario>.json
```

## Scenario file shape

```json
{
  "name": "human-readable one-liner, used as sub-test name",
  "protocol": "acp2",
  "wire_file": "tests/fixtures/acp2/err_no_access.jsonl",
  "expect_events": ["acp2_access_denied_received"],
  "expect_error_class": "ACP2Error",
  "expect_error_status": 4
}
```

### Fields

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Human-readable description. Used as the Go sub-test name so it shows up in `go test -v` output. |
| `protocol` | yes | One of `acp1`, `acp2`, `emberplus`. Selects the per-protocol runner. |
| `wire_file` | yes | Path to a committed `*.jsonl` capture. Resolved against the scenario file's directory first, then against the repo root. |
| `expect_events` | no | List of compliance-event labels the runner must observe for the first error reply in the capture. Empty = no event assertion. |
| `expect_error_class` | no | Expected Go error type name (`ACP2Error`, `ACP1ObjectError`, …). Matched against the short form (no `*` prefix, no package). |
| `expect_error_status` | no | Expected numeric status / error code. For ACP2 = `stat`; for ACP1 = `MCODE`; for Ember+ = error sub-container. |

## Status today

- **acp2** — 2 scenarios cover the error-reply path (`err_no_access`, `err_invalid_obj`).
- **acp1** — runner stub; scenarios pending (need ACP1 error captures).
- **emberplus** — runner stub; scenarios pending.

## Adding a scenario

1. Capture the wire with `acp extract` or `acp walk --capture` against a real device exhibiting the edge case.
2. Commit the capture under `tests/fixtures/<proto>/` (only if < 100 KB — LFS is frozen).
3. Drop a new `<name>.json` in `tests/scenarios/<proto>/` referencing the capture.
4. Run `go test ./tests/unit/scenario/` — the new scenario runs as a sub-test on every `go test ./...`.

No Go code changes required.

## Out of scope today

- **Timeout / disconnect** scenarios — need an active transport mock (not replay). Tracked as a follow-up.
- **Scenario generator** that captures from live device + stamps the JSON. Separate issue.
