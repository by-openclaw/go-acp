# Probel SW-P-02 Connector

Consumer connector for the Probel SW-P-02 General Remote Control Protocol
(Issue 26) — a single-matrix, single-level matrix-router protocol.

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt](../../../internal/probel-sw02p/assets/probel-sw02/SW-P-02_issue_26.txt) | SW-P-02 Issue 26 (antiword-extracted) |
| Spec (original) | [internal/probel-sw02p/assets/probel-sw02/SW-P-02 issue 26.doc](../../../internal/probel-sw02p/assets/probel-sw02/SW-P-02%20issue%2026.doc) | Original Word document |
| Wireshark dissector | [internal/probel-sw02p/wireshark/dhs_probel_sw02p.lua](../../../internal/probel-sw02p/wireshark/dhs_probel_sw02p.lua) | Byte-exact reference |
| Protocol reference | [CLAUDE.md](../CLAUDE.md) | Wire format, command catalogue, quirks |
| Verbs & config | [verbs.md](verbs.md) | Every verb + transport/redundancy/reports/ensure/wireshark/ansible, with real captures |
| Export fixtures | [internal/probel-sw02p/testdata/](../testdata/) | protocol_types fixtures (per-command) + the demo matrix tree |
| Source code | [internal/probel-sw02p/consumer/](../../../internal/probel-sw02p/consumer/) | Plugin implementation |
| Unit tests | [internal/probel-sw02p/](../) | In-package *_test.go (replay + spec) |

---

## Transport

| Mode | Transport | Port | Description |
|---|---|---|---|
| TCP direct | TCP | 2002 | Default (and only). `SOM(0xFF) COMMAND MESSAGE CHECKSUM` framing. No DLE stuffing — bytes inside a frame are transparent. No framing-layer ACK/NAK. |

The MESSAGE length is command-dependent (no framing-layer length field):
the stream decoder uses the `PayloadSize` registry to derive each frame's
size — fixed-length via `PayloadLen`, variable-length via per-command peek
helpers (tx 015 / tx 076 / tx 077 / tx 100). The CHECKSUM is the 7-bit
two's-complement sum of `COMMAND || MESSAGE` with the MSB forced to 0.

### Firewall rules

```
TCP 2002  outbound    (matrix controller session)
```

### CLI transport selection

SW-P-02 has a single transport, so there is **no `--transport` flag**. The
target is `host:port`; port defaults to 2002 when omitted.

```
dhs consumer probel-sw02p interrogate 127.0.0.1:2002 --dst 5     # default port 2002
dhs consumer probel-sw02p watch       10.6.239.153:2002 --dsts 64 --srcs 64
```

---

## Verb catalogue (all wired)

Every SW-P-02 consumer verb dispatches to a `Send*` method on the consumer
`Plugin` and performs a single round-trip, printing one decoded line on
stdout (wire hex logged on stderr). Run `dhs consumer probel-sw02p --help`
for the live catalogue.

| Verb | Cmd(s) | Flags | Notes |
|---|---|---|---|
| `interrogate` | rx 01 → tx 03 (`--extended`: rx 65 → tx 67) | `--dst`, `--extended`, `--timeout` | Read the source routed to a destination |
| `connect` | rx 02 → tx 04 (`--extended`: rx 66 → tx 68) | `--dst`, `--src`, `--extended`, `--bad-source`, `--timeout` | Route a source to a destination |
| `connect-on-go` | rx 05 → tx 12 | `--dst`, `--src`, `--bad-source`, `--timeout` | Stage one crosspoint into the pending buffer |
| `go` | rx 06 → tx 04 + tx 13 | `--op set\|clear`, `--timeout` | Commit / discard the pending buffer |
| `salvo-connect` | rx 35 → tx 37, rx 36 → tx 38 | `--src`, `--dsts`, `--salvo`, `--bad-source`, `--clear` | Stage N crosspoints under a group, fire atomically — **see CLI limitation below** |
| `protect-interrogate` | rx 101 → tx 96 | `--dst`, `--timeout` | Read a destination's protect state |
| `protect-connect` | rx 102 → tx 97 | `--dst`, `--device`, `--timeout` | Owner-only write-protect a destination |
| `protect-disconnect` | rx 104 → tx 98 | `--dst`, `--device`, `--timeout` | Owner-only clear protection |
| `protect-dump` | rx 105 → tx 100 (fan-out) | `--first-dst`, `--count`, `--collect`, `--timeout` | Stream the protect map |
| `protect-name` | rx 103 → tx 99 | `--device`, `--timeout` | Resolve a device id → 8-char name |
| `dual-status` | rx 50 → tx 51 | `--timeout` | Redundant-controller health |
| `lock-status` | rx 14 → tx 15 | `--controller lh\|rh`, `--timeout` | Read-only source-signal-lock bitmap |
| `status` | rx 07 → tx 09 | `--controller lh\|rh`, `--timeout` | Controller status — 2 |
| `router-config` | rx 75 → tx 76/77 | `--timeout` | Level bitmap + per-level dst/src counts |
| `watch` | rx 01 sweep + subscribe | `--dsts`, `--srcs`, `--timeout`, `--capture` | Bootstrap sweep + live tallies |

