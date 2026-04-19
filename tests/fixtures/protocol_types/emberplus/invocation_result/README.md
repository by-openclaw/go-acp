# InvocationResult

APP tag **23**. Provider's reply to an Invoke Command. Carries the returned
tuple plus success / failure status.

## Spec

Ember+ Documentation v2.50, p. 92.

```
Root ::= CHOICE {
    elements [APPLICATION 11] RootElementCollection,
    streams  [APPLICATION 6]  StreamCollection,
    result   [APPLICATION 23] InvocationResult          -- NB: direct CHOICE branch
}

InvocationResult ::= [APPLICATION 23] SEQUENCE {
    invocationID [0] Integer32,                         -- echoes Invocation
    success      [1] BOOLEAN OPTIONAL,                  -- defaults true
    result       [2] SEQUENCE OF Tuple OPTIONAL         -- return tuple
}
```

Note that InvocationResult is a **direct CHOICE branch of Root**, not wrapped
in RootElementCollection. The dissector must handle this alternative form
at the top level.

The fixture shows the reply to [`function_invoke/`](../function_invoke/) —
`invocationID=8, result={12}`. (The `add(a,b)` arguments in that fixture
were 2+4; this result `12` is from a different invocation — any integer
reply proves the decoder round-trips correctly.)

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 440 bytes.
- Extracted from: `bin/emberplus_glow_functions_lua.pcapng` frame 348.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp invoke 127.0.0.1 --protocol emberplus --port 9092 \
    --path router.Functions.add --args '[2,4]'
# → prints returned tuple from InvocationResult
```
