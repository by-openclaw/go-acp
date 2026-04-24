# CLAUDE.md — OSC (Open Sound Control v1.0 + v1.1)

Atomic per-protocol context for the OSC plugin. Read the root `CLAUDE.md`
first for cross-cutting rules (registry, compliance, error hierarchy, Go
idioms); this file holds OSC-specific wire spec + quirks.

---

## Folder layout (this package)

```
internal/osc/
├── CLAUDE.md        ← this file
├── codec/           stdlib-only byte codec (lift-ready)
├── consumer/        package osc — implements protocol.Protocol (both versions)
├── provider/        package osc — implements provider.Provider (both versions)
└── wireshark/       dissector_osc.lua — full dhs dissector covering
                     UDP + TCP/length-prefix (1.0) + TCP/SLIP (1.1);
                     every type tag including 1.1 payload-less and
                     array markers; recursive bundle decode; per-message
                     Info column with address + type-tag + arg count.
                     Prefs: UDP port 8000, TCP-LP 8000, TCP-SLIP 8001.
```

## Authoritative specs

- OSC 1.0: https://opensoundcontrol.stanford.edu/spec-1_0.html (Stanford,
  CNMAT Berkeley — free, public)
- OSC 1.1: https://opensoundcontrol.stanford.edu/spec-1_1.html (paper,
  not a formal spec; community-accepted in practice)
- Index of pages: https://opensoundcontrol.stanford.edu/page-list.html

## Plugin scope (locked 2026-04-24)

Symmetric plugin — OSC has no client/server model; any peer can send
to any peer. dhs exposes both **consumer** and **provider** on the same
codec, with version-specific wire behaviour.

Per `memory/feedback_protocol_versioning.md` **Pattern A** (separate
registry entries per version):

| Registry name | Wire version | Transport surface |
|---|---|---|
| `osc-v10` | OSC 1.0 | UDP (primary) + TCP with int32 length prefix |
| `osc-v11` | OSC 1.1 | UDP + TCP with SLIP framing (RFC 1055 double-END) + adds `T`/`F`/`N`/`I` + `[`/`]` array markers |

**Default port**: 8000 (common OSC convention — user-configurable). No
port is officially mandated.

## Wire layer (byte-exact)

### Common primitives

- **OSC-string**: NUL-terminated ASCII, padded with NULs to 4-byte multiple
- **OSC-blob**: int32 big-endian size + bytes + pad to 4-byte multiple
- **int32 / float32**: big-endian, 4 bytes
- **int64 / float64**: big-endian, 8 bytes (extended types `h` / `d`)
- **Timetag**: u64 NTP format (seconds since 1900 + fractional); `0x0000000000000001` = "immediately"

### OSC Message

```
<OSC-string address> <OSC-string type tag> <arg1> <arg2> ...
```

- Type-tag string begins with `,` (comma) followed by one char per arg

### OSC Bundle

```
<OSC-string "#bundle"> <timetag u64> (<int32 element-size> <element>)*
```

- Element is either a Message or a nested Bundle (recursive)

### Required type tags (1.0 core)

| Tag | Type | Payload |
|---|---|---|
| `i` | int32 | 4 bytes big-endian |
| `f` | float32 | 4 bytes IEEE 754 BE |
| `s` | OSC-string | NUL-term + 4-byte pad |
| `b` | OSC-blob | int32 size + bytes + pad |

### Extended type tags (commonly implemented)

| Tag | Type | Payload |
|---|---|---|
| `h` | int64 | 8 bytes BE |
| `d` | float64 | 8 bytes BE |
| `S` | symbol (alt-string) | same as `s` |
| `c` | char | 4 bytes (ASCII in low byte) |
| `r` | RGBA32 colour | 4 bytes |
| `m` | MIDI message | 4 bytes (port, status, data1, data2) |
| `t` | Timetag | 8 bytes u64 NTP |

### 1.1 additions

| Tag | Type | Payload |
|---|---|---|
| `T` | boolean true | none |
| `F` | boolean false | none |
| `N` | nil/null | none |
| `I` | infinitum | none |
| `[` / `]` | array begin / end | none; marks a nested sequence in the tag string |

### SLIP framing (1.1 TCP / serial)

RFC 1055 with double-END encoding (OSC 1.1):

- `END = 0xC0`
- `ESC = 0xDB`
- `ESC_END = 0xDC` — stuffed form of a data byte equal to END
- `ESC_ESC = 0xDD` — stuffed form of a data byte equal to ESC
- Each packet is delimited by END both BEFORE and AFTER (double-END per 1.1)

### 1.0 TCP length-prefix framing

- `<int32 big-endian size>` + packet bytes. No SLIP.

## Plugin semantics (both versions)

- **Address → canonical path**: OSC addresses like `/mixer/ch/1/fader` map to canonical tree paths (dotted equivalent). dhs's canonical tree substrate is authoritative; the plugin translates.
- **Args → canonical.Value**: `i`/`h` → Integer; `f`/`d` → Number; `s`/`S` → String; `b` → Blob; `T`/`F` → Boolean; `N` → null.
- **Bundles → atomic SetValue batches**: a bundle groups N messages under one timetag; the consumer fires subscribers in order; the provider optionally batches SetValue calls within a tick into a single outgoing bundle.
- **Broadcast-friendly**: UDP providers honour `255.255.255.255` / subnet-broadcast destinations (SO_BROADCAST set on the egress socket).
- **Multi-listener**: UDP consumers set SO_REUSEADDR so multiple dhs instances on the same host share the port — matches the ACP1 / TSL multi-listener contract.

## Compliance events

| Event | Fires when |
|---|---|
| `osc_type_tag_unknown` | Type tag outside the known set received |
| `osc_alignment_violation` | OSC-string / OSC-blob not padded to 4-byte multiple |
| `osc_truncated_message` | Message or bundle body ends before expected argument boundary |
| `osc_comma_missing` | Type-tag string doesn't begin with `,` |
| `osc_slip_truncated` | SLIP frame ends mid-packet on TCP |
| `osc_array_unbalanced` | `[` without matching `]` inside a type-tag string (1.1) |
| `osc_bundle_nested_depth` | Bundle nested beyond sane limit (compliance guard) |

## Testbed

- **[TallyArbiter](https://github.com/josephdadams/TallyArbiter)** — `sources/OSC.ts` + `actions/OSC.ts`. Used for cross-vendor sanity, not authoritative.
- **QLab** (macOS) — sends OSC over UDP; rich bundle usage
- **TouchOSC / Lemur** — mobile control surfaces; UDP
- **Behringer X32 / Yamaha QL/CL** — audio-console OSC peers (producer use case)
- **Bitfocus Companion** — OSC source + action in broadcast control panels

## What NOT to do

- Do NOT import `acp/internal/*` from `internal/osc/codec/` — codec is stdlib-only.
- Do NOT hardcode address semantics (`/tally/preview_on`, etc.) in the plugin. Semantic mapping lives in consumer/UI config; the plugin surfaces raw addresses + args.
- Do NOT silently pad-align non-compliant inputs — fire `osc_alignment_violation` and accept what we can.
- Do NOT mix 1.0 length-prefix and 1.1 SLIP on the same TCP connection — they're different framings. Registry split (`osc-v10` vs `osc-v11`) enforces this at the API level.
- Do NOT accept SLIP-framed bytes without the trailing END (single-END mode). OSC 1.1 requires END before AND after per packet.
