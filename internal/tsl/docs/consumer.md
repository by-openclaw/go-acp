# TSL UMD Connector — Consumer (multiviewer receiver)

Consumer connector for TSL UMD v3.1 / v4.0 / v5.0. TSL is a **tally / UMD
push protocol**: the consumer is a multiviewer-style *receiver* that binds a
socket and decodes every frame a tally source (Lawo VSM, Miranda Kaleido,
TallyArbiter, our own provider) pushes at it. It never queries the source —
there is no request/reply.

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [internal/tsl/assets/tsl-umd-protocol.pdf](../assets/tsl-umd-protocol.pdf) | TSL UMD spec PDF |
| Wireshark dissector | [internal/tsl/wireshark/dhs_tsl.lua](../wireshark/dhs_tsl.lua) | Byte-exact reference (all 3 versions + UDP/TCP) |
| Protocol reference | [internal/tsl/CLAUDE.md](../CLAUDE.md) | Wire format, compliance events, quirks |
| Replay fixtures | [internal/tsl/testdata/fixtures/](../testdata/fixtures/) | golden_frames.jsonl — real wire bytes per (version, transport) |
| Source code | [internal/tsl/consumer/](../consumer/) | Plugin implementation |
| Integration tests | [internal/tsl/integration/](../integration/) | `//go:build integration` loopback round-trips |
| Dispatcher | [cmd/dhs/cmd_tsl.go](../../../cmd/dhs/cmd_tsl.go) | `runTSLConsumer` / `runTSLListen` |

---

## Transport

| Version | Transport | Default port | Description |
|---|---|---|---|
| v3.1 | UDP | 4000 | 18-byte datagram, one frame per datagram. No colour |
| v4.0 | UDP | 4004 (testbed; spec 4000) | v3.1 frame + CHKSUM + VBC + XDATA |
| v5.0 | UDP | 8901 (Kaleido) | PBC/VER/FLAGS/SCREEN + DMSG(s), ≤2048 B |
| v5.0 | TCP | 8902 (testbed; spec 8901) | DLE/STX-wrapped, 0xFE byte-stuffing |

### Firewall rules

```
UDP 4000  inbound    (v3.1, v4.0 spec port)
UDP 8901  inbound    (v5.0 Kaleido)
TCP 8902  inbound    (v5.0 DLE/STX)
```

### CLI transport selection

```
dhs consumer tsl-v31 listen --bind 0.0.0.0:4000          # v3.1 UDP
dhs consumer tsl-v40 listen --bind 0.0.0.0:4004          # v4.0 UDP
dhs consumer tsl-v50 listen --bind 0.0.0.0:8901          # v5.0 UDP
dhs consumer tsl-v50 listen --bind 0.0.0.0:8902 --tcp    # v5.0 TCP (DLE/STX)
```

`--tcp` is rejected on v3.1/v4.0 (off-spec). Real guard:

```
$ dhs consumer tsl-v31 listen --tcp
error: consumer tsl-v31: --tcp is only supported for tsl-v50
```

---

## Capabilities & Compliance Status

| Capability | Status | Notes |
|---|---|---|
| v3.1 UDP decode (18-byte frame) | ✅ fully compliant | HEADER/CTRL/DATA; 4 binary tallies + 2-bit brightness, no colour |
| v4.0 UDP decode (v3.1 + CHKSUM + VBC + XDATA) | ✅ fully compliant | 2's-complement checksum verified; 2-byte XDATA → L/R display × LH/Text/RH colour |
| v5.0 UDP decode (PBC/VER/FLAGS/SCREEN + DMSG+) | ✅ fully compliant | single + multi-DMSG; little-endian fields |
| v5.0 TCP decode (DLE/STX wrapper) | ✅ fully compliant | 0xFE byte-unstuffing; SO_KEEPALIVE 30 s dead-socket detect |
| UTF-16LE labels (FLAGS bit 0) | ✅ fully compliant | transcoded to UTF-8 for display; fires `tsl_charset_transcode` |
| Broadcast (SCREEN / INDEX = 0xFFFF) | ✅ fully compliant | decoded; fires `tsl_broadcast_received` |
| Compliance event absorption | ✅ fully compliant | reserved bits / checksum / version mismatch absorbed + fired, never silently patched |
| Multi-source arbitration | ⛔ not applicable | TSL has no client identity; last frame wins per display — an app/deployment concern |
| get / set / walk / export / import | ⛔ not applicable | push protocol — no request/reply, no addressable object tree |
| `ensure` idempotent converge | ⛔ not applicable | no read-back to converge against |
| v3.1/v4.0 over TCP | ⛔ out of scope | spec silent; TallyArbiter ships raw 18-byte chunks off-spec |

