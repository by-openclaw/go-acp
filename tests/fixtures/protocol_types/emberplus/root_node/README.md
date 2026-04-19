# Root → RootElementCollection → Node

APP tag **0** (Root) → **11** (RootElementCollection) → **3** (Node).
Every Glow frame on the wire starts with this sequence.

## Spec

Ember+ Documentation v2.50, pp. 87 (Node), 93 (Root CHOICE).

```
Root ::= CHOICE {
    elements [APPLICATION 11] RootElementCollection,
    streams  [APPLICATION 6]  StreamCollection
}

RootElementCollection ::= SEQUENCE OF [0] Element

Node ::= [APPLICATION 3] SEQUENCE {
    number   [0] Integer32,                  -- child index inside parent
    contents [1] SET OF NodeContents OPTIONAL,
    children [2] ElementCollection OPTIONAL
}
```

The fixture shows a nested Node tree — `router.Device.Channels.Channel1` —
coming back as a provider response to a `GetDirectory` probe.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 516 bytes.
- Extracted from: `bin/emberplus_glow_stream_subscribe_lua.pcapng` frame 1.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/acp walk 127.0.0.1 --protocol emberplus --port 9092
```
