# Preset (object type 1)

A preset child — pid 7 `preset_depth` lists valid idx values; pids 8/9/10/11
(value, default, min, max) repeat once per idx in every get_object reply.
This fixture uses `depth=1` (single ACTIVE INDEX slot) to keep the wire
shape readable; higher depths behave identically with N-times repetition.

## Spec

`acp2_protocol.pdf` §2 obj_type=1 + §5 "Preset depth". See also
[`internal/acp2/CLAUDE.md`](../../../CLAUDE.md) "Preset depth" and "Object
types".

Key properties on the wire:
- pid 1 `object_type` = 1
- pid 5 `number_type` — here s32 (2); spec also allows u32/float/etc.
- pid 7 `preset_depth` — u32[] of valid idx values. For `depth=1` the
  payload is `[0]`.
- pid 8 `value` — repeated once per idx value listed in pid 7.
- pid 9/10/11/12/13 — default/min/max/step/unit; default/min/max repeat
  once per idx alongside pid 8 (so consumers pair them positionally).

## How to declare one in a canonical tree

Use the bare `preset` token on `Parameter.format` and specify depth plus
a numeric wire type:

```json
{ "number": 7, "identifier": "PresetGain", ...,
  "type": "integer", "value": 0, "default": 0, "minimum": -60, "maximum": 20, "step": 1,
  "format": "preset,s32,depth=1", "unit": "dB", ... }
```

`deriveACP2Type` in [`internal/acp2/provider/tree.go`](../../../provider/tree.go)
detects the `preset` token and routes to `ObjTypePreset`. `presetDepthHint`
extracts `depth=N`. Depth defaults to 1 when absent.

## Source

- Pcap: [`capture.pcapng`](capture.pcapng) — 1 frame, 532 bytes.
- Extracted from: `bin/acp2_fixtures.pcapng` frame 58.
- Frozen tree: [`tshark.tree`](tshark.tree) — reply for
  `get_object(slot=1, obj-id=7)` = `PresetGain`.

## CLI equivalent

```bash
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label PresetGain
./bin/dhs consumer acp2 get 127.0.0.1 --port 2072 --slot 1 --label PresetGain --idx 0
```

## Related open work

[#79 (provider-acp2): Enum pid 15 options layout — validate against Cerebrum](https://github.com/by-rune/acp/issues/79)
— preset and enum both surface pid 5/8/15 encoding questions.