**Global flags** (parsed before the subcommand, apply to every verb):
`--capture FILE.jsonl`, `--mtx-id N` (0-127), `--level L` (0-27),
`--dsts N`, `--srcs N` (matrix-config bootstrap + keep-alive),
`--initial-poll`, `--app-keepalive`, `--bootstrap-spacing`.

> **Known CLI limitation — `salvo-connect --dsts` collides with the global
> `--dsts`.** The global matrix-config parser consumes `--dsts` (accepting
> only a single uint) **before** subcommand dispatch, so the
> `salvo-connect` subcommand's own `--dsts` (which accepts a CSV/range like
> `0-7`) never receives the value: `--dsts 0-2` fails with `strconv.ParseUint
> parsing "0-2"`, and even `--dsts 1` is swallowed globally so the
> subcommand then errors `--dsts is required`. The salvo wire path itself
> is fully implemented and proven by the loopback integration test
> `TestSalvoConnectOnGoThenGo` (rx 35 ×N → rx 36 → tx 38 + read-back); only
> the CLI flag plumbing is currently unreachable. The group-salvo wire
> shape (cmd 35/36/37/38) is captured in that test, not via this CLI verb.

The generic `consumer.Protocol` methods (`Walk`, `GetValue`, `SetValue`,
`Subscribe`) intentionally do **not** map onto SW-P-02's matrix addressing
(matrix / level / dst / src does not fit the slot/group/label tree those
methods assume); use the matrix verbs above instead.

---

## Capabilities & Compliance Status

| Capability | Spec § | Status | Notes |
|---|---|---|---|
| TCP direct (port 2002) | §3.1 | ✅ fully compliant | One stream carries requests + spontaneous tallies; no DLE stuffing |
| SOM/COMMAND/MESSAGE/CHECKSUM framer | §3.1 | ✅ fully compliant | 7-bit two's-complement checksum, MSB forced 0; byte-exact per spec |
| Crosspoint Interrogate (rx 01) → Tally (tx 03) | §3.2.3 / §3.2.5 | ✅ fully compliant | Narrow Multiplier packs DIV-128 dst/src bits; sentinel src=1023 for out-of-range dst |
| Crosspoint Connect (rx 02) → Connected (tx 04) | §3.2.4 / §3.2.6 | ✅ fully compliant | Destination-driven (one source per destination per level); broadcast to all sessions |
| Connect-On-Go / Go (rx 05/06, tx 12/13) | §3.2.7/8/14/15 | ✅ fully compliant | Pending-buffer staged by CONNECT ON GO, committed/cleared by GO |
| Group salvo (rx 35/36, tx 37/38) | §3.2.36/37/38/39 | ✅ codec/provider; ⚠ CLI | Wire path proven by integration test; CLI verb blocked by `--dsts` flag collision (above) |
| Status Request / Response-2 (rx 07 / tx 09) | §3.2.9 / §3.2.11 | ✅ fully compliant | `--controller lh\|rh` |
| Source Lock Status (rx 14 / tx 15, var-len) | §3.2.16 / §3.2.17 | ✅ fully compliant | Read-only; software provider reports all-locked (no input cards). No command writes the lock bit — **N/A by spec** |
| Dual Controller Status (rx 50 / tx 51) | §3.2.45 / §3.2.46 | ✅ fully compliant | Zero-message request |
| Extended interrogate/connect/tally (rx 65/66, tx 67/68) | §3.2.47–50 | ✅ fully compliant | Separate 7-bit Multipliers per axis (range 0-16383) |
| Extended protect tally/connect/disconnect (tx 96/97/98, rx 101/102/104) | §3.2.60–62, §3.2.65/66/68 | ✅ fully compliant | Owner-only authority ladder; reject echoes unchanged state |
| Extended protect tally dump (rx 105 / tx 100, var-len) | §3.2.64 / §3.2.69 | ✅ fully compliant | Stream-processed; ascending-destination ordering |
| Protect device name (rx 103 / tx 099) | §3.2.63 / §3.2.67 | ✅ fully compliant | 8-char ASCII; resolved lazily after rx 102 captures OwnerDevice |
| Router configuration (rx 75 / tx 76 / tx 77, var-len) | §3.2.57–59 | ✅ fully compliant | Level bitmap + per-level dst/src counts derived from the tree |
| Source / destination label set on the wire | — | ⛔ not applicable | SW-P-02 has no command to rename a source/destination; labels live only in the served tree / controller UI |
| inc / dec / reset on a crosspoint | — | ⛔ not applicable | A crosspoint is a discrete route, not a numeric parameter with a step |
| Wire-side discovery | — | ⛔ not applicable | No discovery primitive every controller honours; matrix shape is caller-supplied (`--mtx-id`/`--level`/`--dsts`/`--srcs`) |
| App-layer retry / reconnect / keepalive | — | ⏳ pending | §3.1 has no framing ACK, so retry is app-layer; reconnect + heartbeat on the client-hardening backlog |
| Compliance profile | — | ✅ fully compliant | Event catalogue in `internal/probel-sw02p/{consumer,provider}/compliance_events.go` |

