# CLAUDE.md — Snell RollCall

Atomic per-protocol context for the RollCall plugin. Read the root `CLAUDE.md`
first for cross-cutting rules (registry, compliance, error hierarchy, Go
idioms); this file holds RollCall-specific wire spec + quirks.

Analysis behind every claim here: [`docs/audit-2026-09-07.md`](docs/audit-2026-09-07.md)
(oracles, migration plan) and [`docs/dm-and-ui.md`](docs/dm-and-ui.md) (device
model, UI projection). Live evidence: [`docs/captures/`](docs/captures/).

---

## Folder layout (this package)

```text
internal/snell-rollcall/
├── CLAUDE.md    <- this file
├── codec/       stdlib-only byte codec (lift-to-own-repo ready)
├── session/     link + session state machine, shared by both roles
├── consumer/    package snellrollcall - implements consumer.Protocol
├── provider/    package snellrollcall - implements provider.Provider
├── wireshark/   dhs_snell_rollcall.lua
├── integration/ -tags integration (driven by Ansible only)
├── testdata/    protocol_types/ + fixtures/ + exports/
├── docs/        audit, dm-and-ui, captures, README/consumer/producer/runbook
└── assets/      vendor SDK: spec PDFs, C library, emulator, tools
```

`session/` exists because the link and session state machine is identical for
consumer and provider with the roles flipped — it mirrors the vendor library's
`Core/`. It imports `codec/` and `internal/transport` only. `consumer/` and
`provider/` never import each other.

## Authoritative spec

| Source | Covers |
| --- | --- |
| `assets/Protocol/Docs/RollCall-Tech-spec-V14-external.pdf` (Rev 14, 2006) | base protocol: header, addressing, sessions, services, types 0-64 |
| `assets/Protocol/Docs/Long String and 32-bit Command Number support.docx` (2014) | types 65-71, `SV_LONGSTR` negotiation |
| `assets/Protocol/Docs/Full Control Command Set.docx` (V12, 2014) | router interface, Data Transfer Params |
| `assets/Protocol/Source/RollCallV2/` | the C library — byte-exact where the spec is silent |

Where spec and library disagree, the **library wins on facts the spec omits**
and the **spec wins on conflicts**; every conflict is a compliance event.

---

## Transport

TCP only. Default port **2050**; a device's own IPShare port may differ (Centra
uses 2057). Wireshark decodes 2050-2060. No UDP anywhere — presence (`SP_IAM`)
travels inside the TCP stream as a broadcast-addressed message.

```text
off  size  field
 0    2    TxHdr.Flags   = 0x000C   (anything else: resync)
 2    2    TxHdr.Length  = 14 + rLength
 4    6    cDst {rNet u16, rUnit u8, rPort u8, rIndex i16}
10    6    cSrc {same}
16    2    rLength = 2 + payload
18    1    rPktType   (0..71)
19    1    rPktFlags  (0x80 PF_BACKCHANNEL, 0x40 PF_WIDEAREA)
20    n    payload (0..420)
```

All multi-byte fields are **big-endian**; structs are packed. Max frame on IP
is 440 bytes (library ceiling); the spec permits up to 1 574 — decode to the
spec, encode to the library.

`TCP_NODELAY` is mandatory for real-time control (spec §10.2.2).

## Addressing

`NNNN-UU-PP:SS`. `rNet` is a route of up to four bridge nibbles filled from the
top; `rUnit` is the node (0 = broadcast or "the unit at the other end"); `rPort`
is the module within it (0 = the unit itself); `rIndex` is the session index.

| Index | Meaning |
| --- | --- |
| `0` | blind / direct control, no session, treated as `UL_SUPERVISOR` |
| `1..253` | allocated sessions |
| `0xFE` | logging |
| **`0xFF`** | **`UNKNOWNSESS`** — unconnected, broadcasts, `SP_CALL` target |

Mapping to ADR-0022: **unit = Device/Frame, port = Slot/Card**. Enumeration is
two-level — map/net gives units, `SP_GETDEVLIST` gives that unit's ports, and
the real device model lives on the **ports**.

## Sessions

`SP_CALL{CONNECT_STR}` to `dst:FF` → `SP_ACK` carrying the server's session
index (or `SP_BUSY` + `ID_STR`, or `SP_NACK`). Service bits are all-or-nothing.

Front channel (flag 0) is client→server request/reply. Back channel (flag 0x80)
is server→client push, and **each push is itself an active message the client
must `SP_ACK`**. One active message in flight per channel per session; 3 s reply
timeout; 5 consecutive timeouts kills the session.

Back channel is off until `SP_BKCHNREADY{1}`; value pushes additionally need
`SP_REPFCHG` (`0xFFFF` = all). On enable the server flushes display state.

