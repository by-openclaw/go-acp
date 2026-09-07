# RollCall DM → UI: what the live menus tell us, and how to generalise it

Date: 2026-09-07 · Source: live capture from `10.6.250.105` (Centra stack, direct on `:2057`)
Companion to [`audit-2026-09-07.md`](audit-2026-09-07.md). Analysis only, no code.

Goal, in the owner's words: *"test all cmd and get the menu file for analyse …
later it will help us to build the UI on the fly (and later we wish to use the
same for any connector: collect DM and check how we could build UI from them)."*

---

## 1. What was collected

| Artefact | Location | Content |
| --- | --- | --- |
| Per-unit menu dumps | [`captures/menus/*.json`](captures/menus/) | 15 units, **560 menu items**, full style/flags/ranges/format + sampled live values |
| Command support matrix | [`captures/cmd-matrix.json`](captures/cmd-matrix.json) | 27 message types probed against a live unit |
| Raw wire frames | [`captures/rollcall-live-2026-09-07.hex`](captures/rollcall-live-2026-09-07.hex) | 1 623 frames, every exchange in this analysis |

> **Correction (same day).** The first pass walked only **unit-level** nodes
> (`rPort = 0`) and concluded the cards were "skeletal because simulated".
> **That was wrong.** Every unit also exposes a `SV_PORTS` list, and the real
> device model lives on those **ports**. See §1A — the collection above is a
> small fraction of the true DM.

Menu sizes at unit level are uneven: the Centra gateway serves 57 lines,
TIELINES 444, each card node only 5, the matrices 3.

## 1A. The port layer — where the DM actually lives

A RollCall unit is a *container*; its controls sit on ports enumerated with
`SP_GETDEVLIST` (`SV_PORTS`). Measured live:

| Unit | Ports | Port nodes | Menu lines **per port** |
| --- | ---: | --- | ---: |
| `41:00` IP Slot 1: 5915 | **24** | `41:01…41:18` id 623 `Src 1, R1M1L1S1` … | **473** |
| `11:00` Matrix 1 | **2** | `11:01`, `11:02` id 637 `Level 1/2` | **5 549** |
| `12:00` Matrix 2 | **3** | `12:01…12:03` id 637 `Level 1/2/3` | 5 549 (assumed) |

So one card alone is 24 × 473 ≈ **11 400 lines**, and a single Router Level is
**5 549**. The true plant DM is two orders of magnitude larger than the 560
lines first collected. Consequences that matter for both the connector and the
UI:

- **Enumeration is two-level**: map/net → units, then `SP_GETDEVLIST` → ports,
  then menus per port. A consumer that stops at unit level sees almost nothing.
  This maps cleanly onto ADR-0022: unit = Frame/Device, **port = Slot/Card**.
- **The matrix is exposed as ports, not as the Full Control command set.**
  `Matrix N` (id 636 `ID_ROUTER_MATRIX`) contains `Router Level` ports (id 637
  `ID_ROUTER_LEVEL`), and the routing itself is menu lines on those ports —
  the "names served up via menu lines" approach the 2013 Full Control paper
  described as the *old* mechanism it wanted to replace. Commands 100–110
  NACK here, so this build predates or omits that interface.
- **Lazy loading is mandatory, not an optimisation.** A UI must never walk a
  5 549-line node eagerly to draw one page.

## 2. The model, as observed (not as guessed)

**A menu is a flat array with nested spans.** Each line has an index; a
container line (`LIST` / `TILED`) owns the **next `rStep` lines in the array**
— the whole span, not the immediate children. Reconstruction is a pre-order
walk that consumes `rStep` entries per container.

Verified against every unit, exactly:

| Unit | Flat items | Reconstructed | Roots |
| --- | ---: | ---: | ---: |
| Card 5915 | 5 | 5 | 1 |
| Centra gateway | 57 | 57 | 9 |
| TIELINES | 444 | 444 | 1 |

Real output, gateway:

```
LIST 'Comms Setup'            span=12
  LIST 'Ethernet'             span=10
    EDITSTRING 'IP address'          cmd=16802  '192.168.151.1'
    EDITSTRING 'Subnet mask'         cmd=16803  '255.255.0.0'
    EDITSTRING 'Default IP gateway'  cmd=16804  '0.0.0.0'
    DISPLAY    'MAC address'         cmd=16801
    BUTTON     'Restart'             cmd=17300  value=1
  CHECKBOX 'Net Show'                cmd=17202  on=1
```

