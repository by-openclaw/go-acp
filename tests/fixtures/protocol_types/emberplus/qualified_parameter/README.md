# QualifiedParameter

APP tag **9**. A Parameter addressed by full path (OID-like), so the
provider can announce a single value change without re-transmitting the
whole parent chain.

## Spec

Ember+ Documentation v2.50, p. 85.

```
QualifiedParameter ::= [APPLICATION 9] SEQUENCE {
    path     [0] RELATIVE-OID,
    contents [1] SET OF ParameterContents OPTIONAL,
    children [2] ElementCollection OPTIONAL
}
```

QualifiedParameter is the form normally used for value-change announcements
(consumer has already walked the tree, so the path uniquely identifies the
leaf without ambiguity).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 592 bytes.
- Extracted from: `bin/emberplus_glow_mtx_labels_param_lua.pcapng` frame 19.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Subscribe to value changes — they arrive as QualifiedParameter frames
./bin/acp watch 127.0.0.1 --protocol emberplus --port 9092
```
