# Announce (type=2)

A live value-change event broadcast by the provider to every session
whose consumer has called `AN2 EnableProtocolEvents([2])`. Carries the
changed property exactly once per session, independent of the session
that caused the change.

## Spec

`acp2_protocol.pdf` §4 "Announces". See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Announces (type=2)".

Wire layout:
- `type=2, mtid=0, stat=0, pid=<changed property id>, obj-id, idx, property_header + value`.

`mtid=0` marks it as asynchronous (not a reply to any request). The
consumer MUST have enabled proto=ACP2 events beforehand — without
`EnableProtocolEvents([2])` the provider silently drops the broadcast
for that session.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 456 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 284 — fired by
  `set_property(GainS32, value=0)` on another session while our
  `watch` session is still subscribed.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

Start a long-running watch, then in another shell fire a set:

```bash
./bin/dhs consumer acp2 watch 127.0.0.1 --port 2072 --slot 1 &
./bin/dhs consumer acp2 set   127.0.0.1 --port 2072 --slot 1 --label GainS32 --value 3
```

The watch session prints the announce as soon as the set completes.
