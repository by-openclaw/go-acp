# CLAUDE.md — Probel SW-P-08 / SW-P-88

Atomic per-protocol context for the Probel plugin. Read the root `CLAUDE.md`
first for cross-cutting rules (registry, compliance, error hierarchy, Go
idioms); this file holds the Probel-specific wire spec + quirks.

---

## Folder layout (this package)

```
internal/probel-sw08p/
├── CLAUDE.md    ← this file
├── codec/       stdlib-only byte codec (lift-to-own-repo ready)
├── consumer/    package probel — implements protocol.Protocol
├── provider/    package probel — implements provider.Provider
├── wireshark/   Lua dissector (TODO — see #TBD)
└── assets/      SW-P-08 spec + Commie + TS SW-P-08 emulator
```

- `codec/` has zero imports outside stdlib.
- `consumer/` and `provider/` both use `package probel` (two packages at
  different import paths — callers alias when they import both).
- Command files are one-per-command-byte, named
  `cmd_rxNNN_xxx.go` / `cmd_txNNN_xxx.go` (NNN = decimal command byte
  zero-padded to 3 digits).

## Authoritative spec

- `internal/probel-sw08p/assets/probel-sw08p/SW-P-08 Issue 30.doc` — read via antiword; the
  sibling `.pdf` is corrupted.
- ACK handling lives in §2 (Transmission Protocol), NOT §3.5.

## Transport — SW-P-08 §2

- TCP connection to the matrix controller. Default port 2008.
- Frame: `DLE STX <dest> <cmd> <data...> <cksum> DLE ETX`
- DLE stuffing: any 0x10 byte inside `<dest>...cksum` is doubled to `0x10 0x10`.
  Framing DLEs (STX/ETX/ACK/NAK) are never stuffed.
- Every good frame → send `DLE ACK`. Every bad frame → send `DLE NAK`.
- ACK timeout 1s, 5 retries per spec.
- DATA soft cap 128 bytes, hard cap 255.

## Matrix-centric RX/TX convention

- Rx = controller → matrix (request into the matrix)
- Tx = matrix → controller (response out of the matrix)
- One byte has two meanings depending on direction — e.g. 0x11 is
  `TxAppKeepaliveRequest` on the matrix side AND
  `RxProtectDeviceNameRequest` on the controller side. File naming
  disambiguates: `cmd_rx017_protect_device_name_request.go` vs
  `cmd_tx017_app_keepalive_request.go`.

## Commands implemented (SW-P-08 §3.2)

| Dec | Dir | Name                              |
|----:|:---:|-----------------------------------|
|   1 | Rx  | Crosspoint Interrogate            |
|   2 | Rx  | Crosspoint Connect                |
|   3 | Tx  | Crosspoint Tally                  |
|   4 | Tx  | Crosspoint Connected              |
|   7 | Rx  | Maintenance                       |
|   8 | Rx  | Dual Controller Status Request    |
|   9 | Tx  | Dual Controller Status Response   |
|  10 | Rx  | Protect Interrogate               |
|  11 | Tx  | Protect Tally                     |
|  12 | Rx  | Protect Connect                   |
|  13 | Tx  | Protect Connected                 |
|  14 | Rx  | Protect Disconnect                |
|  15 | Tx  | Protect Disconnected              |
|  17 | Rx  | Protect Device Name Request       |
|  17 | Tx  | App Keepalive Request             |
|  18 | Tx  | Protect Device Name Response      |
|  19 | Rx  | Protect Tally Dump Request        |
|  20 | Tx  | Protect Tally Dump                |
|  21 | Rx  | Crosspoint Tally Dump Request     |
|  22 | Tx  | Crosspoint Tally Dump (byte)      |
|  23 | Tx  | Crosspoint Tally Dump (word)      |
|  29 | Rx  | Master Protect Connect            |
|  34 | Rx  | App Keepalive Response            |
| 100 | Rx  | All Source Names Request          |
| 101 | Rx  | Single Source Name Request        |
| 102 | Rx  | All Dest Assoc Names Request      |
| 103 | Rx  | Single Dest Assoc Name Request    |
| 106 | Tx  | Source Names Response             |
| 107 | Tx  | Dest Assoc Names Response         |
| 112 | Rx  | Crosspoint Tie-Line Interrogate   |
| 113 | Tx  | Crosspoint Tie-Line Tally         |
| 114 | Rx  | All Source Assoc Names Request    |
| 115 | Rx  | Single Source Assoc Name Request  |
| 116 | Tx  | Source Assoc Names Response       |
| 117 | Rx  | Update Name Request               |
| 120 | Rx  | Crosspoint Connect-On-Go Salvo    |
| 121 | Rx  | Crosspoint Go Salvo               |
| 122 | Tx  | Crosspoint Connect-On-Go Salvo Ack|
| 123 | Tx  | Crosspoint Go-Done Salvo Ack      |
| 124 | Rx  | Crosspoint Salvo Group Interrogate|
| 125 | Tx  | Crosspoint Group Salvo Tally      |

