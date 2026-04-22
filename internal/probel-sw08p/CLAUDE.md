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

## What NOT to do

- Do NOT use positional byte offsets outside the codec package — everyone
  else calls into `codec.Foo{...}.Marshal()` / `codec.ParseFoo(...)`.
- Do NOT combine multiple commands in one file. One command byte = one file.
- Do NOT silently work around a spec deviation — always route through
  `compliance.Profile` so the event is observable.
- Do NOT import `acp/internal/*` from `internal/probel-sw08p/codec/` — codec is
  stdlib-only.
