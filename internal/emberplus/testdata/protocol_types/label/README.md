# Label

APP tag **18**. Matrix label pointer — references a Parameters subtree that
holds the human-readable labels for targets and sources.

## Spec

Ember+ Documentation v2.50, p. 89.

```
Label ::= [APPLICATION 18] SEQUENCE {
    basePath    [0] RELATIVE-OID,
    description [1] EmberString
}
```

A Matrix carries zero-or-more Labels inside `contents.labels[10]`. Each Label
points at a basePath (e.g. `router.labels.target`) where a subtree of
Parameters holds the actual strings (index → name). Consumers resolve label
strings by walking that basePath.

The fixture includes the full QualifiedMatrix → labels[10] → Label sequence
so the reader can see how Labels sit under MatrixContents.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 568 bytes.
- Extracted from: `bin/emberplus_glow_lua.pcapng` frame 127.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp walk 127.0.0.1 --protocol emberplus --port 9000 \
    --labels inline   # absorbs label subtree into target/source arrays
```
