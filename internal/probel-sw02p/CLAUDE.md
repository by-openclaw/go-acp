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
├── wireshark/   Lua dissector (TODO — scaffold pending)
└── assets/      SW-P-02 spec (Issue 26 .doc + antiword-extracted .txt)
```

- `codec/` has zero imports outside stdlib.
- `consumer/` and `provider/` both use `package probelsw02p` (two
  packages at different import paths — callers alias when they import
  both).
- Command files are one-per-command-byte, named
  `cmd_rxNNN_xxx.go` / `cmd_txNNN_xxx.go` (NNN = decimal command byte
  zero-padded to 3 digits). None are in the scaffold; they land in
  follow-up commits as commands are implemented.

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

## Commands implemented

Empty table for now — commands land in subsequent per-command commits.

| Dec | Dir | Name |
|----:|:---:|------|
|  —  |  —  | —    |

## Testbed

- `Commie.exe` (matrix-side receiver): ships `commie_SWP02.dat`, the
  SW-P-02 command set definition. Once per-command files land, use the
  same Commie build used for SW-P-08 testing — switch its loaded .dat
  file to SW-P-02 via its UI.
- No SW-P-02 TypeScript emulator is in-tree yet; the SW-P-08 emulator
  under `internal/probel-sw08p/assets/smh-probelsw08p/` is the closest
  reference for codec-layer layout expectations.

## Quirks to remember (applies once commands land)

- MESSAGE length is command-dependent — there is no framing-layer
  length field, so the stream decoder needs a per-command length table
  or state machine. The scaffold's `Unpack` treats the whole buffer as
  one frame; per-command commits must replace this with a length-aware
  scanner.
- Matrix + level + destination + source sizing follows §3 spec widths
  (generally byte-sized; extended commands exist but are not yet
  catalogued here — add as per-command files land).
- NAK from a peer for a specific command is a *command-layer* signal,
  not a framing signal; route it through a compliance event, do not
  treat it as fatal.

## What NOT to do

- Do NOT use positional byte offsets outside the codec package —
  everyone else calls into `codec.EncodeFrame` / `codec.DecodeFrame`
  (or per-command `codec.EncodeFoo` / `codec.ParseFoo` as they land).
- Do NOT combine multiple commands in one file. One command byte =
  one file.
- Do NOT silently work around a spec deviation — always route through
  `compliance.Profile` so the event is observable.
- Do NOT import `acp/internal/*` from `internal/probel-sw02p/codec/` —
  codec is stdlib-only.
- Do NOT add DLE-stuffing logic — SW-P-02 is transparent. If you
  find yourself reaching for DLE, STX, ETX, ACK, or NAK byte escapes,
  re-read §3.1 of the spec.
