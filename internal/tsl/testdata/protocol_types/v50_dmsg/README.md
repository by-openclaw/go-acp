# TSL v5.0 packet (UDP — §5.0)

| Wire | Default port | Tally model |
| --- | ---: | --- |
| UDP, ≤ 2048 B | 8901 (Kaleido) | LH / Text / RH (3× 2-bit colour) + 2-bit brightness, multi-DMSG |

v5.0 is a brand-new protocol — no v3.1 / v4.0 backward compatibility.
All 16-bit values little-endian.

## Wire shape

```
| PBC      | 2 LE  | total byte count of body (PBC excluded) |
| VER      | 1     | minor version (v5.00 → 0) |
| FLAGS    | 1     | bit 0 = UTF-16LE (1) vs ASCII (0); bit 1 = SCONTROL; bits 2-7 reserved |
| SCREEN   | 2 LE  | 0..65534; 0xFFFF = broadcast |
| body     | …     | DMSG+ or SCONTROL (per FLAGS bit 1) |
```

Each DMSG:

```
| INDEX     | 2 LE  | 0..65534; 0xFFFF = broadcast |
| CONTROL   | 2 LE  | bits 0-1 RH; 2-3 Text; 4-5 LH; 6-7 brightness; 8-14 reserved; 15 control-data flag |
| LENGTH    | 2 LE  | only when CONTROL.15 = 0 |
| TEXT      | LENGTH bytes | encoding per FLAGS.0 |
| CONTROL_DATA | … | only when CONTROL.15 = 1 (undefined in v5.0) |
```

## Compliance notes the codec emits

- `tsl_reserved_bit_set` — CONTROL bits 8-14 set.
- `tsl_control_data_undefined` — CONTROL bit 15 set (frame parses; payload opaque).
- `tsl_unknown_display_index` — DMSG INDEX not modelled by the consumer.
- `tsl_broadcast_received` — SCREEN=0xFFFF or INDEX=0xFFFF.
- `tsl_charset_transcode` — UTF-16LE label transcoded to UTF-8.

## Fixtures (when captured)

- `frames.jsonl` — Miranda IP Emulator captures (per CLAUDE.md
  "Miranda TSL IP Emulator v1.02 | v5.0 UDP + TCP | ... live-validated
  2026-04-26 (single-DMSG + 5-DMSG group via 'Group display
  messages')").
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
