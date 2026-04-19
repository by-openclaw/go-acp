# StreamEntry / StreamCollection

APP tags **5** (StreamEntry) + **6** (StreamCollection). High-rate parameter
value delivery — consumer subscribes once, provider fires values as fast as
it likes inside a StreamCollection that addresses Parameters by
streamIdentifier (NOT by path).

## Spec

Ember+ Documentation v2.50, p. 93.

```
Root ::= CHOICE {
    elements [APPLICATION 11] RootElementCollection,
    streams  [APPLICATION 6]  StreamCollection
}

StreamCollection ::= [APPLICATION 6] SEQUENCE OF StreamEntry

StreamEntry ::= [APPLICATION 5] SEQUENCE {
    streamIdentifier [0] Integer32,
    streamValue      [1] Value
}
```

StreamCollection is a distinct CHOICE branch of Root — streams do NOT
travel through RootElementCollection. A consumer must recognise both
branches during dissection.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 500 bytes.
- Extracted from: `bin/emberplus_glow_lua.pcapng` frame 9.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
# Subscribe; each tick prints streamIdentifier = value
./bin/acp stream 127.0.0.1 --protocol emberplus --port 9092
```
