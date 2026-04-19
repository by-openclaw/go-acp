# QualifiedMatrix

APP tag **17**. Path-addressed form of Matrix. Used when a provider needs
to announce a single connection change deep in the tree without re-sending
the full parent chain.

## Spec

Ember+ Documentation v2.50, p. 88.

```
QualifiedMatrix ::= [APPLICATION 17] SEQUENCE {
    path       [0] RELATIVE-OID,
    contents   [1] SET OF MatrixContents OPTIONAL,
    children   [2] ElementCollection OPTIONAL,
    targets    [3] SEQUENCE OF Target OPTIONAL,
    sources    [4] SEQUENCE OF Source OPTIONAL,
    connections[5] SEQUENCE OF Connection OPTIONAL
}
```

The fixture shows a QualifiedMatrix with a single Connection update —
target 2 ← source [3], operation=`modified`. This is the common shape of
the connection-change announcement a provider emits after a crosspoint set.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 460 bytes.
- Extracted from: `bin/emberplus_glow_mtx_lua.pcapng` frame 43.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# After `acp matrix set`, watch for the QualifiedMatrix echo from the provider
./bin/acp watch 127.0.0.1 --protocol emberplus --port 9000
```