## Quirks to remember

- Level-scoped matrix: every command that carries `<matrix, level>` is
  level-scoped. Do not assume level 0.
- Protect states are multi-state (not just locked/unlocked). See
  `codec.ProtectState`.
- Salvo "Element" is a distinct record type carrying src/dst/level triplets.
- NAK from a peer = "command unsupported," NOT a fatal error. The compliance
  profile absorbs NAK and emits an event.
- Name-family commands (100-series) are UTF-8 padded to 4/8/12/16 bytes per
  spec table — trailing NUL or space pad depending on command.

## Testbed

Four peers in the testbed:
1. Commie.exe — full receiver. **UI is 1-based across the board**:
   "Matrix: 0001" = wire MatrixID 0, dest columns "0001..2048" = wire
   0..2047, reply numbers displayed +1. UI "0000" underflows to
   all-1s wildcard on the wire (`FF 70 7F` = matrix 15 / level 15 /
   dst 1023). Supports up to 2048×2048, not limited to §3.1.2 caps.
   Interactive Parameters dialog drives Salvo testing via F1..F4.
2. TS server `internal/probel-sw08p/assets/smh-probelsw08p/` — full
   transmitter, byte-exact spec reference.
3. Real user device — partial transmitter; NAKs unsupported commands.
4. **Lawo VSM Studio** — both directions:
   - Controller mode: connects outbound to our `:2008`. Drives
     cmd 2/120/121 (connect + salvos) en masse. Ignores levels
     (Matrix-only navigation). Locks local-only unless forward
     configured. **Label push is unsolicited** — with Auto-Transmit
     attribute on, VSM emits `cmd 106` / `cmd 107` directly on
     connect without waiting for `cmd 100` / `cmd 102` requests.
     Registry `Source Label <N>` / `Target Label <N>` (REG_SZ) maps
     a SW-P-08 level index to a VSM label column.
   - Server mode: exposes its own 8×8 matrix at user-configurable
     port (validated at `10.6.239.153:7800`). **Read-only** — rejects
     `cmd 2` writes. Doesn't reply to `cmd 8` dual-status (only
     sends as keepalive probe). Labels returned space-padded. See
     `memory/project_probel_vsm_validation.md`.

A DLE ACK (`10 06`) from any peer is §2 frame-level only — it does NOT
imply the peer's application layer processed the command. VSM ACKs
every well-framed packet regardless. Infer "supported" only from a
follow-up app-layer reply (e.g. `cmd 11` after `cmd 12`) or a visible
UI change.

## Known deviations from spec

- Viewer on user device sometimes returns short frames for tally dumps —
  absorbed via `compliance.Profile`, event fired, no silent workaround.

## Metrics + observability (landed 2026-04-22)

- Consumer `Plugin` and provider `server` each embed a
  `*metrics.Connector`; call `Metrics()` to read it.
- `codec.CommandName(id)` + `codec.CommandIDs()` give the plugin a
  static catalogue for registering cmd names on the Connector. Call
  `RegisterCmd(id, name)` once per command byte at plugin init.
- Consumer wires per-cmd counters by parsing the wire header in the
  OnTx/OnRx callbacks via `probelCmdFromBytes`; bare ACK/NAK fall
  through to the aggregate path.
- Provider wires per-cmd counters using `f.ID` directly after Unpack
  in `session.run`, plus `ObserveCmdTx(id, n, time.Since(rxAt))` on
  reply emission so handler latency is attributable per-cmd.
- Producer CLI exposes `--metrics-addr :9100 --log-format json` to
  mount Prom `/metrics` + `/snapshot.json` and emit JSON logs.

See root `CLAUDE.md` "Metrics surface on the producer" section +
`memory/project_connector_metrics_v2.md` for the full plan.

### Scale bench (2 mtx × 65535 × 1 level)

- Tree generator: `tools/gen-probel-tree/main.go` emits labelled JSON at
  `internal/probel-sw08p/assets/scale_2mtx_65535_1lvl.json` — 2 matrices,
  single level, every src/dst labelled, sparse crosspoints.
