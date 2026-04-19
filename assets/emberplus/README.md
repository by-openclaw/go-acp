# Ember+ Protocol

## What is Ember+

Ember+ is an open, asynchronous control and monitoring protocol developed by
Lawo. It models device state as a tree of Nodes, Parameters, Matrices, and
Functions, serialised with BER over the S101 framing layer. Ember+ is the
lingua franca of modern broadcast audio routing and mixing consoles (Lawo,
DHD, Riedel, Solid State Logic, Evertz SDVN).

## Transport

- TCP only. No UDP variant.
- S101 framing: BoF `0xFE`, EoF `0xFF`, escape `0xFD` with XOR-0x20 rule.
- CRC-CCITT16 (reflected polynomial `0x8408`, init `0xFFFF`, inverted result).
- Common provider ports: **9000** (vendor-specific), **9090**, **9092**
  (TinyEmber+ default). No IANA-assigned number.

## Wire format summary

```
S101 frame: BoF | escaped(header + BER payload + CRC_LE) | EoF
Header (4 bytes): slot(1) msgType(1=0x0E) command(1) version(1=0x01)
For EmBER data command (0x00): +5 bytes = flags(1) dtd(1=Glow) appLen(1=2) appMinor(1=0x1F) appMajor(1=0x02)
BER payload: Glow APPLICATION-tagged tree (Root / RootElementCollection / ...)
```

See `CLAUDE.md` and [`Ember+ Documentation.pdf`](Ember%2B%20Documentation.pdf)
for the full spec.

## CLI examples

```bash
# Discover via TCP connect
./bin/acp info localhost --protocol emberplus --port 9092

# Walk provider tree
./bin/acp walk localhost --protocol emberplus --port 9092 --capture out/

# Watch for announcements
./bin/acp watch localhost --protocol emberplus --port 9092

# Export canonical JSON with templates + labels + gain resolved
./bin/acp walk localhost --protocol emberplus --port 9092 \
    --templates inline --labels pointer --gain inline --capture out/
```

## Provider emulators shipped under `tools/`

| Tool                   | Port | Purpose                      |
|------------------------|------|------------------------------|
| TinyEmberPlus-2.10     | 9092 | Parameter / Node sandbox     |
| TinyEmberPlusRouter-1.6| 9000 | Matrix-capable router        |
| EmberPlusView-1.6.2    | —    | GUI consumer for exploration |

## Reference

- Spec: [Ember+ Documentation.pdf](Ember%2B%20Documentation.pdf)
- Formulas reference: [Ember+ Formulas.pdf](Ember%2B%20Formulas.pdf)
- Wireshark dissector: [dissector_emberplus.lua](dissector_emberplus.lua) — install per [docs/wireshark.md](../../docs/wireshark.md)
- Canonical schema: [docs/protocols/emberplus/consumer.md](../../docs/protocols/emberplus/consumer.md)

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
