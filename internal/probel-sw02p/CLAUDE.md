# CLAUDE.md — Probel SW-P-02

Atomic per-protocol context for the SW-P-02 plugin. Read the root
`CLAUDE.md` first for cross-cutting rules (registry, compliance, error
hierarchy, Go idioms); this file holds SW-P-02-specific wire spec +
quirks.

---

## Folder layout (this package)

```
internal/probel-sw02p/
├── CLAUDE.md    ← this file
├── codec/       stdlib-only byte codec (lift-to-own-repo ready)
├── consumer/    package probelsw02p — implements protocol.Protocol
├── provider/    package probelsw02p — implements provider.Provider
├── wireshark/   Lua dissector (landed; covers all 33 bytes)
└── assets/      SW-P-02 spec (Issue 26 .doc + antiword-extracted .txt)
```

- `codec/` has zero imports outside stdlib.
- `consumer/` and `provider/` both use `package probelsw02p` (two
  packages at different import paths — callers alias when they import
  both).
- Command files are one-per-command-byte, named
  `cmd_rxNNN_xxx.go` / `cmd_txNNN_xxx.go` (NNN = decimal command byte
  zero-padded to 3 digits).

## Authoritative spec

- `internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt` —
  antiword-extracted text of Issue 26 of the SW-P-02 General Remote
  Control Protocol. The sibling `.doc` is the original Word document.

## Transport — SW-P-02 §3.1

Frame layout:

```
SOM  COMMAND  MESSAGE  CHECKSUM
```

| Field     | Bytes | Notes                                                          |
|-----------|-------|----------------------------------------------------------------|
| SOM       |   1   | literal `0xFF`. No DLE escaping — bytes inside frame are transparent. |
| COMMAND   |   1   | command id per §3.2 table (decimal in the spec).               |
| MESSAGE   |   N   | 0 or more bytes; length determined by COMMAND.                 |
| CHECKSUM  |   1   | 7-bit two's-complement sum of `COMMAND || MESSAGE`. MSB forced to 0. |