- Bench subcommand: `dhs consumer probel-sw08p bench <host:port>` keeps
  one long-lived TCP connection, runs interrogate-all + connect-all with
  `src = 1 + (dst / 16)` across both matrices. Captures per-cmd latency
  to CSV/MD.
- Rationale + expected numbers in `memory/project_scale_bench_2mtx_65535.md`.

### Session 2026-04-23 closeout (post-compact)

- **W2 (commit `9bf57f3`)** — Wireshark Lua dissector at
  `wireshark/dissector_probel_sw08p.lua`. Handles §2 framing + DLE
  stuffing, DLE ACK/NAK pseudo-frames, checksum + BTC validation with
  expert-info notes, and per-cmd decode for crosspoint interrogate /
  connect / tally / tally-dump (byte + word) / name requests + responses
  / salvo build+fire / protect. Pure arithmetic (no Lua 5.3 bitops) so
  it loads on Wireshark 4.x (Lua 5.2) as well as 5.x.
- **W4 (commit `9504441`)** — `retries_total` counter on
  `metrics.Connector`. `ObserveRetry()` + `Snapshot.Retries` exposed as
  `dhs_connector_retries_total` in Prom and a "Retries" row in CSV/MD.
  Consumer wires `OnRetry → prof.Note(RetryAttempted) + met.ObserveRetry()`
  so retry storms are alertable without parsing logs. Retry machinery
  itself already in place since `1878e1c` (5× retry + 1 s ACK timeout +
  NAK handling). Closes #90.
- **W5 (commit `6a67a31`)** — `Plugin.IsOnline()` /
  `IsOnlineWithin(stale)` on the consumer. Derived from
  `metrics.Connector.LastRxAt`; any rx traffic (tally, name reply,
  DLE ACK, keepalive ping) keeps us alive; silence past
  `DefaultOnlineStaleAfter` (90 s = 3× provider keepalive cadence)
  flips offline. This is the cross-protocol alive-bit contract — the
  Ember+ mirror reads `IsOnline` off every plugin and pushes it into
  `canonical.Header.IsOnline`. Closes #91.
- **W6-A (commit `2f7797e`)** — Cross-layer integration tests at
  `provider/integration_test.go`. Three scenarios (maintenance round-
  trip, streaming tally-dump with 200 dsts, 3× reconnect) exercise the
  full codec + dispatcher + handler + session stack against a real
  TCP listener, catching regressions that net.Pipe tests miss. Retry
  + name-size + keepalive scenarios stay at their layer (codec /
  consumer) where peer behaviour is deterministic.

### Session 2026-04-23 refactors

- **S1 (commit `5241679`)** — per-session dispatcher goroutine + bounded
  channel; ACK fires immediately after decode so read loop never waits
  on handler. Prereq for multi-consumer scaling.
- **S2 (commit `ea42ca4`)** — per-frame `rx` / `tx` / `tally fan-out`
  slog calls moved to Debug, gated by `Logger.Enabled`. Follows
  `feedback_logging.md` "skip announce logs". Connect p50 dropped
  2.8 ms → 1.0 ms in 10× bench.
- **W1a (commit `3fb7d85`)** — per-level name encoding: `NameSize`,
  `MultiLine`, `PadChar` (pointer `*uint8`), `KeepPadding` on
  `canonical.MatrixLabel`. Codec `packNameWithPad` / `unpackNameWithTrim`
  accept explicit padChar + trim flag; CR/LF preserved verbatim inside
  fixed-width fields for 2-row display vendors (Calrec style).
- **W3 (commit `0e88ca4`)** — streaming tally-dump: `handlerResult`
  gains `streamToSender func(emit func(Frame) error) error` callback;
  rx 021 handler emits chunks of 128 byte-form / 64 word-form tallies
  instead of buffering the whole dump. Respects the 128-byte soft DATA
  cap (spec §2). Memory per dump goes from O(targetCount) to O(chunk).

### Known latent bug fixed along the way

`matrixState.sources []int16` silently wrapped source IDs at 32768
(int16 overflow). Source=40000 became int16(-25536) = "unrouted"
sentinel. Fixed by switching to `map[uint16]uint16` in commit
`c495fa3` — sparse storage AND correct 0-65535 source range.

## What NOT to do

- Do NOT use positional byte offsets outside the codec package — everyone
  else calls into `codec.Foo{...}.Marshal()` / `codec.ParseFoo(...)`.
- Do NOT combine multiple commands in one file. One command byte = one file.
- Do NOT silently work around a spec deviation — always route through
  `compliance.Profile` so the event is observable.
- Do NOT import `acp/internal/*` from `internal/probel-sw08p/codec/` — codec is
  stdlib-only.
