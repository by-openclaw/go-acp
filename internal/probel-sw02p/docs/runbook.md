# Probel SW-P-02 — operational runbook

Quick-reference card for operators. For wire-format detail see
[CLAUDE.md](../CLAUDE.md); for in-depth consumer behaviour see
[consumer.md](consumer.md); for the full verb + sample reference see
[verbs.md](verbs.md).

---

## Transport matrix

| Mode | Port | Notes |
|---|---:|---|
| TCP direct | 2002 | `SOM(0xFF) COMMAND MESSAGE CHECKSUM`. No DLE stuffing — bytes inside a frame are transparent. One TCP stream carries requests + spontaneous tallies. |

SW-P-02 was originally an RS-485/422 serial protocol; this plugin runs it
over a single TCP socket. There is no UDP transport and no framing-layer
ACK/NAK (unlike SW-P-08): any peer confirmation arrives as a regular
application command (tx 03 TALLY, tx 04 CONNECTED, ...). Default port 2002
mirrors SW-P-08's 2008 at the project level.

## Verb reference

### Consumer

| Verb | What it does | Common flags |
|---|---|---|
| `dhs consumer probel-sw02p interrogate <host:port>` | Read the source routed to a destination | `--dst N`, `--extended`, `--timeout` |
| `dhs consumer probel-sw02p connect <host:port>` | Route a source to a destination (broadcasts tx 04) | `--dst N --src M`, `--extended`, `--bad-source`, `--timeout` |
| `dhs consumer probel-sw02p connect-on-go <host:port>` | Stage one crosspoint into the pending buffer | `--dst N --src M`, `--bad-source` |
| `dhs consumer probel-sw02p go <host:port>` | Commit / discard the pending buffer | `--op set\|clear` |
| `dhs consumer probel-sw02p salvo-connect <host:port>` | Stage N crosspoints under a group, fire atomically | `--src M --dsts CSV --salvo ID` — ⚠ see "Known issues" |
| `dhs consumer probel-sw02p protect-interrogate <host:port>` | Read a destination's protect state | `--dst N` |
| `dhs consumer probel-sw02p protect-connect <host:port>` | Owner-only write-protect a destination | `--dst N --device D` |
| `dhs consumer probel-sw02p protect-disconnect <host:port>` | Owner-only clear protection | `--dst N --device D` |
| `dhs consumer probel-sw02p protect-dump <host:port>` | Stream the protect map | `--first-dst N --count K --collect 1s` |
| `dhs consumer probel-sw02p protect-name <host:port>` | Resolve a device id → 8-char name | `--device D` |
| `dhs consumer probel-sw02p dual-status <host:port>` | Redundant-controller health | `--timeout` |
| `dhs consumer probel-sw02p lock-status <host:port>` | Read-only source-signal-lock bitmap | `--controller lh\|rh` |
| `dhs consumer probel-sw02p status <host:port>` | Controller status — 2 | `--controller lh\|rh` |
| `dhs consumer probel-sw02p router-config <host:port>` | Level bitmap + per-level dst/src counts | `--timeout` |
| `dhs consumer probel-sw02p watch <host:port>` | rx 01 bootstrap sweep + subscribe to async tallies | `--dsts N --srcs N`, `--timeout`, `--capture FILE.jsonl` |

**Global flags** (parsed before the subcommand): `--capture FILE.jsonl`,
`--mtx-id N` (0-127), `--level L` (0-27), `--dsts N`, `--srcs N`,
`--initial-poll`, `--app-keepalive`, `--bootstrap-spacing`.

SW-P-02 has no wire-side discovery primitive every controller honours —
VSM and Commie both configure matrix size + id + level in their UI per
matrix. The consumer follows that pattern: matrix shape is supplied via
the global `--mtx-id` / `--level` / `--dsts` / `--srcs` flags, not probed.
`router-config` (rx 75) is the one in-protocol "info" call and is wired,
but most controllers configure size externally rather than probe it.

### Provider

| Verb | What it does | Required flags |
|---|---|---|
| `dhs producer probel-sw02p serve --tree fixture.json --port 2002 --bind <ip>` | Serve a canonical matrix as an SW-P-02 device over TCP | `--tree` (or `--manifest`), `--port`, optional `--bind` (VIP) |

The provider binds a single TCP listener on `--port` (default 2002).
Multi-client is inherent: each TCP connection is an independent session,
and broadcast frames (tx 04 CONNECTED, tx 03 TALLY on connect, GO-done
acks) fan out to every connected session per §3.

`--metrics-addr :9100` mounts Prometheus `/metrics` + `/snapshot.json`;
`--log-format json` emits Loki/Promtail-friendly logs.

## Provider — protect & lock authority (owner-only)

SW-P-02 models **source lock** (HD-router input signal health,
§3.2.16/17) and **protect** (destination write-protection, §3.2.60+) as
two orthogonal fields. A crosspoint can be both locked AND protected.

| Layer | Where it lives | Behaviour |
|---|---|---|
| **Source lock** | per-source bit (§3.2.17) | read-only — reflects signal carrier presence. Software provider reports all-locked (no physical input cards). |
| **Protect** | per-destination 2-bit state + OwnerDevice (§3.2.60/66) | only the owning device may modify/clear; `ProBelOverride` cannot be altered remotely at all. |