Servers do **not** reap idle sessions — always `SP_TERM`.

## Two generations

Selected **per session** by `SV_LONGSTR` (0x8000) in the `SP_CALL` service mask.
Capability is advertised **per unit** in `ID_STR.rService`.

| | 16-bit (Gen A) | 32-bit (Gen B) |
| --- | --- | --- |
| Command / menu index | u16 | u32 |
| Strings | ≤20 B incl. NUL, fixed fields | ≤64 B, NUL-terminated, UTF-8 |
| Read value | 11 `GETFSTAT` → 12 `RETFSTAT` | 69 `GETVALUE` → 71 `RETVALUE` |
| Write value | 16 `SETPARAM` → 12 | 70 `SETVALUE` → 71 |
| Menu count | 8 `GETFUNC` → 39 `BLOCKHEADER` | 65 `GETMENUCOUNT` → 66 |
| Menu line | 35 `GETNEXTPKT` (offset) → 9 `RETFUNC` (58 B) | 67 `GETMENUITEM` (absolute) → 68 |
| Pushes | 12 / 9 / 30 | 71 / 68 |

Detection: read `rService`; if 0x8000 set, `SP_CALL` with it. A NACK means fall
back to Gen A and fire `rollcall_longstr_advertised_but_refused`. A server
answers requests in whatever form it was asked; only **pushes** follow the
negotiated generation. Verified live: both forms answered on one session.

`--api-ver auto|16|32` selects; `auto` is the flow above.

## Services (`rService` bits)

`SV_MENUS 0x0001` · `SV_CONTROL 0x0002` · `SV_DISPLAY 0x0004` · `SV_FILE 0x0008`
· `SV_LOGGING 0x0010` · `SV_STREAM 0x0020` · `SV_MAP 0x0040` · `SV_PORTS 0x0080`
· `SV_NET 0x0100` · `SV_EXEC 0x0200` · `SV_TIME 0x0400` · `SV_RES2 0x0800`
· `SV_THUMBNAIL 0x1000` · `SV_FASTMENU 0x2000` · `SV_LOC3 0x4000`
· **`SV_LONGSTR 0x8000`**

## User levels

`UL_USER 0` · `UL_ENGINEER 1` · `UL_SUPERVISOR 2` · `UL_FACTORY 3`
(`UL_ALL 4` is a mask, never a session level).

Gating is a per-line/per-command `usermask`, enforced **twice** in the vendor
engine: the control service NACKs an out-of-level command, and the menu service
**replaces** the line with `CM_DATA|CM_HIDDEN|CM_DISABLED`, command 0, text
`"Reserved"`. Line counts never change, so **compare flags, not counts**.

Blind control (index 0) runs at `UL_SUPERVISOR`, so a factory-gated command
cannot be set blind. User level is part of the DM cache key:
`rId@cmdSet@userLevel`.

## Timers

| Timer | Value |
| --- | --- |
| Active reply timeout | 3 s (`SP_WAIT` extends it) |
| Consecutive failures → session dead | > 5 |
| `SP_IAM` period | 12.5-17.5 s |
| Map entry expiry | 60 s |
| Idle probe | `SP_KEEPALIVE` every ~15 s, 3 s per probe, 3 misses → reconnect |

`SP_KEEPALIVE` **is** answered by devices and proxies, on both connected and
unconnected sessions (measured, 4 ms). The C library never sends it and probes
with `SP_GETDEVINFO` instead — that is its own choice, not a server limitation.

## Menu model

A menu is a **flat array with nested spans**: a container (`CM_LIST` / `CM_TILED`)
owns the next `rStep` lines — the whole span, not just immediate children.
`CM_PARTIAL` is a lazily-loaded subtree whose `rCommand` is the target base
index; the first non-disabled one, conventionally hidden and named `RETURN`, is
the parent backlink and must not be drawn.

Styles: `CM_TILED 0x00` `CM_LIST 0x10` `CM_DISPLAY 0x20` `CM_BUTTON 0x30`
`CM_CHECKBOX 0x40` `CM_NUMBER 0x50` `CM_VGRAPH 0x60` `CM_HGRAPH 0x70`
`CM_EDITSTRING 0x80` `CM_VLEVEL 0x90` `CM_HLEVEL 0xA0` `CM_PARTIAL 0xB0`
`CM_DATA 0xC0` `CM_LINK 0xD0`; flags `CACHEABLE 0x01` `WRAPS 0x02`
`DISABLED 0x04` `HIDDEN 0x08`, plus Rope's private `DEFERRED 0x8000`.

Radio groups are implicit: consecutive `CM_BUTTON` lines sharing a `rCommand`,
each option's value in its own `rMinRange`.

## Value model (`rMode`)