Legend: ✅ fully compliant · ⛔ not applicable / out of scope.

---

## Timeouts

TSL consumer is a passive listener — there are **no request/reply timeouts**
(nothing is sent, so nothing can time out waiting for a reply). The only
timer is the v5.0 TCP keep-alive:

| Timer | Default | Where | Override |
|---|---|---|---|
| v5.0 TCP SO_KEEPALIVE period | 30 s | `--keepalive DUR` (TCP only) | `--keepalive 15s` |
| UDP receive | none (blocking listen until ctx cancel) | — | Ctrl-C / ctx |

The TSL v5 spec defines no app-layer heartbeat — a pcap audit confirmed VSM
never sends one — so OS-layer SO_KEEPALIVE is the correct dead-socket
detector for the persistent TCP path.

---

## Compliance Profile

Every wire tolerance gets a named counter; the consumer absorbs the
deviation (keeps decoding) and fires the event rather than silently working
around it. Catalogue from [`../CLAUDE.md`](../CLAUDE.md):

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
| `tsl_v31_null_pad` | v3.1 frame received with 0x00 pad (non-spec, TallyArbiter) |
| `tsl_v5_tcp_unwrapped` | v5.0 TCP frame received without DLE/STX wrapper |

A clean frame (no deviation) carries no notes — the loopback integration
test asserts exactly this (`len(f.Notes) != 0` ⇒ fail).

---

## Error Reference

### Transport / bind layer

| Name | When | Recovery |
|---|---|---|
| `listen udp HOST:PORT: ...` | UDP bind failed (port in use, bad addr) | free the port or pick another `--bind` |
| `listen tcp HOST:PORT: ...` | v5.0 TCP bind/accept failed | same |

### Usage / validation (client-side, pre-bind)

| Name | When |
|---|---|
| `consumer tsl-v31: --tcp is only supported for tsl-v50` | `--tcp` on v3.1/v4.0 |
| `consumer <proto>: unknown verb %q (expected: listen)` | a verb other than `listen` |
| `unknown TSL version %q` | proto not in {tsl-v31, tsl-v40, tsl-v50} |

Wire-level deviations are **not errors** — they are absorbed and surfaced as
compliance events (above), so a malformed-but-recoverable frame still
decodes and the listener keeps running.

---

## Subscriptions (push frames)

The consumer's entire job is a subscription: bind → decode → callback. The
`listen` verb wires a `SubscribeV31` / `SubscribeV40` / `SubscribeV50`
callback that prints each frame. There is no filter/selector on the wire —
every frame the source pushes is delivered.

Real v5.0 multi-DMSG decode (live loopback, three displays in one packet):

```
v5.0  remote=127.0.0.1:61443  screen=0  charset=ASCII  dmsgs=3
      display=2  LH=red  Text=off  RH=off  brightness=full  UMD="PGM"
      display=3  LH=off  Text=green  RH=off  brightness=full  UMD="PVW"
      display=4  LH=off  Text=off  RH=amber  brightness=full  UMD="ISO"
```

---

## Raw Capture

There is no `--capture` flag wired on the TSL consumer today (unlike acp1's
`--capture run.jsonl`). The committed
[`../testdata/fixtures/golden_frames.jsonl`](../testdata/fixtures/golden_frames.jsonl)
is the canonical real-bytes artifact — one JSON object per (version,
transport):

```json
{"version":"v3.1","transport":"udp","description":"addr=7 tally1+tally4 brightness=full text=\"PGM LIVE\"","hex":"873950474d204c4956452020202020202020"}
```

To capture raw frames live, run a packet capture (tcpdump / Wireshark) and
decode with the dissector (consumer.md → see verbs.md §10). Live vendor
captures live LOCAL ONLY under `captures/tsl-vXX/<scenario>/` (gitignored,
ADR-0021) and are promoted into `protocol_types/` fixtures by hand.

---

## Test Sources

| Source | Role | Status |
|---|---|---|
| Our own provider (loopback) | in-process tally source for integration tests | ✅ all 3 versions, UDP + TCP |
| Miranda TSL IP Emulator v1.02 | GVG Kaleido production harness — v5.0 UDP + TCP | live-validated 2026-04-26 (per CLAUDE.md testbed) |
| Lawo VSM Studio | cross-vendor producer — v3.1 + v4.0 + v5.0 | live-validated 2026-04-26 |
| TallyArbiter | OSS decoder cross-check (reference only) | not authoritative |

Loopback is the only CI-safe path today; external sources are reached over
VPN to the lab network and are not yet wired into the Go integration body
(see [`../../../ansible/playbooks/tsl-integration.yml`](../../../ansible/playbooks/tsl-integration.yml)).
