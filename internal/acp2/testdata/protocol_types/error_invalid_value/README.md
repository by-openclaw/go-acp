# Error: invalid_value (stat=5)

The provider rejects a set_property whose value byte payload violates
the target object's value-space constraint.

## Provider policy: clamp vs reject

`internal/acp2/provider/set.go` applies a different policy per object
type. Summarising from `applySet*`:

| Object type              | Out-of-range policy             | Error returned |
|--------------------------|---------------------------------|----------------|
| Number (s8..u64, float)  | **CLAMP** to [min, max] silently | none — set succeeds with clamped value |
| Enum                     | **REJECT** when idx ≥ len(options) | stat=5 invalid_value |
| IPv4                     | Reject if len(bytes) ≠ 4         | stat=5 |
| String                   | Truncate to pid 6 max_length     | none |

Only Enum and IPv4 length-mismatch trip stat=5. Numeric out-of-range is
silently clamped — confirmed by the `applySetNumber` code and the doc
comment at `set.go:15`.

## Spec

`acp2_protocol.pdf` §4 "Error stat codes", stat=5 + §5.2.2 enum value.

Wire layout:
- Request:  `type=0, mtid=N, func=3 set_property, pid=8, obj-id=5 (Mode),
  idx=0, property(value vtype=9, data=u32 99)`.
- Error reply: `type=3, mtid=N, stat=5, pid=0`.

## Why `--raw` is needed here

The CLI's `set --value <s>` label-resolution path (see `consumer/plugin.go`
`encodeSetProperty`) maps any unknown enum label to index 0 before
encoding. Passing `--value 99` against the `Mode` enum sends `u32=0` on
the wire — the provider accepts it and emits an announce, not stat=5.

`--raw` bypasses client-side encoding and sends the hex bytes verbatim:
`--raw 00000063` writes `u32=0x63=99`, the provider checks `99 >= 2`
(len([Off, On])) and fires ErrInvalidValue.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 2 frames, 548 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frames 544 + 546 —
  `set_property(slot=1, obj=5 Mode, value=99)` fired via `--raw`.
- Frozen tree: [`tshark.tree`](tshark.tree).

## CLI equivalent

```bash
./bin/dhs consumer acp2 set 127.0.0.1 --port 2072 --slot 1 --label Mode --raw 00000063
```
