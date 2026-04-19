# Matrix

APP tag **13**. A routing element — targets on one axis, sources on the other,
connections in between. Core to broadcast router modelling.

## Spec

Ember+ Documentation v2.50, pp. 88-89.

```
Matrix ::= [APPLICATION 13] SEQUENCE {
    number     [0] Integer32,
    contents   [1] SET OF MatrixContents OPTIONAL,
    children   [2] ElementCollection OPTIONAL,
    targets    [3] SEQUENCE OF Target OPTIONAL,       -- APP 14
    sources    [4] SEQUENCE OF Source OPTIONAL,       -- APP 15
    connections[5] SEQUENCE OF Connection OPTIONAL    -- APP 16
}

MatrixContents ::= CHOICE {
    identifier            [0] EmberString,
    description           [1] EmberString,
    type                  [2] MatrixType,       -- 0=oneToN, 1=oneToOne, 2=nToN
    addressingMode        [3] MatrixAddressingMode,
    targetCount           [4] Integer32,
    sourceCount           [5] Integer32,
    maximumTotalConnects  [6] Integer32 OPTIONAL,
    maximumConnectsPerTarget [7] Integer32 OPTIONAL,
    parametersLocation    [8] ParametersLocation OPTIONAL,
    gainParameterNumber   [9] Integer32 OPTIONAL,
    labels                [10] SEQUENCE OF Label OPTIONAL,     -- APP 18
    schemaIdentifiers     [11] EmberString OPTIONAL,
    templateReference     [12] RELATIVE-OID OPTIONAL
}
```

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 480 bytes.
- Extracted from: `bin/emberplus_glow_mtx_lua.pcapng` frame 41.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Walk a router, reveal matrix contents
./bin/acp walk 127.0.0.1 --protocol emberplus --port 9000
# Set a crosspoint (target 0 ← source 2)
./bin/acp matrix 127.0.0.1 --protocol emberplus --port 9000 --path router.matrix --target 0 --source 2
```
