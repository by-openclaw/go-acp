# Command — GetDirectory

APP tag **2**, `number=32`. The consumer's request for a Node's children.
The single most common command on the wire — every tree walk fires one of
these per visited Node.

## Spec

Ember+ Documentation v2.50, p. 86.

```
Command ::= [APPLICATION 2] SEQUENCE {
    number        [0] CommandType,
    dirFieldMask  [1] FieldFlags OPTIONAL,     -- GetDirectory only
    invocation    [2] Invocation OPTIONAL      -- Invoke only
}

CommandType ::= INTEGER {
    subscribe(30),
    unsubscribe(31),
    getDirectory(32),
    invoke(33)
}

FieldFlags ::= INTEGER {
    sparse(-2),
    all(-1),
    default(0),
    identifier(1),
    description(2),
    tree(3),
    value(4),
    connections(5)
}
```

The fixture shows the consumer sending a GetDirectory under a specific Node
path (`router.Functions`) — the provider will respond with that Node's
ElementCollection populated.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 464 bytes.
- Extracted from: `bin/emberplus_glow_lua.pcapng` frame 125.
- Frozen tree: [`tshark.tree`](tshark.tree) — note `Value (int): 32`.

## CLI equivalent

```bash
# Every walk command sends GetDirectory at each visited node
./bin/acp walk 127.0.0.1 --protocol emberplus --port 9092
```
