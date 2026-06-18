# Probel SW-P-08 Provider

> **Status: shipping**
>
> Serves a canonical `tree.json` as a Probel SW-P-08 matrix controller (tx
> side) over DLE/STX-framed TCP (default port 2008). Symmetric to the
> consumer plugin at
> [internal/probel-sw08p/consumer/](../../../internal/probel-sw08p/consumer/);
> reuses [internal/probel-sw08p/codec](../../../internal/probel-sw08p/codec)
> for the §2 framer + per-command codecs.
>
> CLI: `dhs producer probel-sw08p serve --tree <path> --host 0.0.0.0 --port 2008`.
> See [runbook.md](runbook.md) for the operator workflow.

## What it is

The provider IS the matrix emulator. It loads a canonical `tree.json`
(matrix + level + crosspoint state), binds a TCP listener, and answers
every consumer command on the wire with §2 framing + DLE-stuffing +
link-level `DLE ACK`/`DLE NAK`. It is the CI-safe oracle the loopback
integration test drives — no external device required.

```
dhs producer probel-sw08p serve \
    --tree internal/probel-sw08p/testdata/exports/matrix_tree.json \
    --host 127.0.0.1 --port 2008 --log-level info
```

Expected log on start (real, from the capture run):

```
level=INFO msg="probel provider listening" addr=127.0.0.1:2008 matrices=1
```

## Serve flags (shared producer CLI)

| Flag | Purpose |
|---|---|
| `--tree PATH` | canonical tree.json to serve (required; mutually exclusive with `--manifest`) |
| `--manifest PATH` | assemble the tree from referenced DMs under `--cache-dir` (ADR-0022) |
| `--host` / `--bind` | listen host (default `0.0.0.0`); `--bind` also pins the broadcast source IP |
| `--port N` | TCP listen port (`0` = plugin default 2008) |
| `--log-level` | `debug \| info \| warn \| error` |
| `--log-format` | `text \| json` (json for Loki/Promtail) |
| `--metrics-addr :9100` | serve Prometheus `/metrics` + `/snapshot.json` (the provider exposes `Metrics()`) |

## Layering (this package)

```
plugin.go   Factory + registration (provider.Register)
tree.go     canonical.Export → matrix state (per-matrix per-level map[dst]src)
server.go   TCP accept + session lifecycle
session.go  per-connection dispatcher goroutine + ACK-on-decode
handlers.go per-command request handlers + fan-out
cmd_rxNNN_*.go  one file per inbound command byte
```

- Per-session dispatcher goroutine + bounded channel; the link `DLE ACK`
  fires immediately after decode so the read loop never blocks on a handler
  (commit `5241679`).
- Tree state uses `map[(matrix, level, dst)] → src` (sparse, per root
  `CLAUDE.md` scale rules), never a dense array.

## Served command coverage

The provider answers every consumer verb the loopback integration test +
the captures in [verbs.md](verbs.md) exercise:

