# TSL v3.1 frame (UDP, 18 bytes — §3.0)

| Wire | Default port | Tally model |
| --- | ---: | --- |
| UDP | 4000 | 4× binary tallies + 2-bit brightness (no colour) |

## Wire shape

```
| 0    | HEADER     | 1 byte  | (addr & 0x7F) | 0x80; addr range 0..126 |
| 1    | CTRL       | 1 byte  | bits 0-3 = tallies 1..4; bits 4-5 = brightness; bit 6 reserved (clear); bit 7 = 0 |
| 2..17| DATA       | 16 ASCII| 0x20-0x7E, **space-padded (0x20)** to 16 |
```

Total: 18 bytes. No CHKSUM — that arrived in v4.0.

## Compliance notes the codec emits

- `tsl_reserved_bit_set` — CTRL bit 6 set on rx.
- `tsl_label_length_mismatch` — DATA segment != 16 bytes.
- `tsl_v31_null_pad` — DATA padded with 0x00 instead of 0x20.

## Fixtures (when captured)

- `frames.jsonl` — at least one well-formed frame + one each of the
  three deviations above so the codec round-trip exercises the full
  ComplianceNote vocabulary.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
