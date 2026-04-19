# ACP2 Protocol

## What is ACP2

ACP2 (Axon Control Protocol version 2) is a tree-structured binary protocol
for controlling next-generation Axon broadcast equipment. It runs exclusively
over AN2/TCP transport.

## Transport

- AN2/TCP only, port 2072
- AN2 framing required (magic 0xC635, 8-byte header)
- AN2 initialization sequence required before any ACP2 traffic

## Status

**Not yet implemented.** ACP2 support is planned for a future release.

The codec, session management, and walker are designed but not yet built.
See [CLAUDE.md](../../../CLAUDE.md) for the full ACP2 wire reference.

## Reference

- Spec: [acp2_protocol.pdf](acp2_protocol.pdf)
- AN2 spec: [an2_protocol.pdf](an2_protocol.pdf)
- Wireshark dissector: [dissector_acp2.lua](dissector_acp2.lua) — install per [docs/wireshark.md](../../docs/wireshark.md)
- Full wire reference: [CLAUDE.md](../../CLAUDE.md) (ACP2 section)

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