Reject paths still emit tx 097 / tx 098 echoing the *unchanged* state, and
fire a named compliance event (`probel_sw02p_protect_unauthorized` /
`probel_sw02p_protect_override_immutable`). An rx 02 / rx 66 CONNECT to a
protected destination is dropped (or state-echoed if a prior route exists)
— `probel_sw02p_protect_blocks_connect`. See [../CLAUDE.md](../CLAUDE.md)
"Protect + Lock authority model" for the full authority ladder.

## Matrix-config contract

SW-P-02 has no per-IP DM cache (unlike ACP1). Matrix shape is **caller-
supplied**, not discovered:

```
--mtx-id N    matrix ID (default 0; range 0-127)
--level L     level    (default 0; range 0-27)
--dsts N      destination count on this (matrix, level)
--srcs N      source count on this (matrix, level)
```

- The provider derives matrix shape from the served canonical tree
  (`targetCount` / `sourceCount` per matrix/level).
- The consumer takes the shape from the global flags; `--dsts` non-zero
  drives the bootstrap rx 01 sweep + rotating keep-alive ping (SW-P-02 has
  no keep-alive command, so the rx 01 / tx 03 round-trip IS the
  keep-alive).
- rx 75 ROUTER CONFIGURATION REQUEST is supported as an explicit command
  but most controllers configure size externally rather than probe it.

## Common flows

### Bring up a loopback matrix and drive a crosspoint

```
# producer: serve the committed 8x8 demo matrix on TCP :2002
dhs producer probel-sw02p serve --tree internal/probel-sw02p/testdata/exports/matrix_tree.json --port 2002

# consumer: route src 5 → dst 2, then read it back
dhs consumer probel-sw02p connect     127.0.0.1:2002 --dst 2 --src 5
dhs consumer probel-sw02p interrogate 127.0.0.1:2002 --dst 2      # → src=5
```

### Watch the matrix + capture the wire

```
dhs consumer probel-sw02p watch 127.0.0.1:2002 --dsts 4 --srcs 4 \
    --capture sw02-watch.jsonl --timeout 4s
```

### Protect a destination then release it

```
dhs consumer probel-sw02p protect-connect    127.0.0.1:2002 --dst 4 --device 9
dhs consumer probel-sw02p protect-interrogate 127.0.0.1:2002 --dst 4      # → state=probel device=9
dhs consumer probel-sw02p protect-disconnect  127.0.0.1:2002 --dst 4 --device 9
```

### Bring up an emulator alongside production traffic

```
dhs producer probel-sw02p serve --tree matrix-fixture.json --port 2002 --bind 10.6.239.200
```

`--bind <vip>` binds the listener to a specific NIC so multi-instance
emulators on the same host appear as distinct addresses.

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| `interrogate` reports source 1023 for every dst | destination has no route in the served tree (1023 = "destination out of range" sentinel, §3.2.5) | connect a crosspoint, or check the tree's `targetCount`/`sourceCount` |
| `watch` shows no events | `--dsts` not set, so no bootstrap rx 01 sweep fires | pass `--dsts N` (and `--srcs N`) to drive the sweep |
| `salvo-connect` errors `--dsts: strconv.ParseUint` or `--dsts is required` | the global matrix-config `--dsts` swallows the subcommand's range `--dsts` | known CLI limitation (see [consumer.md](consumer.md) "Known CLI limitation"); use the loopback integration test for the salvo path until fixed |
| `session desync: dropping byte` in provider logs | a non-`0xFF` byte arrived where a SOM was expected | check the controller is speaking SW-P-02 (not SW-P-08 with DLE framing) on this port |
| remote CONNECT silently dropped | destination is protected by another device, or `ProBelOverride` | only the owning device may change it; inspect the `probel_sw02p_protect_*` compliance counters |
| `--mtx-id: 200 exceeds 127` | matrix id out of the wire range | use 0-127 (narrow addressing tops out lower) |

## Known issues

- **`salvo-connect --dsts` is unreachable from the CLI.** The global
  matrix-config flag parser consumes `--dsts` (single uint only) before
  subcommand dispatch, colliding with the salvo verb's own CSV/range
  `--dsts`. The salvo wire path (rx 35/36 → tx 37/38) is fully implemented
  and proven by the loopback integration test `TestSalvoConnectOnGoThenGo`;
  only the CLI flag plumbing needs a fix (rename one of the flags or
  exempt `salvo-connect` from the global `--dsts` capture).

## Pointers

- Wire format: [CLAUDE.md](../CLAUDE.md)
- Verbs & samples: [verbs.md](verbs.md)
- Deep-dive consumer doc: [consumer.md](consumer.md)
- Provider doc: [provider.md](provider.md)
- Wireshark dissector: [../wireshark/dhs_probel_sw02p.lua](../wireshark/dhs_probel_sw02p.lua)
- Spec: [../assets/probel-sw02/SW-P-02_issue_26.txt](../assets/probel-sw02/SW-P-02_issue_26.txt)
