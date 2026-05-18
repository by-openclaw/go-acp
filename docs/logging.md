# docs/logging.md — logging ladder, formats, ingestion

R15 #476 binds the operator-facing logging contract used by every
consumer and producer verb. This file replaces the legacy
`memory/project_logging` entry per ADR-0015.

## Ansible-style verbosity ladder

The `-v / -vv / -vvv / -vvvv` ladder is intentionally aligned with
Ansible's `verbosity` levels so an operator who knows `ansible-playbook`
can read a `dhs` runbook without learning a second convention. Mapping:

| `dhs` flag | Ansible equivalent | dhs level | Output |
| --- | --- | --- | --- |
| (none) | (none) — Ansible default | `info` | warnings + errors + per-verb summary lines |
| `-v` | `-v` | `info` | (default; explicit) |
| `-vv` | `-vv` | `debug` | + plugin debug: connection state, walk progress, codec internals |
| `-vvv` | `-vvv` | `trace` | + per-frame decoded events |
| `-vvvv` | `-vvvv` | `trace` (+ raw hex when wired) | + raw S101 / AN2 hex (today routed through `--capture`) |

Practical consequence: `dhs producer emberplus serve -vvv` and
`ansible-playbook ... -vvv` reach similar verbosity. A future
`dhs ensure` verb (R14 #475) shares the same ladder so a single
`-vvv` on the Ansible command line propagates to every spawned `dhs`
sub-process via env or arg passthrough.

## Verbosity ladder

| Flag | `--log-level` equivalent | Output |
| --- | --- | --- |
| (none) | `info` | warnings + errors + per-verb summary lines |
| `-v` | `info` | same as default (explicit) |
| `-vv` | `debug` | + plugin debug: connection state, walk progress, codec internals |
| `-vvv` | `trace` | + per-frame decoded events |
| `-vvvv` | `trace` (+ raw hex stream when wired) | + raw S101 / AN2 hex (today routed through `--capture`) |

`--log-level <name>` and the `-v…` ladder are **mutually exclusive**.
Setting both fails with `validation:log-level-conflict` (CLI exit 2).
Per-host scripts should pick one convention and stick with it.

`--verbose` is preserved as a deprecated alias for `-vv` on every
consumer verb to keep older runbooks working — emit a deprecation note
in the next major version.

## Formats

`--log-format <text|json|loki>`. Defaults to `text` (current behavior).

### `text`

Slog text handler to stderr. Human-readable. Suitable for interactive
use. Example:

```text
time=2026-05-17T14:25:28Z level=INFO msg="session connected" host=127.0.0.1 port=9100
```

### `json`

Slog JSON handler to stderr. Standard slog keys (`time`, `level`,
`msg`). Useful for ad-hoc machine parsing.

### `loki`

Loki / Promtail-shaped JSON. Stable field set:

| Key | Type | Meaning |
| --- | --- | --- |
| `ts` | RFC3339 | timestamp (renamed from slog `time`) |
| `level` | lowercase string | `trace` / `debug` / `info` / `warn` / `error` / `critical` |
| `component` | string | source path (`<proto>.<plugin>` form, renamed from slog `source`) |
| `msg` | string | log line |
| (any) | any | arbitrary k/v from the slog call, passed through |

Example:

```json
{"ts":"2026-05-17T03:11:10Z","level":"info","component":"emberplus.consumer","msg":"session connected","host":"127.0.0.1","port":9100}
```

## `--log-only`

Suppresses operator stdout result lines so an operator can tail the
stderr log feed without interleaved result text. All log output goes to
stderr in the selected `--log-format`. Useful for:

```sh
dhs consumer emberplus walk 10.0.0.10:9000 --log-only --log-format loki 2>>./dhs.loki.log
```

Implementation: `os.Stdout` is redirected to `os.DevNull` when
`--log-only` is set. Verbs that print results via `fmt.Println` /
`fmt.Fprintln(os.Stdout, ...)` become no-ops without code changes.

## Promtail / Vector ingestion snippet

A minimal Promtail scrape config to ingest `dhs.loki.log` into Loki:

```yaml
scrape_configs:
  - job_name: dhs
    static_configs:
      - targets: [localhost]
        labels:
          job: dhs
          __path__: /var/log/dhs/*.loki.log
    pipeline_stages:
      - json:
          expressions:
            level: level
            component: component
            ts: ts
      - labels:
          level:
          component:
      - timestamp:
          source: ts
          format: RFC3339
```

Field aliases follow the Loki best-practice: keep cardinality low on
the label set (`level`, `component`), let `msg` and per-call k/v stay
in the log body for full-text search.

A Promtail / Vector reference config under `docs/deployment/grafana/`
is on the backlog and is **not** part of R15. R15 ships the contract;
the agent config lands when the dhs-srv Grafana stack does.

## Where this is wired

| Verb family | File | Status |
| --- | --- | --- |
| Every consumer verb (`get`, `walk`, `info`, `watch`, etc.) | `cmd/dhs/common.go` via `addCommonFlags` → `addLogFlags` | ✅ |
| `dhs producer <proto> serve` | `cmd/dhs/cmd_producer.go` | ✅ |
| `dhs producer acp1 fuzz` | `cmd/dhs/cmd_acp1_fuzz.go` | ✅ |
| `dhs producer tsl …` | `cmd/dhs/cmd_tsl_producer.go` | pending — separate flag set, retrofit in R19 #484 parity audit |

The shared helper is `cmd/dhs/logflags.go`:

- `addLogFlags(fs, defaultLevel)` registers the `-v / -vv / -vvv / -vvvv`
  ladder, `--log-level`, `--log-format`, and `--log-only` onto the
  verb's own `flag.FlagSet`.
- `(*logFlagSet).resolve(defaultLevel)` returns the configured
  `*slog.Logger` and applies the `--log-only` side effect.

## Non-blocking handler (performance)

Every logger returned by `NewTextLogger` / `NewJSONLogger` /
`NewLokiLogger` is wrapped in `internal/logging.AsyncHandler`. The
contract:

- `Handle()` clones the slog.Record and pushes it onto a buffered
  channel (default 4 KiB records). Returns immediately.
- A dedicated goroutine drains the channel and calls the inner
  handler synchronously. Drain runs at whatever rate stderr / file /
  Loki can sustain.
- Channel full → record dropped, `DropCount()` increments. Hot-path
  callers (Ember+ keepalive, Probel tally fan-out, ACP1 status
  announce) never block on the log writer.
- `Close()` drains the queue and joins the goroutine.
  `FlushTimeout(d)` waits up to `d` for the queue to empty (used at
  shutdown to give last records a chance).

Use the `*Sync` variants (`NewTextLoggerSync`, `NewJSONLoggerSync`,
`NewLokiLoggerSync`) when test code needs to inspect rendered output
immediately after the log call.

## Logger DI

Per CLAUDE.md "Architecture principles", loggers flow through
constructors — every plugin's `Factory.New(logger, ...)` accepts the
configured `*slog.Logger` from the CLI. Falling back to `slog.Default()`
when nil is acceptable defensive code; bypassing the constructor
parameter (calling `slog.Default()` from inside a hot-path function
that has access to a session-scoped logger) is **not**. Reviewed
2026-05-18: all production paths under `internal/<proto>/{consumer,
provider}/` route through the constructor parameter.

## Out of scope (future work)

- Per-component sub-levels (`--log-level=emberplus.consumer:debug,transport:warn`).
- Per-rung explicit "raw hex stream" plumbing (currently `--capture`).
- Shipping a Promtail / Vector / Loki / Grafana stack
  (`docs/deployment/grafana/` concern).
