# Command — Unsubscribe

APP tag **2**, `number=31`. Counterpart of Subscribe. Consumer tells the
provider to stop sending announcements / stream ticks for the wrapping
element.

## Spec

Ember+ Documentation v2.50, p. 86.

```
CommandType.unsubscribe(31)
```

Like Subscribe, Unsubscribe is positional with no body. `acp stream` fires
this on SIGINT to leave the provider clean.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 488 bytes.
- Extracted from: `bin/emberplus_glow_stream_subscribe_lua.pcapng` frame 105.
- Frozen tree: [`tshark.tree`](tshark.tree) — note `Value (int): 31`.

## CLI equivalent

```bash
# Automatic — sent on Ctrl-C
./bin/acp stream 127.0.0.1 --protocol emberplus --port 9092
```
