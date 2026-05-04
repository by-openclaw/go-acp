# TSL v4.0 frame (UDP, 20+ bytes — §4.0)

| Wire | Default port | Tally model |
| --- | ---: | --- |
| UDP | 4000 (testbed: 4004) | v3.1 binary tallies + XDATA 2-bit colour per LH / Text / RH × L / R display |

Full-compat extension of v3.1. The trailing CHKSUM + VBC + XDATA bytes
are absent in v3.1 frames — receivers MUST NOT confuse the two.

## Wire shape

```
| v3.1 prefix    | 18 bytes | HEADER + CTRL + DATA (see v31_frame/) |
| CHKSUM         | 1 byte   | 7-bit two's-complement of HEADER+CTRL+DATA |
| VBC            | 1 byte   | bit 7 = 0; bits 6-4 = minor version (v4.0 → 0); bits 3-0 = XDATA byte count |
| XDATA          | N bytes  | per VBC.bits3-0; min-version 0 = 2 bytes (Display L, Display R) |
```

Each XByte: bit 7 = 0; bit 6 reserved; bits 5-4 LH tally (2-bit colour);
bits 3-2 Text tally; bits 1-0 RH tally. Colours: 0=OFF, 1=RED,
2=GREEN, 3=AMBER.

## Compliance notes the codec emits

- `tsl_checksum_fail` — CHKSUM mismatch (consumer still parses body).
- `tsl_version_mismatch` — VBC minor version != 0.
- `tsl_control_data_undefined` — CTRL.6 = 1 (control data flag).
- All v3.1 notes (reserved bit, label length, null pad).

## Fixtures (when captured)

- `frames.jsonl` — Lawo VSM v4.0 producer captures (per CLAUDE.md
  "Lawo VSM Studio | v3.1 + v4.0 + v5.0 producer | ... live-validated
  2026-04-26"). Pair every PGM red, PVW green, ISO amber tally so the
  XByte parser round-trips all three colours.
- `capture.pcapng` + `tshark.tree` — dissector artefacts.
