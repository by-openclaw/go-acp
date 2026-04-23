# Parameter

APP tag **1**. A leaf holding a typed value (integer, real, string, bool,
octets, enum, trigger). The backbone of any Ember+ parameter tree.

## Spec

Ember+ Documentation v2.50, p. 85.

```
Parameter ::= [APPLICATION 1] SEQUENCE {
    number   [0] Integer32,
    contents [1] SET OF ParameterContents OPTIONAL,
    children [2] ElementCollection OPTIONAL
}

ParameterContents ::= CHOICE {
    identifier [0] EmberString,
    description [1] EmberString,
    value       [2] Value,                 -- current
    minimum     [3] MinMax OPTIONAL,
    maximum     [4] MinMax OPTIONAL,
    access      [5] ParameterAccess OPTIONAL,    -- 1=r, 2=w, 3=rw
    format      [6] EmberString OPTIONAL,
    enumeration [7] EmberString OPTIONAL,
    factor      [8] Integer32 OPTIONAL,
    isOnline    [9] BOOLEAN OPTIONAL,
    formula     [10] EmberString OPTIONAL,
    step        [11] Integer32 OPTIONAL,
    default     [12] Value OPTIONAL,
    type        [13] ParameterType OPTIONAL,
    streamIdentifier       [14] Integer32 OPTIONAL,
    enumMap                [15] StringIntegerCollection OPTIONAL,
    streamDescriptor       [16] StreamDescription OPTIONAL,
    schemaIdentifiers      [17] EmberString OPTIONAL,
    templateReference      [18] RELATIVE-OID OPTIONAL
}
```

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 472 bytes.
- Extracted from: `bin/emberplus_glow_glow_lua.pcapng` frame 19.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Read a parameter
./bin/acp get 127.0.0.1 --protocol emberplus --port 9092 --path router.labels.target.Gain
# Write a parameter
./bin/acp set 127.0.0.1 --protocol emberplus --port 9092 --path router.labels.target.Gain --value -3.0
```