| Inbound (rx) | Reply (tx) | Source file |
|---|---|---|
| 001 Crosspoint Interrogate | 003 Tally | [cmd_rx001_crosspoint_interrogate.go](../provider/cmd_rx001_crosspoint_interrogate.go) |
| 002 Crosspoint Connect | 004 Connected (+ 003 fan-out to other sessions) | [cmd_rx002_crosspoint_connect.go](../provider/cmd_rx002_crosspoint_connect.go) |
| 007 Maintenance | — (fire-and-forget) | [cmd_rx007_maintenance.go](../provider/cmd_rx007_maintenance.go) |
| 008 Dual Controller Status | 009 | [cmd_rx008_dual_controller_status.go](../provider/cmd_rx008_dual_controller_status.go) |
| 010 / 012 / 014 Protect interrogate/connect/disconnect | 011 / 013 / 015 | [cmd_rx010](../provider/cmd_rx010_protect_interrogate.go) · [cmd_rx012](../provider/cmd_rx012_protect_connect.go) · [cmd_rx014](../provider/cmd_rx014_protect_disconnect.go) |
| 017 Protect Device Name | 018 | [cmd_rx017_protect_device_name.go](../provider/cmd_rx017_protect_device_name.go) |
| 019 Protect Tally Dump | 020 | [cmd_rx019_protect_tally_dump.go](../provider/cmd_rx019_protect_tally_dump.go) |
| 021 Crosspoint Tally Dump | 022 byte / 023 word (streamed) | [cmd_rx021_crosspoint_tally_dump.go](../provider/cmd_rx021_crosspoint_tally_dump.go) |
| 029 Master Protect Connect | 013 Protect Connected (state 2) | [cmd_rx029_master_protect_connect.go](../provider/cmd_rx029_master_protect_connect.go) |
| 100 / 101 source names | 106 | [cmd_rx100](../provider/cmd_rx100_all_source_names.go) · [cmd_rx101](../provider/cmd_rx101_single_source_name.go) |
| 102 / 103 dest assoc names | 107 | [cmd_rx102](../provider/cmd_rx102_all_dest_assoc_names.go) · [cmd_rx103](../provider/cmd_rx103_single_dest_assoc_name.go) |
| 112 Tie-Line Interrogate | 113 | [cmd_rx112_crosspoint_tie_line_interrogate.go](../provider/cmd_rx112_crosspoint_tie_line_interrogate.go) |
| 114 / 115 source assoc names | 116 | [cmd_rx114](../provider/cmd_rx114_all_source_assoc_names.go) · [cmd_rx115](../provider/cmd_rx115_single_source_assoc_name.go) |
| 117 Update Name | — (fire-and-forget) | [cmd_rx117_update_name_request.go](../provider/cmd_rx117_update_name_request.go) |
| 120 / 121 salvo build / fire | 122 / 123 | [cmd_rx120](../provider/cmd_rx120_crosspoint_connect_on_go_salvo.go) · [cmd_rx121](../provider/cmd_rx121_crosspoint_go_salvo.go) |
| 124 Salvo Group Interrogate | 125 | [cmd_rx124_crosspoint_salvo_group_interrogate.go](../provider/cmd_rx124_crosspoint_salvo_group_interrogate.go) |

### Default label synthesis

When the served export declares no explicit source/dest labels (as the
committed fixture does not), the provider synthesises stable positional
strings — source index 0 → `"SRC 0001"`, dest index 4 → `"DST 0005"`,
device 9 → `"DEV 0009"`. These are the names returned by the real
`all-source-names` / `protect-name` captures in [verbs.md](verbs.md).

### Tally fan-out (§3.2.3)

On `rx 002 Connect`, the originator receives `tx 004 Connected`; every
**other** connected session receives the `tx 003 Tally` fan-out (§3.2.3
"issued on all ports"). The loopback test `TestConnectFanOutToSecondSession`
pins this, and the `watch` capture in [verbs.md §8](verbs.md#8-watch--async-tally-fan-out)
shows a real fanned-out `tx 003` frame.

## Known deviation served

**Salvo commit emits `tx 004 Connected` per applied slot (issue #92).**
§3.2.30 says the matrix emits no `cmd 04` on the salvo path; neither Commie
nor Lawo VSM implement the §3.2.30 listener path. To match the de-facto
contract, `handleSalvoGo` emits one `tx 004` per slot (to the originator +
fan-out to other sessions) and fires `probel_salvo_emitted_connected` per
slot so every occurrence is auditable. Unit + integration tests pin it.
Full rationale in [`../CLAUDE.md`](../CLAUDE.md) "Known deviations from spec".

## Metrics & observability

The `*Server` embeds a `*metrics.Connector` (`Metrics()` accessor). With
`--metrics-addr :9100` the producer mounts Prometheus `/metrics` +
`/snapshot.json`. Per-cmd counters are wired using `f.ID` after Unpack in
`session.run`, plus `ObserveCmdTx(id, n, latency)` on reply emission so
handler latency is attributable per command byte.

## Open work

- The streaming tally-dump path (`rx 021` → chunked `tx 022`/`tx 023`)
  respects the 128-byte soft DATA cap; the multi-frame word-form path is
  covered by the provider's own `TestIntegrationStreamingTallyDump`.
- Live-controller (Lawo VSM in controller mode) provider validation is a
  Tier-3 integration item — gated behind the lab VPN, not CI.
