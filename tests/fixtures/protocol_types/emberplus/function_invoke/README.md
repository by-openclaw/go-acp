# Function + Command{Invoke} + Invocation

APP tags **19** (Function), **2** (Command `number=33`), **22** (Invocation).
A consumer's RPC call — the frame that wraps a Function's path, wraps an
`Invoke` Command, and carries the argument tuple inside an Invocation.

## Spec

Ember+ Documentation v2.50, p. 91.

```
Function ::= [APPLICATION 19] SEQUENCE {
    number    [0] Integer32,
    contents  [1] SET OF FunctionContents OPTIONAL,
    children  [2] ElementCollection OPTIONAL
}

FunctionContents ::= CHOICE {
    identifier [0] EmberString,
    description [1] EmberString,
    arguments  [2] SEQUENCE OF TupleItemDescription,   -- APP 21
    result     [3] SEQUENCE OF TupleItemDescription,
    templateReference [4] RELATIVE-OID OPTIONAL
}

CommandType.invoke(33)

Invocation ::= [APPLICATION 22] SEQUENCE {
    invocationID [0] Integer32,                    -- consumer-assigned
    arguments    [1] SEQUENCE OF Tuple OPTIONAL    -- positional args
}
```

The fixture shows an `add(a, b)` call — two integer arguments (2 and 4) in the
Invocation tuple. The provider responds with an InvocationResult carrying
the sum (6) — see [`invocation_result/`](../invocation_result/).

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 496 bytes.
- Extracted from: `bin/emberplus_glow_functions_lua.pcapng` frame 346.
- Frozen tree: [`tshark.tree`](tshark.tree) — `Value (int): 33` (Invoke).

## CLI equivalent

```bash
./bin/acp invoke 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.Functions.add --args '[2,4]'
```
