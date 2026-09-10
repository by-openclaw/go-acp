# protocol_types — one folder per wire structure

Per ADR-0025 deliverable 6. Each folder holds a small slice of real traffic
showing one structure, the dissector's rendering of it, and a note on what the
bytes mean and what people get wrong about them.

    <type>/capture.pcapng   the frames themselves
    <type>/tshark.tree      dhs_snell_rollcall.lua's rendering of them
    <type>/README.md        the spec reference and what these bytes show

## Where they came from

Two real devices, one per generation:

- Every folder but one was sliced out of
  [`tests/fixtures/snell-rollcall/centra-sirius800.pcapng`](../../../../tests/fixtures/snell-rollcall/centra-sirius800.pcapng)
  — a live capture of our client driving the vendor Snell Centra controller
  configured as a Sirius 800. Nothing in those is synthetic and nothing was
  edited.
- `menu_16bit/` comes from the Snell IQ modular frame at `10.6.255.113`, whose
  walk is kept as a JSONL wire trace in `../fixtures/iq-frame-IQDBE00/`. A
  trace keeps RollCall frames rather than packets, so its pcap was built with
  Wireshark's `text2pcap`: the RollCall bytes are the card's own and the
  IP/TCP headers around them are stand-ins. Its README says exactly how.

The slices are small on purpose: a fixture is only reviewable if a person can
read all of it.

Regenerate a tree after changing the dissector:

    tshark -r <type>/capture.pcapng \
      -X lua_script:internal/snell-rollcall/wireshark/dhs_snell_rollcall.lua \
      -V -O dhs_snell_rollcall > <type>/tshark.tree

## What is here

| Folder | Structure | Why it is worth keeping |
|---|---|---|
| `session/` | SP_CALL / SP_ACK / SP_TERM | both generations negotiated to one unit, seconds apart |
| `identity/` | ID_STR | the type id that decides what every command number on a node means |
| `status/` | STATUS_STR | the per-node state an enumeration reports |
| `device_map/` | block transfer | how a plant is enumerated, one item per round trip |
| `menu_16bit/` | FUNC_STR over a block transfer | a real card's menu in the older generation, one line per round trip |
| `menu_32bit/` | MENUITEM_STR | a control surface, and a matrix node that deliberately has none |
| `value_32bit/` | VALUE_STR | a read and its reply, and the ambiguity when a node type is unknown |
| `source_pin/` | crosspoint as Data Transfer Params | the take, the reply carrying the pin from *before*, and the tally |
| `names_file/` | filename + checksum | why names travel in files rather than in commands |
| `back_channel/` | SP_BKCHNREADY / SP_REPFCHG | subscribing, and why it does not replay state |
| `display/` | DISPDATA | unsolicited frames a client must skip rather than reject |
| `refusal/` | NACK / INVCMD / BUSY / INVSESS | four different noes, and where the Centra sends the wrong one |

## What is not here, and why

**The 16-bit value structures** (`FUNCSTATUS_STR` — SP_GETFSTAT, SP_RETFSTAT,
SP_SETPARAM) have no real-device capture yet. The IQ frame speaks the 16-bit
generation — it does not advertise `SV_LONGSTR`, and both sessions in its trace
were opened without it — but that walk read the card's menu and nothing else,
so the trace holds 167 `FUNC_STR` lines and not one value. Reading and writing
values on it is the capture to take next. Our own producer serves this
generation too (`serve --generation 16`), but that would be our bytes rather
than a device's, which is the difference this folder exists to preserve.

**The file service** (SP_FILEOPEN and its family) is not in this capture: the
Centra names its names-files under a path its own file service will not serve,
so the connector never opens one there. See `names_file/README.md`.

Both are gaps in the *fixtures*, not in the connector — the codec, the
provider and the dissector all implement them, and they are covered by unit
tests against bytes taken from the specification.

## Related fixtures

| Path | What it is |
|---|---|
| `../fixtures/iq-frame-IQDBE00/` | a real IQ frame card walk: 367 frames as JSONL, replayed by the codec in CI |
| `../integration-test/` | a DM and manifest, so the producer serves a real card with no device present |
| `../exports/` | canonical trees, including the two-matrix router plant |
| `../measured/` | notes taken off the Centra by hand, where a measurement has no byte form |
