# Matrix Connection

APP tag **16**. A single connection entry inside a Matrix's `connections[5]`
sequence. Represents one crosspoint state (target ← source[s], operation).

## Spec

Ember+ Documentation v2.50, p. 89.

```
Connection ::= [APPLICATION 16] SEQUENCE {
    target      [0] Integer32,
    sources     [1] RELATIVE-OID OPTIONAL,    -- list of source indices
    operation   [2] ConnectionOperation OPTIONAL,
    disposition [3] ConnectionDisposition OPTIONAL
}

ConnectionOperation ::= INTEGER {
    absolute(0),
    connect(1),
    disconnect(2)
}

ConnectionDisposition ::= INTEGER {
    tally(0),       -- current state (announcement)
    modified(1),    -- change request
    pending(2),
    locked(3)
}
```

The fixture is the same Matrix frame as [`matrix/`](../matrix/) — connections
appear inside the Matrix element under tag `[5]`. The focus here is the inner
Connection SEQUENCE, not the outer Matrix.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 480 bytes.
- Extracted from: `bin/emberplus_glow_mtx_lua.pcapng` frame 41.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp matrix 127.0.0.1 --protocol emberplus --port 9000 \
    --path router.matrix --target 0 --source 3 --op connect
```