Legend: ✅ fully compliant · ⚠ partial · ⛔ not applicable · ⏳ pending (on roadmap).

---

## Timeouts

All timeouts are deterministic, user-overridable. No silent hangs.

| Timer | Default | Where | Override |
|---|---|---|---|
| TCP dial | 5 s | `ClientConfig.DialTimeout` | constructor |
| Per-Send | caller-owned | `ctx` threaded through `Client.Send(ctx, frame, matchFn)` | per-verb `--timeout` (default 5 s) |
| Online staleness ("alive" bit) | 90 s | `DefaultOnlineStaleAfter` | `IsOnlineWithin(d)` |
| Keep-alive ping spacing | 2 s | `DefaultAppKeepaliveSpacing` | `MatrixConfig.AppKeepaliveSpacing` (`--app-keepalive`; `0s` disables) |
| Bootstrap sweep spacing | 10 ms | `DefaultBootstrapSpacing` | `MatrixConfig.BootstrapSpacing` (`--bootstrap-spacing`) |
| `watch` run duration | until Ctrl-C | `--timeout` | `watch ... --timeout 4s` |

**Single-flight:** `ErrSendInFlight` if two `Send`s overlap on the same
client. **Keep-alive:** SW-P-02 has no in-protocol keep-alive command, so
the rotating rx 01 INTERROGATE / tx 03 TALLY round-trip *is* the
keep-alive — any rx traffic keeps the alive bit set.

---

## Compliance Profile

Every wire tolerance gets a named counter. A session is classified
**strict** if zero events fire, **partial** if any fire.

### Consumer event catalog

| Event | Meaning |
|---|---|
| `probel_sw02p_inbound_frame_decode_failed` | Inbound frame with a bad checksum; the reader drops the byte and resynchronises on the next SOM (0xFF). Frequent desync suggests a bad link. |

### Provider event catalog (for the served-device side)

| Event | Meaning |
|---|---|
| `probel_sw02p_inbound_frame_decode_failed` | Bad inbound frame; byte dropped, resync on next SOM |
| `probel_sw02p_unsupported_command` | Well-framed command with no handler (§3 permits matrices to ignore unknown commands) |
| `probel_sw02p_handler_decode_failed` | Handler could not decode a well-framed command's MESSAGE |
| `probel_sw02p_outbound_write_failed` | Reply/broadcast write to a session socket failed |
| `probel_sw02p_salvo_emitted_connected` | tx 04 CONNECTED emitted per committed salvo slot (sibling to the SW-P-08 salvo deviation) |
| `probel_sw02p_protect_unauthorized` | rx 102 / rx 104 from a device that does not own the destination — rejected, unchanged state echoed |
| `probel_sw02p_protect_override_immutable` | rx 102 / rx 104 on a `ProBelOverride` destination — §3.2.60 "cannot be altered remotely" |
| `probel_sw02p_protect_blocks_connect` | rx 02 / rx 66 CONNECT rejected because the destination is protected |
| `probel_sw02p_protect_blocks_connect_state_echoed` | tx 04 / tx 68 echoing the existing (unchanged) route so the controller does not oscillate |

---

## Error Reference

### Transport layer (TCP)

| Name | When | Recovery |
|---|---|---|
| `TransportError{Op:"connect"}` | TCP dial failed | Check host + firewall + port 2002 |
| `ErrNotConnected` | Verb issued before `Connect` | Connect first |
| `ErrSendInFlight` | Two `Send`s overlapped on one client | Serialise sends |
| `context deadline exceeded` | Per-Send deadline elapsed (`--timeout`) | Raise the timeout or check responsiveness |

