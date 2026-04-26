# CLAUDE.md — TSL UMD (v3.1 / v4.0 / v5.0)

Atomic per-protocol context for the TSL UMD plugin. Read the root
`CLAUDE.md` first for cross-cutting rules (registry, compliance, error
hierarchy, Go idioms); this file holds TSL-specific wire spec + quirks.

---

## Folder layout (this package)

```
internal/tsl/
├── CLAUDE.md        ← this file
├── codec/           stdlib-only byte codec (lift-ready). One file per
│                    version+frame-type: v31_frame.go, v40_xdata.go,
│                    v50_packet.go, v50_dmsg.go, v50_dle_stx.go.
├── consumer/        package tsl — implements protocol.Protocol
├── provider/        package tsl — implements provider.Provider
├── wireshark/       dissector_tsl.lua (covers all three versions)
└── assets/          tsl-umd-protocol.pdf + extracted .txt + Miranda JARs
```

## Authoritative spec

- `internal/tsl/assets/tsl-umd-protocol.txt` — `pdftotext` extract of the
  TSL UMD spec PDF.
- `internal/tsl/assets/tools/TSL IP Emulator_1.02.jar` — **GVG Kaleido
  production test harness** for v5; reference for port 8901 + Miranda-
  style producer behaviour.

## Plugin scope (locked 2026-04-24)

Three wire versions, both directions (consumer = MV-receiver; provider
= tally-source pushing to an MV):

| Version | Transport | Default port | Tally model |
|---|---|---|---|
| v3.1 | UDP (spec §3.0) | 4000 | 4× binary tallies + 2-bit brightness; **no colour** |
| v4.0 | UDP | 4000 | v3.1 tallies + XDATA (3 positions × 2-bit colour × L/R display) |
| v5.0 | UDP (≤2048 B) or TCP with DLE/STX wrapper | 8901 (Kaleido) | LH/Text/RH (3× 2-bit colour) + 2-bit brightness |

**All ports configurable.**

Out of scope (spec-strict posture):

- v3.1/v4.0 over TCP — spec silent; TallyArbiter ships it as raw 18-byte
  chunks but that's off-spec.
- v5.0 TCP without DLE/STX wrapper — spec requires the wrapper on TCP.
  Consumer may tolerate on rx and fire `tsl_v5_tcp_unwrapped`.
- RS-422/485 serial — no hardware in-tree.

## Wire-layer spec (byte-exact)

### v3.1 — §3.0
- 18-byte frame: `HEADER(1) | CTRL(1) | DATA(16)`
- HEADER: `(addr & 0x7F) | 0x80`, addr range 0-126
- CTRL: bits 0-3 tallies 1-4 (on/off), bits 4-5 brightness (2-bit),
  bit 6 reserved (clear on tx; on rx fire `tsl_reserved_bit_set`), bit 7 = 0
- DATA: 16 ASCII 0x20-0x7E, **space-padded (0x20)** to 16 if shorter

### v4.0 — extends v3.1, full-compat
- Frame: `v3.1 frame | CHKSUM(1) | VBC(1) | XDATA(N)`
- CHKSUM: 2's-complement of sum(HEADER+CTRL+DATA) modulo 128
- VBC: bit 7 = 0, bits 6-4 = minor version (v4.0 → 0), bits 3-0 = XDATA byte count
- CTRL.6 = 0 display data / 1 command data (reserved in this version)
- XDATA (min-version 0): 2 bytes — Xbyte1 Display L, Xbyte2 Display R
  - each: bit 7 = 0, bit 6 reserved, bits 5-4 LH tally, bits 3-2 Text tally,
    bits 1-0 RH tally (all 2-bit colour)
  - colour values: 0=OFF, 1=RED, 2=GREEN, 3=AMBER

### v5.0 — §5.0, new protocol, no v3.1 compat
- Transport: UDP primary (≤2048 B); TCP with DLE/STX wrapper
  - DLE=0xFE, STX=0x02. Packet starts DLE/STX. 0xFE in body doubled to
    0xFE 0xFE. Byte-count fields unaffected by stuffing.
- All 16-bit values little-endian
- Packet: `PBC(2LE) | VER(1) | FLAGS(1) | SCREEN(2LE) | (DMSG+ | SCONTROL)`
  - PBC = total byte count of following body (excluding PBC itself)
  - VER = minor version (v5.00 = 0)
  - FLAGS bit 0 = UTF-16LE (1) vs ASCII (0); bit 1 = SCONTROL; bits 2-7 reserved
  - SCREEN: 0-65534; 0xFFFF = broadcast; 0 if unused