- No DLE stuffing (unlike SW-P-08).
- Originally RS-485/422 serial; this plugin runs over TCP with default
  port **2002** (mirrors SW-P-08's 2008 at the project level).
- Because there is no framing-layer ACK/NAK, the codec's TCP client
  does not auto-emit control bytes. Any peer confirmation is delivered
  via regular application commands registered per-command.

## Implementation scope (locked 2026-04-24)

The SW-P-02 plugin ships the VSM driver's supported command set
bilaterally (codec + consumer rx + provider tx). Every command
outside the VSM set needs explicit per-command approval from the
user before code lands, referenced by sequence number from
`memory/project_probel_sw02p_cmd_queue.md`.

Authoritative VSM driver page:
https://docs.lawo.com/vsm-ip-broadcast-control-system/vsm-interface-driver-and-application-details/driver-supported-protocol-driver/driver-pro-bel-sw-p-02-generic

### Landed on `feat/probel-sw02p-commands` (PR #106, NOT merged / NOT pushed)

33 command bytes in three groups + Wireshark dissector:

#### Salvo family (10 bytes — original PR #106 scope)

| Byte | § | Name | Dir |
|-----:|---|------|:---:|
| 05 | 3.2.7 | CONNECT ON GO | Rx |
| 06 | 3.2.8 | GO | Rx |
| 12 | 3.2.14 | CONNECT ON GO ACKNOWLEDGE | Tx |
| 13 | 3.2.15 | GO DONE ACKNOWLEDGE | Tx |
| 35 | 3.2.36 | CONNECT ON GO GROUP SALVO | Rx |
| 36 | 3.2.37 | GO GROUP SALVO | Rx |
| 37 | 3.2.38 | CONNECT ON GO GROUP SALVO ACKNOWLEDGE | Tx |
| 38 | 3.2.39 | GO DONE GROUP SALVO ACKNOWLEDGE | Tx |
| 71 | 3.2.53 | Extended CONNECT ON GO GROUP SALVO | Rx |
| 72 | 3.2.54 | Extended CONNECT ON GO GROUP SALVO ACKNOWLEDGE | Tx |

#### VSM-supported bulk (14 bytes)

| Byte | § | Name | Dir |
|-----:|---|------|:---:|
| 01 | 3.2.3 | INTERROGATE | Rx |
| 02 | 3.2.4 | CONNECT | Rx |
| 03 | 3.2.5 | TALLY | Tx |
| 04 | 3.2.6 | CROSSPOINT CONNECTED | Tx |
| 07 | 3.2.9 | STATUS REQUEST | Rx |
| 09 | 3.2.11 | STATUS RESPONSE - 2 | Tx |
| 65 | 3.2.47 | Extended INTERROGATE | Rx |
| 66 | 3.2.48 | Extended CONNECT | Rx |
| 67 | 3.2.49 | Extended TALLY | Tx |
| 68 | 3.2.50 | Extended CONNECTED | Tx |
| 96 | 3.2.60 | Extended PROTECT TALLY | Tx |
| 97 | 3.2.61 | Extended PROTECT CONNECTED | Tx |
| 98 | 3.2.62 | Extended PROTECT DIS-CONNECTED | Tx |
| 100 | 3.2.64 | Extended PROTECT TALLY DUMP (var-len) | Tx |

#### Approved non-VSM seqs (17 bytes across 9 approvals)

| Seq | Byte | § | Name | Dir |
|----:|-----:|---|------|:---:|
|  5  |  14 | 3.2.16 | SOURCE LOCK STATUS REQUEST | Rx |
|  6  |  15 | 3.2.17 | SOURCE LOCK STATUS RESPONSE (var-len) | Tx |
| 30  |  50 | 3.2.45 | DUAL CONTROLLER STATUS REQUEST (zero-msg) | Rx |
| 31  |  51 | 3.2.46 | DUAL CONTROLLER STATUS RESPONSE | Tx |
| 32  |  69 | 3.2.51 | Extended CONNECT ON GO | Rx |
| 33  |  70 | 3.2.52 | Extended CONNECT ON GO ACKNOWLEDGE | Tx |
| 36  |  75 | 3.2.57 | ROUTER CONFIGURATION REQUEST (zero-msg) | Rx |
| 37  |  76 | 3.2.58 | ROUTER CONFIGURATION RESPONSE - 1 (var-len) | Tx |
| 38  |  77 | 3.2.59 | ROUTER CONFIGURATION RESPONSE - 2 (var-len) | Tx |
| 39  |  99 | 3.2.63 | PROTECT DEVICE NAME RESPONSE | Tx |
| 40  | 101 | 3.2.65 | Extended PROTECT INTERROGATE | Rx |
| 41  | 102 | 3.2.66 | Extended PROTECT CONNECT (owner-only auth) | Rx |
| 42  | 103 | 3.2.67 | PROTECT DEVICE NAME REQUEST | Rx |
| 43  | 104 | 3.2.68 | Extended PROTECT DIS-CONNECT (owner-only auth) | Rx |
| 44  | 105 | 3.2.69 | Extended PROTECT TALLY DUMP REQUEST | Rx |

### Non-VSM queue (per-command approval required)

33 numbered commands still pending per-cmd approval. See
`memory/project_probel_sw02p_cmd_queue.md`. Never write code for any
of these without explicit `approve seq N` from the user.

## Protect + Lock authority model (owner-only)

Per `memory/project_probel_extensions.md`, the canonical schema
treats source **lock** (HD-router input signal health, §3.2.16/17)
and **protect** (destination write-protection, §3.2.60+) as two
orthogonal fields. The provider enforces:

### Source Lock (rx 014 / tx 015)
- Read-only. No Rx command sets/clears the bit — it reflects
  hardware signal carrier presence. Handler returns all-locked
  by default (this plugin has no physical input cards).

### Protect (rx 101/102/104 + tx 096/097/098/100 + rx 103/tx 099)
- 2-bit state per dst: ProtectNone / ProtectProBel /
  ProtectProBelOverride / ProtectOEM.
- OwnerDevice (uint16) captured on rx 102 for authority checks.
- OwnerName (8-char ASCII) resolved lazily via rx 103 ↔ tx 099.

### Authority ladder on rx 102 / rx 104

| Current state | Requester Device | Result |
|---|---|---|
| `ProbelOverride` | any remote | **Reject** — §3.2.60 "Cannot be altered remotely". Fires `probel_sw02p_protect_override_immutable`. |
| `None` | any | **Accept** — on rx 102 sets state=Probel, owner=Device; on rx 104 no-op. |
| `Probel` / `OEM` | == stored owner | **Accept** — modify or clear. |
| `Probel` / `OEM` | ≠ stored owner | **Reject** — fires `probel_sw02p_protect_unauthorized`. |

Reject paths still emit tx 097 / tx 098 per §3.2.61 / §3.2.62
(fan-out fires on BOTH success and failure; Protect details byte
carries the actual resulting state). Echo unchanged state on reject.

Local-admin escape hatch (bypasses override) is reserved for a
future `server.AdminUnprotect(dst)` method — not wired to any Rx
command.

## Codec transport status

- **DialTimeout**: 5 s default, override via `ClientConfig.DialTimeout`.
- **Per-Send cancellation**: `ctx.Context` threaded through
  `Client.Send(ctx, frame, matchFn)` — caller owns timeout + cancel.
- **Single-flight**: `ErrSendInFlight` if two Sends overlap on the
  same Client.
- **Async events**: `Client.Subscribe(fn)` for unsolicited frames.
- **Length-aware scanner**: `codec.PayloadSize + isVariableLenCommand`
  handle variable MESSAGE lengths (tx 015 / tx 076 / tx 077 / tx 100).
- **Per-command metrics**: `metrics.Connector.ObserveCmdRx/Tx` wired
  on both sides.
- **Compliance Profile**: events defined in
  `provider/compliance_events.go` — `InboundFrameDecodeFailed`,
  `UnsupportedCommand`, `HandlerDecodeFailed`, `OutboundWriteFailed`,
  `SalvoEmittedConnected`, `ProtectUnauthorized`,
  `ProtectOverrideImmutable`.

### Gaps (tracked in `memory/project_probel_sw02p_client_hardening.md`)

- ❌ No auto-retry on Send timeout (§3.1 has no framing ACK, so
  retry has to be app-layer / per-command).
- ❌ No auto-reconnect on TCP drop.
- ❌ No keepalive / heartbeat.
- ❌ No `IsOnline` / `IsOnlineWithin` on Plugin.
- ❌ Server-side idle-session cleanup / graceful-drain / per-session
  write timeout.

Next-session priority: app-layer retry policy + reconnect + keepalive.

## Testbed

- `Commie.exe` (matrix-side receiver): ships `commie_SWP02.dat`, the
  SW-P-02 command set definition. Use the same Commie build that
  drives SW-P-08 testing — switch its loaded .dat file to SW-P-02
  via its UI.
- No SW-P-02 TypeScript emulator is in-tree yet; the SW-P-08 emulator
  under `internal/probel-sw08p/assets/smh-probelsw08p/` is the closest
  reference for codec-layer layout expectations.
- Real VSM SW-P-02 driver — see
  `memory/project_probel_vsm_validation.md` for the SW-P-08 testbed
  pattern; same shape expected once SW-P-02 hits live validation.

## Quirks to remember

- MESSAGE length is command-dependent — there is no framing-layer
  length field, so the stream decoder uses the `PayloadSize` registry
  to derive each frame's size (fixed-length via `PayloadLen` +
  variable-length via per-command peek helpers for tx 015 / 076 /
  077 / 100).
