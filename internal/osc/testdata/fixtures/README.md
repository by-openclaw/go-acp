# OSC Test Fixtures

Replay fixtures for the OSC (Open Sound Control 1.0 + 1.1) connector, per
ADR-0025 deliverable 8 (replay fixtures).

This directory is the OSC sibling of `internal/probel-sw02p/testdata/` —
same role: committed, real, minimal artifacts that let the codec and
integration tests run without a live peer.

OSC is a message/push protocol with no client/server handshake and no
matrix tree to export, so the natural fixture is **golden OSC packet
bytes** — the byte-exact wire form of a message or bundle. These differ
from the probel `exports/matrix_tree.json` canonical-tree fixture, which
has no OSC analogue.

## Layout

```
internal/osc/testdata/
├── fixtures/
│   ├── README.md                     ← this file
│   ├── message_all_tags.bin          OSC 1.0 Message, all common type tags
│   ├── message_v11_payloadless.bin   OSC 1.1 Message, payload-less T/F/N/I
│   └── bundle_timetag.bin            OSC Bundle with a non-immediate timetag
└── scenarios/
    └── battery/                      multi-frame pcapng for the Wireshark
                                      dissector replay (see its own README.md)
```

## Golden packet `*.bin`

Each `.bin` is the raw OSC wire form of one packet, produced by the
repo's own codec encode path (`codec.Message.Encode` /
`codec.Bundle.Encode`) — **never hand-fabricated wire bytes**. The
integration test `TestOSCFixtures`
(`internal/osc/integration/fixture_test.go`) re-encodes each spec and
asserts the committed file is byte-identical to what the codec emits, so
a fixture can never drift from the codec. `TestOSCFixtures_RoundTrip`
additionally decodes each fixture back through the codec with zero
compliance notes, proving the bytes are real OSC.

| File | Bytes | Spec content |
| --- | ---: | --- |
| `message_all_tags.bin` | 92 | `/mixer/fader1` with the core (`i f s b`) + extended (`h d t S c r m`) type tags in one packet. Pins the full common type-tag set. |
| `message_v11_payloadless.bin` | 20 | `/q/go` with the OSC 1.1 payload-less tags `T F N I` plus an `i`. Pins the 1.1 tag-string additions (no arg bytes for T/F/N/I). |
| `bundle_timetag.bin` | 68 | `#bundle` carrying a non-immediate NTP timetag (`0x0000000100000000`) and two element Messages. Pins the bundle envelope + timetag + element-size framing. |

After an intentional codec change, regenerate:

```
DHS_REGEN_FIXTURES=1 go test -tags integration \
  ./internal/osc/integration/ -run TestOSCFixtures
```

then run WITHOUT the env var to confirm the drift-guard passes:

```
go test -tags integration ./internal/osc/integration/ -run TestOSCFixtures
```

## `../scenarios/battery/`

The comprehensive multi-frame OSC capture (`capture.pcapng`) used by the
Wireshark dissector replay regression
(`internal/osc/integration/dissector_replay_test.go`, which runs
`tshark -X lua_script:internal/osc/wireshark/dhs_osc.lua` over it). It
covers every type tag, both wire versions, and all three transports
(UDP, TCP length-prefix, TCP SLIP). See
`../scenarios/battery/README.md` for its capture context. It is
referenced here rather than duplicated.

## Pairing with `captures/`

Live captures live LOCAL ONLY at `captures/osc/<scenario>/frames.jsonl`
(gitignored per ADR-0021). Trim the relevant frames and promote them
into a committed scenario under `../scenarios/<name>/`, or — for a
single golden packet — add a codec-backed entry to `oscFixtures` in
`fixture_test.go` and regenerate, so the committed bytes always trace to
the real codec.
