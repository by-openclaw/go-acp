# mibs/

**The MIB library lives in its own repository: `github.com/by-protocol/mib`.**

Do not copy MIBs in here. That repo is the single source (ADR-0015), it is
shared across projects, and duplicating 500 KB of vendor source into this
tree would only create a second copy to drift.

It holds, per device under `ird/`:

    ird/TT1260/    ird/RX1290/    ird/RX8200/

plus `standard/` — the six IETF base modules every vendor MIB imports and no
vendor ships (`RFC1155-SMI`, `RFC-1212`, `RFC-1215`, `SNMPv2-SMI`,
`SNMPv2-TC`, `RFC1213-MIB`). Without those, nothing under `ird/` compiles.

The offline MIB compiler (see `../../CLAUDE.md`) takes a path to a checkout
of that repo and emits committed Go OID tables into this tree. The MIB source
stays there; the generated tables are what ships.

This folder remains only as the place to put a MIB that is genuinely specific
to this project and belongs nowhere else. That has not happened yet.
