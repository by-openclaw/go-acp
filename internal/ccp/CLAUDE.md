# CLAUDE.md — ccp (Axon Cortex Control Panel Protocol)

## Spec

*Protocol Description — Cortex Control Panel Protocol (CCPP) 1.18*, Axon
Digital Design, 15 August 2019, 34 pages. The PDF sits outside git
(`internal/cortex/assets/Cortex CCPP-1v18.pdf`, Axon copyright). Section
numbers below are its own.

## Transport (§1.0.0, §1.1.0)

- TCP, port **2071** (shared with the Axon Control Protocol). The panel is
  the server; the controller connects.
- At least **two clients** at once: replies go only to the asker,
  unsolicited frames (button, GPI) to every client. dhs connects next to
  Cerebrum.
- With **no client** the panel shows its IP address and an off-line screen
  after ~5 s. Labels therefore last only while a client holds the
  connection: `dhs consumer ccp panel` holds it; a one-shot `set` does not.
- Framing: one command byte + data, **no length field**. The command byte
  fixes the length (`codec.Next`); 0x82 is 3 data bytes, 4 for a T-bar
  (action 0x05); 0x85/0x86 carry a length byte; 0x8E a 2-byte size; 0xF1
  is up to 9 ASCII bytes, NUL-terminated — the CCP-1601 sends 9 and no NUL.
  An unknown byte cannot be skipped; the read buffer is dropped and a
  compliance note raised.

## Measured on the lab panel (10.6.255.105, 2026-10-25)

| What | Value |
|---|---|
| Poll reply | `81 01 0d 01 00 01` — CCP-1601, protocol 13, PSU A on, PSU B off, refresh required |
| Firmware | `DEL869-12` |
| Read MAC (0x76) | **refused**: `83 02 76 00` (spec: works on the CCP-1601) |
| DHCP / subnet / gateway | on / 255.255.255.0 / 192.168.0.1 (the factory gateway, not the lease) |
| Buttons | IDs 0x01-0x10, down `82 01 id 00`, up `82 02 id 00` |
| Rotary | ID 0x11: `82 03 11 n` right, `82 04 11 n` left, n = steps (1..11 by speed); push = down/up |
| Colour 0x05 | 6-bit index 0b00RRGGBB; colours picked by eye in `codec.NamedColours` |

### Bitmap layout (§1.2.6 leaves it to the button)

64×32, 256 bytes. Measured by sending a known pattern and reading the
button by eye, then confirmed with the inverse: columns from the right
edge (x = 63 first), 4 bytes per column; the bytes are 8-row bands from
the bottom with neighbouring bands swapped (byte 0 → rows 16-23, byte 1 →
24-31, byte 2 → 0-7, byte 3 → 8-15); inside a band the MSB is the lowest
row. `codec.Bitmap.Encode` / `codec.Decode`, pinned by tests.

## Layout

```
codec/      stdlib: commands, framing, poll/button/error parsers, 64×32 bitmap + 5×7 font
consumer/   package ccp: TCP session, Plugin (registry name "ccp")
```

CLI: neutral `info walk get set watch`; own `panel` (hold, layout CSV,
`--knob N=NAME,MIN,MAX,STEP,UNIT[,INIT]` driven by the rotary) and `colours`.

## Not done

- Set IP/subnet/gateway/DHCP (0x20-0x23, XOR checksum since v16), MAC,
  bootload: deliberately not exposed.
- GPI/GPO, serial port, T-bar: codec only, untested (the CCP-1601 has none).
- Knobs change local values; binding a knob to a real device parameter is
  the next step.
- Wireshark dissector, integration test file, fixtures from a capture.