- DMSG: `INDEX(2LE) | CONTROL(2LE) | (LENGTH(2LE) | TEXT) | CONTROL_DATA`
  - INDEX: 0-65534; 0xFFFF = broadcast
  - CONTROL bits 0-1 RH Tally, 2-3 Text Tally, 4-5 LH Tally, 6-7 Brightness
    (0-3), 8-14 reserved, 15 Control Data flag
  - if bit 15 = 0: LENGTH + TEXT bytes (encoding per FLAGS.0)
  - if bit 15 = 1: CONTROL_DATA (undefined in v5.0)
- SCONTROL: undefined in v5.0

## Compliance events

| Event | Fires when |
|---|---|
| `tsl_reserved_bit_set` | v3.1 CTRL bit 6 or v5.0 CONTROL bits 8-14 set |
| `tsl_version_mismatch` | v4.0 VBC minor version != 0 |
| `tsl_checksum_fail` | v4.0 CHKSUM mismatch |
| `tsl_control_data_undefined` | v4.0 CTRL.6=1 or v5.0 CONTROL bit 15 set |
| `tsl_unknown_display_index` | DMSG arrives for an INDEX not modelled |
| `tsl_broadcast_received` | v5.0 SCREEN=0xFFFF or INDEX=0xFFFF |
| `tsl_charset_transcode` | UTF-16LE label transcoded to UTF-8 |
| `tsl_label_length_mismatch` | v3.1 packet arrives with != 16 DATA bytes |
| `tsl_v31_null_pad` | v3.1 frame received with 0x00 pad (non-spec) |
| `tsl_v5_tcp_unwrapped` | v5.0 TCP frame received without DLE/STX wrapper |

## CLI surface (consumer = MV-receiver)

```
dhs consumer tsl-v31 listen [--bind HOST:PORT]
dhs consumer tsl-v40 listen [--bind HOST:PORT]
dhs consumer tsl-v50 listen [--bind HOST:PORT] [--tcp]
```

Default ports: v3.1 UDP 4000, v4.0 UDP 4004 (testbed offset since
v3.1 owns 4000; spec is also 4000 for v4.0), v5.0 UDP 8901, v5.0
TCP 8902 (testbed offset since v5.0 UDP owns 8901; spec is also
8901 for TCP). Listener prints decoded frames live with field
labels mirroring Miranda IP Emulator UI exactly.

Dispatcher: `cmd/dhs/cmd_tsl.go` (`runTSLConsumer`).

## Testbed

| Tool | Coverage | Role |
|---|---|---|
| Miranda TSL IP Emulator v1.02 | v5.0 UDP + TCP | **GVG Kaleido production test harness.** Default port 8901. ✅ live-validated 2026-04-26 (single-DMSG + 5-DMSG group via "Group display messages"). |
| Miranda TSL Agent DEV-SNAPSHOT | v5.0 | v5 consumer |
| Lawo VSM Studio | v3.1 + v4.0 + v5.0 producer | cross-vendor producer. ✅ live-validated 2026-04-26 from 10.6.239.160 — single-DMSG per display, real tally semantics (PGM=red Text, PVW=green LH, ISO=amber RH). |
| [TallyArbiter](https://github.com/josephdadams/TallyArbiter) | v3.1 UDP + TCP (off-spec raw chunks), v5 UDP + TCP | OSS peer for decoder cross-check; **reference only**, not authoritative |

**Priority stack:** spec > Miranda (GVG Kaleido) > VSM (Lawo) >
TallyArbiter. When Miranda and TallyArbiter disagree, Miranda wins.
Never silently absorb a deviation — fire the relevant compliance event.

## What NOT to do

- Do NOT import `acp/internal/*` from `internal/tsl/codec/` — codec is stdlib-only.
- Do NOT hardcode tally-position semantics (`lh=preview` etc.) in the
  plugin. The plugin surfaces raw positional tallies; semantic mapping
  lives in the consumer UI / config layer.
- Do NOT copy TallyArbiter's brightness parser — it reads CTRL bits 5+6
  instead of the spec's bits 4+5.
- Do NOT pad v3.1 DATA with 0x00 on tx — space-pad (0x20) per spec. On
  rx, tolerate 0x00 pad and fire `tsl_v31_null_pad`.
- Do NOT emit v5.0 over TCP without DLE/STX wrapper — spec §Phy requires it.