`PARTIAL` is a **lazy subtree**: `rCommand` is the base index of another
partial, fetched with `SP_GETMENUCOUNT`/`SP_GETMENUITEM`. Every unit carries a
hidden `PARTIAL` named `RETURN` at index 1 as the parent backlink, exactly as
the spec prescribes — a renderer must skip it, not draw it.

## 3. Widget mapping — evidence-based

| `rStyle` | UI control | Binding rules | Live example |
| --- | --- | --- | --- |
| `LIST` | vertical group / menu page | children = span | `'Setup'` span 12 |
| `TILED` | tile grid | children = span | `'Status'` span 2 |
| `PARTIAL` | navigate (lazy load) | target = `rCommand` | `'RETURN'` (hidden) |
| `DISPLAY` | read-only label | value from `SP_GETVALUE` | `'Controller Type'` → `Centra WIN32 Controller` |
| `EDITSTRING` | text input | current text arrives in `szParamString` | `'IP address'` → `192.168.151.1` |
| `NUMBER` | numeric input / spinner | `min`,`max`,`step`,`divScale`,printf `format` | `'Changeover If No Net'` −1..3600 step 1 |
| `CHECKBOX` | toggle | **`rMinRange` is the "on" value** | `'Use Long Names'` on=1 |
| `BUTTON` | radio option **or** action | group by shared `rCommand`; this option's value is `rMinRange` | `cmd 301`: `8 char`=8, `32 char`=32 |
| `VGRAPH` / `HGRAPH` | slider | ranged, editable | — |
| `VLEVEL` / `HLEVEL` | meter | ranged, read-only | — |
| `DATA` | opaque blob | no generic widget | — |
| `LINK` | jump to another unit | — | — |

Flags: `HIDDEN` omit entirely · `DISABLED` render inert, and a disabled
container disables its whole span · `CACHEABLE` cache by `rId@rCmdSet` ·
`WRAPS` wrap at limits · `DEFERRED` batch the redraw.

**Radio groups are implicit.** Consecutive `BUTTON` lines sharing a
`rCommand` form one group; the selected option is the one whose `rMinRange`
equals the current value. TIELINES uses this heavily (`cmd 100`, 20 matrix
options). A parent `LIST` carrying `#SEL:` in `szParamString` is a vendor hint
that the group is a selector — useful, not required.

## 4. Value rendering — the rule that matters

`rMode` decides, and the live data shows all three cases:

| `rMode` | Render | Live example |
| --- | --- | --- |
| `STRING` only | show the string | `Module Presence` → `OKConfiguredPresent` |
| `VALUE` + `STRING` | **show the string, keep the number for logic** | `Config Checksum` value `-1686180113`, string `0x9B7EEEEF` |
| `VALUE` only | `printf(format, value / divScale)` | `cmd 301` value 8 |

Two traps confirmed live:

- **`divScale = 0` is common** (most gateway lines). The spec says treat 0 as
  1. A renderer that divides blindly will produce a division error on real
  devices.
- **Ranges are meaningless on read-only styles.** Cards report
  `min=-32767, max=23767` on `DISPLAY` lines. Only honour ranges for editable
  numeric styles.

## 5. Device quirk found: label/parameter bleed at exactly 20 characters

Raw bytes, same unit, four lines:

```
'IP address\0192.168.151.1\0'              label 10 chars — clean
'Bridge IP Port\0%.0f\0'                   label 14 chars — clean
'Changeover If No Net%d\0%d\0'             label 20 chars — BLED
'RollCall IPShare Por2015\02015\0'         label 20 chars — BLED
```

Both strings are correctly NUL-terminated, so this is **not** a framing or
parsing fault. The *content* of `szText` is wrong: when the label fills the
legacy 20-byte `szText` array there is no room for a terminator, so the
Generation-B builder's `strlen` runs on into the adjacent `szParamString`
buffer and copies it in. The parameter is then also emitted correctly as the
second string, so the value appears twice.

Consequences:

- **Decoder**: do not "fix" it silently. Surface
  `rollcall_menu_text_param_bleed` when `len(szText) >= 20` and `szText` ends
  with `szParamString`. Offer repair behind a flag, never by default
  (spec-strict posture).
- **Our provider**: always guarantee a NUL *inside* the field. Never emit a
  20-character `szText` in Generation A, and always terminate in Generation B.
  This bug is exactly what a fixed-array → NUL-terminated bridge produces.