### Command layer

| Name | When | Recovery |
|---|---|---|
| `ErrWrongCommand` | Decoder asked to parse a frame whose COMMAND byte does not match | Internal — indicates a mis-routed frame |
| `ErrShortPayload` | MESSAGE shorter than the command's fixed length | Absorbed as `inbound_frame_decode_failed`; resync on next SOM |

A peer NAK for a specific command is a *command-layer* signal, not a
framing signal: it is routed through a compliance event, never treated as
fatal.

### CLI exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Runtime / wire / protocol error |
| 2 | Validation / usage error (bad flags, out-of-range `--dst`/`--src`) |

---

## Addressing (matrix / level / dst / src)

SW-P-02 is a **matrix** protocol — it does not address by slot / group /
label. A crosspoint is identified by **(matrix, level, destination)** and
carries a single **source**:

| Field | Range (narrow §3.2.3) | Range (extended §3.2.47/48) | Wire encoding |
|---|---|---|---|
| Matrix | 0-15 | 0-127 | Multiplier high bits / extended byte |
| Level | 0-15 | 0-27 | Multiplier / extended byte |
| Destination | 0-1023 | 0-16383 | Multiplier DIV-128 bits + MOD-128 byte |
| Source | 0-1023 | 0-16383 | Multiplier DIV-128 bits + MOD-128 byte |

Source value **1023** (narrow) is reserved to mean "destination out of
range" (§3.2.5) — the matrix reports it when the queried destination has
no declared route. The narrow Multiplier packs 3 bits of DIV-128 for
Destination + 3 for Source + 1 bad-source bit; the extended form uses
separate 7-bit Multipliers per axis.

---

## Crosspoint verbs — real captures

All hex below is from `--capture` JSONL against a loopback provider
serving the committed 8×8 demo matrix
([`../testdata/exports/matrix_tree.json`](../testdata/exports/matrix_tree.json)).
See [verbs.md §5](verbs.md) for the annotated walkthrough; the essentials:

```
# interrogate dst 2 (no route) → src 1023 sentinel
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2
tally  dst=2 → src=1023 bad_source=false
  tx ff 01 00 02 7d        rx ff 03 07 02 7f 75

# connect dst 2 ← src 5, broadcast tx 04
$ dhs consumer probel-sw02p connect 127.0.0.1:12002 --dst 2 --src 5
connected  dst=2 src=5 bad_source=false
  tx ff 02 00 02 05 77     rx ff 04 00 02 05 75

# read back → route stuck
$ dhs consumer probel-sw02p interrogate 127.0.0.1:12002 --dst 2
tally  dst=2 → src=5 bad_source=false
  tx ff 01 00 02 7d        rx ff 03 00 02 05 76
```

---

## Status / lock / dual-status / router-config — real captures

```
$ dhs consumer probel-sw02p status 127.0.0.1:12002
status  controller=lh idle=false bus_fault=false overheat=false
  tx ff 07 00 79           rx ff 09 00 77

$ dhs consumer probel-sw02p lock-status 127.0.0.1:12002 --srcs 4
source-lock (read-only)  controller=lh sources=8 locked=8
  src=0 locked=true ... src=7 locked=true
  tx ff 0e 00 72           rx ff 0f 00 04 0f 0f 4f      # var-len bitmap, 8 sources all locked

$ dhs consumer probel-sw02p dual-status 127.0.0.1:12002
dual-controller  active=MASTER idle_faulty=false
  tx ff 32 4e              rx ff 33 00 00 4d

$ dhs consumer probel-sw02p router-config 127.0.0.1:12002
router-config (response-1)  level_map=0x0000001 levels=1
  level[0]  dsts=8 srcs=8
  tx ff 4b 35              rx ff 4c 00 00 00 01 00 08 00 08 23
```

The loopback emulator has no physical input cards, so `lock-status`
reports **every** declared source as `locked=true` (§3.2.17 all-present
interpretation). The lock bitmap is **read-only** — there is no rx command
to set or clear a source lock (it reflects hardware signal carrier
presence), so this verb has no write counterpart by spec.

---

## Protect verbs — real captures (full lifecycle)

The protect family enforces the owner-only authority ladder documented in
[../CLAUDE.md](../CLAUDE.md) "Protect + Lock authority model". Real capture
of the full lifecycle (interrogate → connect → interrogate → dump →
disconnect) against the loopback emulator:

