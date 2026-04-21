# Command — Subscribe

APP tag **2**, `number=30`. Consumer tells the provider to start sending
value-change announcements (or stream ticks, for stream parameters) for the
Parameter or Matrix under the command's position.

## Spec

Ember+ Documentation v2.50, p. 86.

```
CommandType.subscribe(30)
```

Subscribe is positional: the consumer wraps it in the target element tree
(Node → ... → Parameter → children → Command{30}). It has no body — just
the command number.

The fixture shows a Subscribe request for a stream parameter — the provider
will start pushing StreamCollection frames (see
[`stream_collection/`](../stream_collection/)) for every tick.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 488 bytes.
- Extracted from: `bin/emberplus_glow_stream_subscribe_lua.pcapng` frame 52.
- Frozen tree: [`tshark.tree`](tshark.tree) — note `Value (int): 30`.

## CLI equivalent

```bash
# acp stream walks + auto-subscribes to every streamIdentifier parameter
./bin/acp stream 127.0.0.1 --protocol emberplus --port 9092
# acp watch subscribes to all value changes at tree level
./bin/acp watch 127.0.0.1 --protocol emberplus --port 9092
```
