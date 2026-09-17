# Logging (uniform syslog contract)

**Epic #987.** Every connector — consumer *and* producer, every protocol —
logs in **syslog format by default**, to a **local file** and optionally a
**remote server**, while the **terminal stays human**. One contract, one set
of flags, everywhere.

## The model (Model B)

Two independent streams:

| Stream | What | Format |
|---|---|---|
| **Terminal — stdout** | the human data tables (`watch`/`tree`/`walk` rows) | human, always |
| **Terminal — stderr** | operational lines (connect, login, warnings, errors) | human text |
| **Local file** (on by default) | operational logs **and** the event stream | `--log-format` (syslog default) |
| **Remote server** (`--syslog-addr`) | same | RFC 5424 |

`--log-format` sets the **sink** format only; it never changes the terminal.
Events (Info level) go to the file/server sinks, not to stderr, so the
terminal table stays clean.

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `--log-format` | `syslog` | sink format: `syslog` (RFC 5424) · `json` (Loki/Promtail) · `text` |
| `--log` | `auto` | local log FILE. `auto` = `.cache/logs/<proto>/<host>/<verb>.log` (like the DM cache — always logs locally). A path overrides it; `off` disables it. |
| `--syslog-addr` | (none) | also forward RFC 5424 UDP datagrams to `host:port` (non-blocking; drops counted on stderr) |
| `--log-level` | `info` | `debug` · `info` · `warn` · `error` |

The local file is **on by default** at the ADR-0028 path — no flag needed,
exactly like the DM cache writes itself. Disable with `--log off`.

## Default log path

```
<dhs binary dir>/.cache/logs/<proto>/<host>/<verb>.log
```
`<host>` is the connection endpoint (for cerebrum-nb that is the NB server,
e.g. `10.6.250.5`, not the device). Gitignored (ADR-0020).

## Record formats

**syslog (RFC 5424)** — the default sink line:
```
<134>1 2026-09-04T00:41:18.9Z dhs-debian dhs 95885 - - cerebrum_value_change device=0.0.0.0 sub_device=0 object=ComputerOverview.ProcessorTime label=ProcessorTime value=2 type=integer access=R-- units= available=true
```
`<134>` = facility local0 (16) × 8 + severity info (6). Severity maps from
the slog level (debug/info/warn/error, plus `critical`).

**json** — `--log-format json`, one object per line (Loki/Promtail/Vector):
```json
{"time":"2026-09-04T00:04:43Z","level":"INFO","msg":"cerebrum_value_change","device":"0.0.0.0","object":"ComputerOverview.ProcessorTime","value":"2","type":"integer","access":"R--","units":"","available":true}
```

### Event fields

`cerebrum-nb watch` (`msg=cerebrum_value_change`):
`device, sub_device, object, label, value, type, access, units, available`.

Generic watch — acp1/acp2/emberplus/tsl (`msg=value_change`):
`proto, oid, slot, group, id, path, label, value, access` (or `kind=matrix`
for a crosspoint change).

## Ingestion

**Promtail — scrape the JSON file** (`--log-format json`):
```yaml
scrape_configs:
  - job_name: dhs
    static_configs:
      - targets: [localhost]
        labels: {job: dhs, __path__: /path/to/.cache/logs/**/*.log}
    pipeline_stages:
      - json:
          expressions: {level: level, msg: msg, object: object, value: value}
      - labels: {level: '', msg: ''}
```

**Promtail — RFC 5424 syslog** (`--syslog-addr <promtail-host>:1514`):
```yaml
scrape_configs:
  - job_name: dhs-syslog
    syslog:
      listen_address: 0.0.0.0:1514
      listen_protocol: udp
      labels: {job: dhs}
```

**rsyslog / syslog-ng**: point `--syslog-addr` at the collector's UDP port;
records are standard RFC 5424 and route by app-name `dhs`.

**Vector**: a `file` source over the JSON logs, or a `syslog` source (mode
`udp`) for `--syslog-addr`.

## Per-connector coverage

Every connector accepts all four flags and logs syslog locally by default.

| Connector | flags (`--log-format`/`--log`/`--syslog-addr`/`--log-level`) | local syslog default | watch events → sink |
|---|---|---|---|
| acp1 / acp2 / emberplus / tsl | ✅ (`addCommonFlags` verbs) | ✅ | ✅ (generic watch) |
| cerebrum-nb | ✅ | ✅ | ✅ |
| producer (all protocols) | ✅ | ✅ | ✅ (serve) |
| probel-sw08p / probel-sw02p | ✅ (stripped at dispatcher → ctx) | ✅ | operational (per-event: follow-up) |
| osc / nmos | ✅ (stripped at dispatcher → ctx) | ✅ | operational (per-event: follow-up) |

probel/osc/nmos honour the flags for operational + connection logs (so
`--syslog-addr` forwards them to a server); routing each individual
watch/tally *event* through the sink for those three (as cerebrum-nb and the
generic watch already do) is the one remaining refinement.