`FS_VALUE 1` · `FS_STRING 2` · `FS_DATA 4` · `FS_WRAPPED 8` · `FS_PRESET 0x10`
· `FS_MATCH_ID 0x20`

- both `VALUE` and `STRING` set → **display the string, keep the number**
- `VALUE` only → `printf(szParamString, rValue / rDivScale)`
- **`rDivScale = 0` means 1** (deprecated but common)
- ranges are meaningless on read-only styles — ignore them there
- writes clamp to `[min,max]` and snap to a multiple of `rStep`
- a value **beyond `rMaxRange` means "recall the default"**, not "clamp to max"
- checkboxes take 0, 1, or 2 = toggle (deprecated; never emit 2)
- `FS_PRESET` recalls the default and the value field is **ignored**

## Compliance events

Naming `rollcall_*`. Full catalogue in the audit; the load-bearing ones:
`rollcall_txhdr_flags_not_12` · `rollcall_length_mismatch` ·
`rollcall_framer_resync` · `rollcall_unknown_pkttype` · `rollcall_string_overrun`
· `rollcall_string_not_utf8` · `rollcall_longstr_advertised_but_refused` ·
`rollcall_generation_mixed_on_session` · `rollcall_bc_before_ready` ·
`rollcall_bc_push_rejected` · `rollcall_sp_wait` · `rollcall_divscale_zero` ·
`rollcall_checkbox_toggle` · `rollcall_menu_text_param_bleed`

**Not events** (undefined by the spec, never report): bytes after the NUL in a
fixed-size string field, and the implicit pad byte in `GETNEXT_STR`.

---

## What NOT to do

1. **Never encode `UNKNOWNSESS` as `-1`.** `rIndex` is `INT16` so `-1` looks
   natural; the value is **255** (`0x00FF`). A lenient proxy answers anyway, a
   real device faults. This crashed the Centra simulator three times.
2. **Never address a back-channel push with our own session index.** The
   client's `cSrc.rIndex` from its `SP_CALL` is **sticky**: it is the
   `cDst.rIndex` of every push. Getting it wrong makes the client answer
   `SP_INVSESS` and **every push is silently lost** while the server looks healthy.
3. **Never treat a non-`SP_ACK` answer to a push as delivery.** `SP_INVSESS` to
   a push is a fault to report, not an acknowledgement to discard.
4. **Never push `SP_DISPDATA` or value updates to a non-`SV_CONTROL` session.**
   Map and ports sessions get device-info updates only.
5. **Never answer `SP_IAM`.** It is a broadcast and expects no reply.
6. **Never send a reply with `cSrc.rIndex = 0`.** On a session it is our session
   index; unconnected it is `0xFF`.
7. **Never rely on bytes after the NUL** in a fixed-size string field, and never
   report them — they are undefined padding, not a deviation.
8. **Never pipeline on one session.** One active message per channel; replies
   match head-of-queue, not command number.
9. **Never assume a reply is the next frame.** Demultiplex on `PF_BACKCHANNEL`;
   a push can arrive between a request and its reply.
10. **Never leave a session open.** Servers do not reap them and then refuse new
    ones. `SP_TERM` on exit, error and context cancel.
11. **Never cache a menu across a differing `rId@cmdSet@userLevel`.**
12. **Never invent an `rId` that collides with an assigned one** (~700 in
    `rc3id.h`) — clients key their GUI template cache on `rId:cmdSet` and would
    render the wrong panel. Keep generated lines non-cacheable.
13. **Never mix generations on one session** (policy: servers tolerate it, IQ
    gateways proxying module sessions may not).
14. **Never import `dhs/*` from `codec/`.**
15. **Never drive integration with `.sh`, `.ps1` or any language but Ansible +
    Go `-tags integration`.**

## Provider: the Control Panel needs a template

A producer is **not renderable by RollCall Control Panel from the menu service
alone**. The client opens a `SV_FILE` session on the node and reads
**`TEMPLATE.ZIP`** — a ZIP containing `Template.tpl`, an INI keyed
`[rId:cmdSet:userMask:0]` mapping widgets to command numbers:

```ini
[9002:1:15:0]
Size=0,0,440,300
Ctl4=New Scrollbar,101,0,-9,80,80,110,8    ; slider   -> cmd 101
Ctl8= ,102,1,64,80,100,17,8                ; checkbox -> cmd 102, on-value 1
Ctl1=Card Type,-1,0,-26,10,60,60,8         ; static label
```

Widget types seen in shipping vendor templates and confirmed by our own
rendering: `-100` tab bar · `-26` static text · `-18` value box · `-9` slider ·
`-14` preset button · `64` checkbox (on-value in the preceding field).

So the provider owes **two** projections from the canonical tree: the menu set
*and* the template. Both land in the provider unit.
