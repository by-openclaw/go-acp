# Error: protocol-error (stat=0)

The provider's request dispatcher falls through on an unknown function
code (anything outside the spec's {0,1,2,3} range) and replies with
`type=3 err, stat=0 protocol-error, pid=0`.

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=0. See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Error stat codes".

Wire layout:
- Request:  4-byte ACP2 header only — `type=0 req, mtid=N, func=0xFF, pid=0`.
- Error reply: `type=3, mtid=N, stat=0, pid=0`.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 532 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 741 + 743 — the
  "unknown func=0xFF" probe fired by `dhs consumer acp2 diag`
  (`internal/acp2/consumer/diag.go` probe 8).
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 diag 127.0.0.1 --port 2072 --slot 1
```

Among the probe suite this command fires, the `unknown func=0xFF`
row produces the stat=0 frame pair captured here.
