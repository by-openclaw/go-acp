# Snell RollCall

RollCall is Snell's device control protocol: units on a network, each with
ports, each port carrying a menu of objects a client reads and writes. It is
what an IQ frame, a Centra controller and a Sirius router all speak.

| Role | Doc | State |
|---|---|---|
| Operator runbook | [runbook.md](runbook.md) | bring-up, verbs, what to check when it does not work |
| Consumer | [consumer.md](consumer.md) | the outbound connector |
| Provider | [provider.md](provider.md) | serving a canonical tree as a gateway |
| What the wire looks like | [../CLAUDE.md](../CLAUDE.md) | wire format, quirks, what not to do |
| Specification coverage | [spec-coverage.md](spec-coverage.md) | every message type and structure, checked by a test |
| Measured behaviour | [oracle-centra.md](oracle-centra.md) | what a vendor controller actually does |
| Device model and UI | [dm-and-ui.md](dm-and-ui.md) | how the tree maps to the neutral model |

## The two things that make RollCall unusual

**There are two wire generations and one connection carries both.** The
original is 16-bit: command numbers and menu indices fit sixteen bits, and
strings live in twenty-byte fixed fields. The 2014 extension widens both. Which
one a session speaks is decided by the `SV_LONGSTR` bit in its `SP_CALL`, not by
the device, so a client with two sessions to one unit can be speaking both at
once. Services are granted all-or-nothing: a call naming one service the peer
does not have is refused entirely.

**A router has no menu.** Every other device describes itself through the menu
service. A router controller serves its routing, its names, its protects and
its salvos as control variables in a flat 32-bit command space, whose numbers a
client works out by arithmetic from a root table at command 100. Walking a
router's menu finds three lines and nothing useful. See
[oracle-centra.md](oracle-centra.md) §2.

## Layout

```
internal/snell-rollcall/
  codec/          the wire format, stdlib-only (ADR-0006)
    dtp/          Data Transfer Params, which the routing interface rides on
    router/       the Full Control command arithmetic and its file formats
  session/        links, sessions, channels, the back channel, keepalive
  consumer/       the outbound connector, both generations, router control
  provider/       serving a canonical tree as a RollCall gateway
  wireshark/      dhs_snell_rollcall.lua — the byte-exact reference
  integration/    replay tests against captured traffic (-tags integration)
  testdata/       trees and fixtures
  assets/         the specification, the vendor sources, the emulators
```

## Specification and vendor material

| What | Where |
|---|---|
| RollCall Technical Specification Rev 14 | [`assets/Protocol/Docs`](../assets/Protocol/Docs) |
| Full Control Command Set (routers) | [`assets/Protocol/Docs/Full Control Command Set.docx`](../assets/Protocol/Docs) |
| Full RollCall Control of Routers (the design behind it) | same directory |
| Vendor C sources (`FileServer.c`, `MsgHandlers.c`, …) | [`assets/Protocol/Source`](../assets/Protocol/Source) |
| Vendor headers (`rc3comm.h`, `RC3FILE.H`, `RC3TYPES.H`) | same tree |
| Centra controller emulator, runs as a Sirius 800 | [`assets/Emulator/RouterSimulator`](../assets/Emulator/RouterSimulator) |
| Wireshark dissector | [`wireshark/dhs_snell_rollcall.lua`](../wireshark/dhs_snell_rollcall.lua) |

When the specification, a device and our codec disagree, the dissector breaks
the tie first: it is what you open before you open the Go code.

## Quick start

```
# What is on the other end
dhs consumer rollcall info 10.6.250.105

# What one node offers
dhs consumer rollcall walk 10.6.250.105 --slot 1

# Read and write an object
dhs consumer rollcall get 10.6.250.105 --slot 1 --label gain
dhs consumer rollcall set 10.6.250.105 --slot 1 --label gain --value -6

# A router
dhs consumer rollcall router 10.6.250.105
dhs consumer rollcall route  10.6.250.105 --matrix 1 --level 1 --dest 4 --source 9
dhs consumer rollcall tally  10.6.250.105 --matrix 1 --level 1 --names

# Serve a tree as a gateway
dhs producer rollcall serve --tree tree.json --port 2050
```
