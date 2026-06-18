# TSL UMD — v3.1 / v4.0 / v5.0 (tally / UMD push protocol)

TSL UMD is a **one-way tally / Under-Monitor-Display push protocol**. A
tally source (a switcher, a Lawo VSM, a Miranda Kaleido) *pushes* display
state — tally lamps + UMD label text — at a multiviewer (MV) listener.
There is no request/reply, no addressable object tree, no matrix. This
docs set is therefore adapted from the acp1 template for a push protocol:
sections that assume get/set/walk/crosspoints are marked **N/A** with the
reason.

| Role | Doc | Status |
|---|---|---|
| **Verbs & config reference** | [verbs.md](verbs.md) | every verb + transport/version/serve/dmsg/wireshark/ansible, with real captures |
| Consumer (MV receiver) | [consumer.md](consumer.md) | ✓ listens on UDP (v3.1/v4.0/v5.0) + DLE/STX TCP (v5.0); decodes + fires compliance events |
| Provider (tally source) | [provider.md](provider.md) | ✓ pushes frames to one or more MVs; `send` (one-shot) + `serve` (refresh loop) |
| Operator runbook | [runbook.md](runbook.md) | ✓ quick-reference card |

## Two roles, one direction

```
dhs consumer tsl-vNN listen [--bind HOST:PORT] [--tcp]    inbound  — MV receiver: decode + print tally frames
dhs producer tsl-vNN send  --dest HOST:PORT [flags]       outbound — tally source: push one frame
dhs producer tsl-vNN serve --dest HOST:PORT --refresh DUR outbound — tally source: push + re-emit on a loop
```

Unlike a Tree/DM connector (acp1, ember+) or a matrix connector (probel),
the consumer never *queries* the producer — it can only listen for frames
the producer chooses to send. The producer is the authoritative tally
source; the consumer is a passive multiviewer-style sink.

## Wire versions & default ports

| Version | Transport | Default port | Tally model |
|---|---|---|---|
| v3.1 | UDP | 4000 | 4× binary tallies + 2-bit brightness; **no colour** |
| v4.0 | UDP | 4004 (testbed; spec 4000) | v3.1 tallies + XDATA (L/R display × LH/Text/RH × 2-bit colour) |
| v5.0 | UDP (≤2048 B) / TCP with DLE/STX wrapper | UDP 8901 (Kaleido); TCP 8902 (testbed; spec 8901) | LH/Text/RH (3× 2-bit colour) + 2-bit brightness, per DMSG; multi-DMSG groups |

All ports are configurable (`--bind` on the consumer, `--dest` on the
producer). The testbed port offsets (v4.0 → 4004, v5.0 TCP → 8902) exist
because v3.1 owns UDP 4000 and v5.0 UDP owns 8901 on a single host; the
spec uses 4000 / 8901 for both.

## Spec documents

| Document | Path | Description |
|---|---|---|
| TSL UMD protocol (PDF) | [tsl-umd-protocol.pdf](../assets/tsl-umd-protocol.pdf) | Spec PDF — authoritative |
| Wireshark dissector | [dhs_tsl.lua](../wireshark/dhs_tsl.lua) | Byte-exact reference — decodes v3.1/v4.0 UDP + v5.0 UDP + v5.0 DLE/STX TCP |
| Wire-format context | [CLAUDE.md](../CLAUDE.md) | Byte-exact wire spec + compliance events + quirks |

## Replay fixtures

Real wire bytes for every (version, transport) are committed at
[`../testdata/fixtures/golden_frames.jsonl`](../testdata/fixtures/golden_frames.jsonl)
— codec-generated, drift-guarded, never hand-edited. See
[`../testdata/fixtures/README.md`](../testdata/fixtures/README.md).