## 5A. User levels — the mechanism, and what this device does

`CONNECT_STR.rUserLevel` selects the level for the whole session:
`UL_USER 0`, `UL_ENGINEER 1`, `UL_SUPERVISOR 2`, `UL_FACTORY 3` (the C header
adds `UL_ALL 4`). The spec is explicit that "a RollCall unit **may** have
different menu sets at each user level", and the vendor's Rope engine
implements that by returning out-of-level lines **flagged
`CM_DATA|CM_HIDDEN|CM_DISABLED`**, not by removing them.

That distinction decides how to test it: **compare flags, not counts.**
Measured on `41:01` (473 lines) at three levels:

| Level | Lines | Hidden | Disabled | vs `UL_USER` |
| --- | ---: | ---: | ---: | --- |
| `UL_USER` (0) | 473 | 1 | 17 | baseline |
| `UL_SUPERVISOR` (2) | 473 | 1 | 17 | **identical** |
| `UL_FACTORY` (3) | 473 | 1 | 17 | **identical** |

### The mechanism, from the spec and the vendor source

The owner's note — *"you need to use factory to enable CMD; it was the fix I
did long ago to manage RollCall"* — is **exactly right**, and the source shows
why. Every menu line and every command table entry carries a **`usermask`**,
a bitmask of the levels at which it is valid, tested as `1 << userlevel`
(`Rope.h:129`, `Rope.h:207`).

Rope enforces it in **two independent places**, which is the part that matters:

| Path | Enforcement | Result below the required level |
| --- | --- | --- |
| **Control** — `RA_CommandIsValid`, `RA_GetParam`, `RA_GetValue`, `RA_SetParam`, `RA_SetValue` (`rc_ctrl.c:58,112,327,545,619`) | `if ((ct->usermask & (1 << userlevel)) == 0)` | **`SP_NACK`** — the command is invisible *and* unwritable |
| **Menu** — `RA_GetFuncStr` (`rc_menu.c:237`) | same test | the line is **replaced**, not removed |

An out-of-level menu line comes back as a precise, detectable substitute
(`rc_menu.c:239-244`):

```c
fs->rStyle   = CM_DATA | CM_HIDDEN | CM_DISABLED;
fs->rCommand = 0;
strcpy(fs->szText, "Reserved");
```

That is a **wire signature a connector can detect**: a `CM_DATA` line, hidden
and disabled, command 0, text `"Reserved"` means *"exists, but not at your
level"* — not a broken or empty control. The line count never changes, which
is why counting alone can never reveal level gating.

The spec states the control-side rule explicitly for `SP_SETMULTI`: *"if a
command is set as 'factory' and the requesting user level is not UL_FACTORY,
then a SP_NACK will be returned"*, and confirms the cache rule: *"a caching
client records which combinations of Unit type, command set **and user level**
it has cached."*

Three further facts worth pinning:

- **Blind control is assumed `UL_SUPERVISOR`**, not factory (spec §9.17/§9.49;
  `ControlServer.c:1021,1132`). A factory-gated command therefore **cannot** be
  set over blind control on session 0 — it needs a connected session at
  `UL_FACTORY`.
- **Valid session levels are 0–3** (`MAX_USERLEVEL 4`, `rc_menu.c:69`).
  `UL_ALL = 4` is a mask value, never a level to send in `SP_CALL`.
- **Do not copy the vendor's comment.** `MenuServer.c:184` labels its bitmask
  table `USER, SUPERVISOR, ENGINEER, FACTORY`, which contradicts the enum
  (`UL_ENGINEER = 1`, `UL_SUPERVISOR = 2`). The *values* are a plain
  `1 << userlevel` and are correct; only the comment is wrong.

### What this simulator does

Nothing is gated here. Searching **every** dumped line for the substitute
signature:

| Metric | Count |
| --- | ---: |
| Menu lines examined | 1 033 |
| Lines named `Reserved` (access-gated) | **0** |
| `HIDDEN` / `DISABLED` lines (ordinary flags) | 20 / 22 |

Together with identical menus and identical command access at levels 0, 2 and
3, this proves the simulator sets `usermask = 0x0F` (all levels) on every line.
**So the negative result is a property of this oracle, not of RollCall.** Real
units gate calibration and factory menus exactly as the owner describes, and
the 16-bit frame or real hardware is what will exercise it.

Rules for the connector:

- Always send a deliberate `rUserLevel`; never leave it at the 0 default by
  accident. My first DM collection did exactly that and under-reported nothing
  here, but would have on real hardware.
- Expose it as `--user-level user|engineer|supervisor|factory` on every verb
  that opens a session.
- Treat the level as **part of the DM cache key** — `rId@rCmdSet@userLevel` —
  because the same unit legitimately yields different menus per level.
- A line arriving `HIDDEN|DISABLED` is an access signal, not a broken line.

## 5B. Write path — proven live

Authorised by the owner and verified against the Control Panel UI. Target
`0000-41-01` (`5915 Embedded Audio`), commands located from its own menu:

| Control | Command | Style | Encoding |
| --- | --- | --- | --- |
| Gain Input *N* | `400+N` | `NUMBER` | raw −720…300, `divScale` 10, `%0.1f dB` |
| Mute Input *N* | `500+N` | `CHECKBOX` | on = `rMinRange` = 1 |
| Invert Input *N* | `600+N` | `CHECKBOX` | on = 1 |
| Preset Gain All | `499` | `BUTTON` | action value 1 |

`SP_SETVALUE` → `SP_RETVALUE` echoing the stored value, confirmed by an
independent `SP_GETVALUE` read-back:

```
set 402 = -125  -> RETVALUE val=-125   read-back -125  (-12.5 dB)
set 502 = 1     -> RETVALUE val=1      read-back 1     (muted)
set 602 = 1     -> RETVALUE val=1      read-back 1     (inverted)
```

Confirms three design points: the reply carries the **device-confirmed** value
(so `SetValue` returns it rather than echoing the request), the scaling round
-trips through `divScale`, and a checkbox writes its `rMinRange` "on" value
rather than a boolean. Deprecated toggle value 2 was never used.

## 5C. Back channel — proven, and the rule that makes or breaks it

Two sessions to `0000-41-01`: **A** subscribes, **B** writes. Result:

```
A: BKCHNREADY(1)    -> ACK
A: catch-up pushes  -> 4 × SP_DISPDATA      (server flushes display state, spec §9.27)
A: REPFCHG(0xFFFF)  -> ACK                  [front reply, demultiplexed past pushes]
B: SETVALUE cmd 404 = -63
A: <- BACKCHANNEL SP_RETVALUE cmd=404 val=-63   ← the subscription push
```

The full `subscribe → observe → unsubscribe` cycle works, and a write on one
session is pushed to every other subscribed session. That is also exactly what
the provider must reproduce.

**Two rules, both learned by getting them wrong first:**

1. **ACK every back-channel push, whatever its type.** The back channel is an
   *active-message* channel: the server sends one push and waits for
   `SP_ACK` before sending the next. My first attempt acknowledged only
   `SP_RETVALUE` and ignored `SP_DISPDATA` — the unacknowledged display frames
   stalled the queue and the value push **never arrived**. The failure is
   silent: no error, just nothing. A connector with this bug looks like "the
   device doesn't support subscriptions".
2. **Demultiplex on `PF_BACKCHANNEL` (0x80), never on arrival order.** In the
   first attempt the reply to `SP_REPFCHG` was read as `pt=10` — that was a
   `SP_DISPDATA` push arriving before the ACK. Every request/reply helper must
   loop: if the frame has 0x80, queue it and ACK it, otherwise it is the reply.

Sequence a consumer must follow on connect: `SP_CALL` → front-channel setup
work → `SP_BKCHNREADY(1)` → drain and ACK the catch-up burst →
`SP_REPFCHG(0xFFFF)` → steady state.

## 6. Command support, measured

27 types probed on one live session (full table in `cmd-matrix.json`):

| Result | Types |
| --- | --- |
| **Answered** | `GETSTAT`→`RETSTAT` · `GETID`→`RETID` · `GETDEVINFO`→`RETDEVINFO` · `KEEPALIVE`→`ACK` · `GETLOCDEVMAP`/`GETDEVLIST`→`BLOCKHEADER` (15) · `LOGREQ`→`ACK` · `BKCHNREADY`→`ACK` · `STOPREPFCHG`→`ACK` · `FILEDIR`→`ACK` |
| **Generation A** | `GETFUNC`→`BLOCKHEADER` (57) · `GETFSTAT`→`RETFSTAT` |
| **Generation B** | `GETMENUCOUNT`→`RETMENUCOUNT` · `GETMENUITEM`→`RETMENUITEM` · `GETVALUE`→`RETVALUE` |
| **NACK (unsupported)** | `GETSRVBYNAME` · `GETDISPDATA` · `GETDISPCAPS` · `REALTIME` · `RAW` · `SETGROUP` · `STREAMMODE` · `DRAWTEXT` · `GETNEXTPKT` with no block in progress (correct) |

