# Function: get_version (func=0)

Handshake function — the consumer issues it right after AN2
EnableProtocolEvents to learn the ACP2 protocol version the device
supports. Body carries the version in byte 3 of the reply header.

## Spec

`acp2_protocol.pdf` §3 "Functions", function=0. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "ACP2 message header"
(byte 3 = version in reply).

Wire layout:
- Request:  `type=0 req, mtid=N, func=0, pid=0` (4 bytes).
- Reply:    `type=1 rep, mtid=N, func=0, pid=<version>` (4 bytes).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 528 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 28 + 30 (request +
  reply pair). Reply carries `pid=1` (ACP2 v1).
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

Runs automatically during session setup — no explicit verb:

```bash
./bin/dhs consumer acp2 walk 127.0.0.1 --port 2072 --slot 1
```

The `info` verb surfaces the version:

```bash
./bin/dhs consumer acp2 info 127.0.0.1 --port 2072
```
