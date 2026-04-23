# QualifiedNode

APP tag **10**. A Node addressed by its absolute path (OID-like), so
consumers don't have to descend through every ancestor just to reach it.

## Spec

Ember+ Documentation v2.50, p. 87.

```
QualifiedNode ::= [APPLICATION 10] SEQUENCE {
    path     [0] RELATIVE-OID,            -- e.g. 1.2.4.3
    contents [1] SET OF NodeContents OPTIONAL,
    children [2] ElementCollection OPTIONAL
}
```

`path` replaces the `number` on Node + the implicit tree position; it lets a
provider emit a child deep in the tree without re-sending all its ancestors.

The fixture shows three QualifiedNodes emitted in a single RootElementCollection
— each one a separately-qualified descendant.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 512 bytes.
- Extracted from: `bin/emberplus_glow_mtx_labels_param_lua.pcapng` frame 582.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Reaches a single sub-path using Qualified addressing
./bin/acp get 127.0.0.1 --protocol emberplus --port 9092 --path router.oneToN.3
```