- **Narrow §3.2.3 Multiplier** packs 3 bits of DIV 128 for Dest + 3
  for Src (range 0-1023) + 1 bad-source bit. **Extended §3.2.47/48**
  uses separate 7-bit Multipliers per axis (range 0-16383).
- Tx 068 Extended CONNECTED is emitted on GO commits of Extended-
  staged salvo slots (rx 069 / rx 071) — narrow slots still emit
  tx 004 Connected. See `provider/salvo_connected.go` for the
  slot.Extended branch.
- NAK from a peer for a specific command is a *command-layer* signal,
  not a framing signal; route it through a compliance event, do not
  treat it as fatal.
- Source lock and protect are **orthogonal** — canonical schema
  models them as separate fields (`locked: bool` vs
  `protect: {state, owner}`). A crosspoint can be both locked AND
  protected simultaneously.

## What NOT to do

- Do NOT use positional byte offsets outside the codec package —
  everyone else calls into `codec.EncodeFrame` / `codec.DecodeFrame`
  (or per-command `codec.EncodeFoo` / `codec.ParseFoo`).
- Do NOT combine multiple commands in one file. One command byte =
  one file.
- Do NOT silently work around a spec deviation — always route through
  `compliance.Profile` so the event is observable.
- Do NOT import `acp/internal/*` from `internal/probel-sw02p/codec/` —
  codec is stdlib-only.
- Do NOT add DLE-stuffing logic — SW-P-02 is transparent. If you
  find yourself reaching for DLE, STX, ETX, ACK, or NAK byte escapes,
  re-read §3.1 of the spec.
- Do NOT bypass the owner-only protect authority ladder. Any rx 102 /
  rx 104 that changes state for a non-owner requester must fire the
  corresponding compliance event (unauthorized / override-immutable)
  and echo the unchanged state on the tx 097 / tx 098 broadcast.
