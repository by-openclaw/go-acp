# TSL v5.0 over TCP with DLE/STX wrapper (§5.0 §Phy)

| Wire | Default port | Tally model |
| --- | ---: | --- |
| TCP | 8901 (testbed: 8902) | Same as `v50_dmsg/` — only the framing differs |

Spec-required wrapper on TCP. UDP frames travel raw; TCP frames are
prefixed with DLE/STX and any 0xFE byte inside the body is doubled.

## Wire shape

```
| 0xFE         | DLE — frame start  |
| 0x02         | STX                |
| body         | as v50_dmsg/, with every 0xFE byte stuffed (0xFE 0xFE) |
```

The decoder consumes the wrapper, un-stuffs 0xFE bytes, and reads PBC
to bound the body — see `internal/tsl/codec/v50_dle_stx.go::ReadFrame`.

## Compliance notes the codec emits

- `tsl_v5_tcp_unwrapped` — A v5.0 TCP frame received WITHOUT the
  wrapper (TallyArbiter ships such frames; spec rejects them).
  Consumer tolerates on rx + fires.

## Fixtures (when captured)

- `frames.jsonl` — Miranda IP Emulator TCP captures showing both:
  (a) a wrapped frame with 0xFE byte-stuffing inside the body, and
  (b) an unwrapped frame from a non-spec producer for the
  `tsl_v5_tcp_unwrapped` event.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