**Headline: both generations answer on the same session.** The session was
opened with `SV_LONGSTR` set, and the 16-bit `SP_GETFUNC` and `SP_GETFSTAT`
still returned proper replies alongside the 32-bit ones. This confirms
§4.2 of the audit from the wire rather than from the vendor source: a server
answers in **whatever form it was asked**, and only *pushes* follow the
negotiated generation. Old clients therefore keep working against these units.

Two rows are unreliable and flagged as such: after `SP_BKCHNREADY(1)` the
probe client read the *next* frame as the reply, but the back channel had
begun pushing. `SP_REPFCHG` shows `RETFUNC` and `SP_GETTIME` shows
`BLOCKHEADER` for that reason. **This is itself the lesson**: once the back
channel is live, a client must demultiplex on the `PF_BACKCHANNEL` flag and
never assume the next frame answers the last request.

## 7. Generalising: one UI pipeline for every connector

Do not build a RollCall UI. Build one pipeline and let each connector feed it:

```
device ──walk──> protocol DM ──project──> canonical tree ──derive──> UI descriptor ──> renderer
                                (export/canonical)          (+ UI hints)
```

The canonical tree already carries most of what a UI needs: `identifier`,
`description`, `access`, `type`, `minimum`, `maximum`, `step`, `factor`,
`format`, `enumMap`, `children`. Two of those map onto RollCall exactly —
`factor` ≡ `rDivScale`, `format` ≡ `szParamString` — which is why the
projection is clean in both directions (audit §7.5A does the reverse, canonical
→ RollCall, for the provider).

What canonical **lacks** for UI is presentation intent. Proposal: an additive,
optional `ui` block per element, ignored by every existing consumer:

| Field | Meaning | RollCall source | Ember+/ACP source |
| --- | --- | --- | --- |
| `container` | `list` \| `grid` \| `page` | `LIST` / `TILED` / `PARTIAL` | node depth + child count |
| `control` | `label` `text` `number` `toggle` `select` `slider` `meter` `action` `blob` | `rStyle` | parameter `type` + `access` |
| `options[]` | `{label, value}` | `BUTTON` lines sharing `rCommand` | `enumMap` |
| `visibility` | `hidden` \| `disabled` | `CM_HIDDEN` / `CM_DISABLED` | `access = none` |
| `cacheable` | cache key valid | `CM_CACHEABLE` + `rId@rCmdSet` | schema/template id |
| `lazy` | fetch subtree on demand | `CM_PARTIAL` | large node subtree |
| `display` | `string` \| `numeric` \| `both` | `rMode` bits | value type |

Rules that generalise beyond RollCall, all learned here:

1. **Prefer the device's display string over the raw number** when both are
   present. Every protocol that offers a formatted string does so because the
   number alone is misleading.
2. **Treat a scale factor of zero as one.** Cheap guard, real devices need it.
3. **Ignore ranges on read-only controls.** They are frequently garbage.
4. **Group by the write target, not by position.** RollCall groups radio
   options by shared command number; Ember+ by `enumMap`. Same concept.
5. **Lazy-load subtrees.** A 444-line menu is small; a 65 535-destination
   matrix level is not. The descriptor must express "fetch on demand".
6. **Cache on an identity key.** RollCall gives `rId@rCmdSet`, which the live
   data confirms is stable and shared across identical cards. Every protocol
   needs an equivalent or the UI re-walks on every connect.

## 8. Next steps

1. Convert the 15 dumps into canonical trees and diff against the `.mnu`
   cache files in `assets/` — validates the importer against a second source.
2. Prototype the `ui` block on one RollCall unit and one Ember+ tree; confirm
   a single renderer draws both.
3. Add `rollcall_menu_text_param_bleed` and `rollcall_divscale_zero` to the
   compliance catalogue (the latter already listed).
4. Re-run the command matrix with a back-channel-aware client so the
   `SP_REPFCHG` and `SP_GETTIME` rows become trustworthy.
5. Dump the real card menus once the 16-bit frame is installed — those are the
   large, realistic DMs the cards here only simulate.