```
# 1. start unprotected
$ dhs consumer probel-sw02p protect-interrogate 127.0.0.1:12002 --dst 4
protect tally  dst=4 → state=none device=0
  tx ff 65 00 04 17        rx ff 60 00 00 04 00 00 1c

# 2. protect dst 4 for device 9
$ dhs consumer probel-sw02p protect-connect 127.0.0.1:12002 --dst 4 --device 9
protect connected  dst=4 device=9 state=probel
  tx ff 66 00 04 00 09 0d  rx ff 61 01 00 04 00 09 11      # tx 097, state=probel, owner=9

# 3. interrogate confirms protect + owner stuck
$ dhs consumer probel-sw02p protect-interrogate 127.0.0.1:12002 --dst 4
protect tally  dst=4 → state=probel device=9
  tx ff 65 00 04 17        rx ff 60 01 00 04 00 09 12

# 4. resolve device 9's name (loopback has no name table → 8 spaces)
$ dhs consumer probel-sw02p protect-name 127.0.0.1:12002 --device 9
device 9 name=""
  tx ff 67 00 09 10        rx ff 63 00 09 20 20 20 20 20 20 20 20 14

# 5. dump the protect map (rx 105 → tx 100 fan-out, 32 entries/frame)
$ dhs consumer probel-sw02p protect-dump 127.0.0.1:12002 --first-dst 0 --count 8 --collect 800ms
  dst=4 state=probel device=9
protect tally-dump  first_dst=0 requested=8 entries_seen=1
  tx ff 69 08 00 00 0f     rx ff 64 01 00 04 09 10 7e

# 6. clear the protect (owning device)
$ dhs consumer probel-sw02p protect-disconnect 127.0.0.1:12002 --dst 4 --device 9
protect disconnected  dst=4 device=0 state=none
  tx ff 68 00 04 00 09 0b  rx ff 62 00 00 04 00 00 1a
```

A non-owner `protect-connect` / `protect-disconnect`, or any change to a
`ProBelOverride` destination, is **rejected**: the provider still emits the
tx 097 / tx 098 broadcast but echoes the **unchanged** state and fires
`probel_sw02p_protect_unauthorized` / `probel_sw02p_protect_override_immutable`
(see [../CLAUDE.md](../CLAUDE.md) authority ladder). Likewise an rx 02 / rx
66 CONNECT to a protected destination is dropped (or state-echoed if a
prior route exists) — `probel_sw02p_protect_blocks_connect`.

---

## Subscriptions (Tallies) & raw capture

- Transport: the same TCP session (no separate broadcast channel).
- `watch` subscribes to every spontaneous frame; tx 03 TALLY and tx 04
  CONNECTED arrive as the matrix or another controller changes routes.
- The bootstrap rx 01 sweep (when `--dsts` is set) primes the session so
  the matrix tallies back the current source for each destination.
- `--capture <path>.jsonl` records every wire frame as
  `{ts,proto,dir,hex,len}` for replay / analysis / debugging.

```
$ dhs consumer probel-sw02p watch 127.0.0.1:12002 --dsts 4 --srcs 4 --timeout 4s --capture watch.jsonl
event  cmd=0x03 payload_len=3      # ×5 (4-dst sweep + 1 keep-alive ping)
```

```json
{"ts":"2026-06-18T09:15:31.872Z","proto":"probel-sw02p","dir":"tx","hex":"ff0100007f","len":5}
{"ts":"2026-06-18T09:15:31.877Z","proto":"probel-sw02p","dir":"rx","hex":"ff0307007f77","len":6}
```

Decoding `ff0100007f` (tx): `SOM=ff cmd=01(INTERROGATE) MESSAGE=00 00
checksum=7f` → Multiplier=0, Destination=0. Reply `ff0307007f77` (rx):
`SOM=ff cmd=03(TALLY) MESSAGE=07 00 7f checksum=77` → Multiplier=0x07
(Source DIV-128 = 7), Destination=0, Source MOD-128 = 0x7f → **Source =
7×128 + 127 = 1023** = "destination out of range" (§3.2.5).

---

## Test Device

| Device | IP | Protocol | Notes |
|---|---|---|---|
| Loopback producer | 127.0.0.1:2002 | SW-P-02/TCP | `dhs producer probel-sw02p serve --tree ...` — the matrix emulator (used for every capture in this doc) |
| Commie.exe | testbed | SW-P-02/TCP | Matrix receiver; load `commie_SWP02.dat` (switch the .dat in its UI) |
| Lawo VSM SW-P-02 driver | testbed | SW-P-02/TCP | Real controller; pending live validation (same shape as the SW-P-08 testbed flow) |
