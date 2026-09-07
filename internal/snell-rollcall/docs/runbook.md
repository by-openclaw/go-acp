# RollCall — operational runbook

What to run, in what order, and what the answers mean. For the wire format see
[../CLAUDE.md](../CLAUDE.md); for measured device behaviour see
[oracle-centra.md](oracle-centra.md).

---

## 1. Bring-up, in order

Each step tells you something the next one needs. Stopping at the first failure
is the point.

```
dhs consumer rollcall info <host>
```

| What you see | What it means |
|---|---|
| `slots N` and a status line per slot | The link, the handshake, the map service and the session service all work. Go on. |
| `dial …: connection refused` | Nothing is listening. A frame listens on 2050; a Centra's IPShare port is configurable and is often **2057**. |
| `handshake …: reply timeout` | Something accepted TCP and did not answer a device enquiry. Wrong port, or a device that is not RollCall. |
| `call …: NACK` | The unit refused the session. Almost always a service it does not have — see §4. |
| `port list …: INVSESS` | The map service was asked for on a session that did not negotiate it. A bug on our side; report it. |

Then walk one node:

```
dhs consumer rollcall walk <host> --slot 1
```

A frame's cards answer with tens to hundreds of objects. A router node answers
with two or three, and that is correct: routing is not in the menu.

## 2. Slots

A slot number is a position in the device's own enumeration, and the address
behind it comes from the device. Two shapes exist and the difference matters:

- **A frame** enumerates the ports of one unit. Slot 0 is the first card.
- **A controller** enumerates units: the matrices, the tielines engine, the
  panel driver that serves routing, and every card in the frames it fronts.
  Slot 9 might be `0000-81-00`, the XY Panel.

`info` prints each slot's identity, and the address is in the JSON:

```
dhs consumer rollcall info <host> --output json | jq '.slot_status[] | {slot, identity}'
```

## 3. Verbs

### Consumer

| Verb | What it does | Notes |
|---|---|---|
| `info <host>` | identity, slot count, per-slot status | costs one enumeration |
| `walk <host> --slot N` | every object on a node | caches the menu for label lookups |
| `get <host> --slot N --label X` | read one object | `--id` takes the command number instead |
| `set <host> --slot N --label X --value V` | write one object | answers with the **stored** value |
| `reset <host> --slot N --label X` | ask the device for its own default | the numeric field is ignored |
| `watch <host> --slot N` | subscribe to value changes | opens the back channel |
| `router <host>` | find and print a routing interface | probes every node unless `--slot` |
| `route <host> --matrix M --level L --dest D [--source S]` | read or make a crosspoint | waits for the tally |
| `tally <host> --matrix M --level L [--names]` | print a level, then follow it | Ctrl+C to stop |

### Producer

```
dhs producer rollcall serve --tree tree.json --port 2050
```

Serves a canonical tree as a gateway: the root is the frame, each child is a
card slot. See [provider.md](provider.md).

## 4. When a session is refused

`SP_NACK` to a call is the commonest failure and it has three usual causes.

**A service the peer does not have.** Services are all-or-nothing: naming one
the unit lacks refuses the whole call. Our consumer intersects what it wants
with what the peer advertised, so this should not happen — but if a unit
advertises a service and then refuses it, that is a compliance event
(`rollcall_long_strings_refused` is the one we have seen) and the fallback is
automatic.

**The unit is out of sessions.** Servers do not time idle sessions out. The
vendor's own comments say units "don't time them out very well (at all), and
then run out of available sessions". A unit that has been talked to by a
crashed client stays that way until it reboots. The specification has
`SP_BUSY` for this; the Centra sends `SP_NACK` instead, so a refusal is worth
retrying before it is believed.

**A user level the unit does not accept.** Four exist: engineer, operator,
supervisor, factory. We ask for supervisor.

## 5. Reading a capture

The dissector is at
[`wireshark/dhs_snell_rollcall.lua`](../wireshark/dhs_snell_rollcall.lua).
Install it per [docs/wireshark.md](../../../docs/wireshark.md), then:

| Question | Filter |
|---|---|
| All RollCall traffic | `dhs_snell_rollcall` |
| Who opened what | `dhs_snell_rollcall.type == 2` |
| One node only | `dhs_snell_rollcall.dst.unit == 0x81` |
| Crosspoints | `dhs_snell_rollcall.source_pin` |
| Tally, not replies | `dhs_snell_rollcall.flags.back_channel == 1` |
| Refusals | `dhs_snell_rollcall.type in {0 14 15 23}` |

A refusal type says which kind: `NACK` is "I understood and will not",
`INVCMD` is "I do not know this message", `INVSESS` is "not on this session",
`BUSY` is "try again".

## 6. Routers

Find the interface first. It is on one node and nothing in a device list says
which:

```
dhs consumer rollcall router <host>
```

If that reports nothing, the device has no routing interface — a frame of cards
does not. If it reports one, everything else follows from the matrices and
levels it printed.

**Three behaviours that surprise people**, all measured:

1. **The reply to a route carries the previous crosspoint.** The result code
   says whether it worked; the new value arrives afterwards on the back
   channel. The `route` verb waits for it, so what it prints is what happened.

2. **The tally follows tielines.** Ask for source 7 and read back matrix 2
   source 1: the number reported is the *final upstream* source. The router is
   working. Reading back what you wrote is not a valid expectation.

3. **Subscribing does not fill in the picture.** Enabling the back channel does
   not replay current state on a real controller, whatever the specification
   says. Read the level once, then follow it. `tally` does both.

Names come in bulk from files, verified by a checksum computed from the names
themselves. Where a controller names a file its own file service will not serve
— which the Centra does, because the path is in the controller's filesystem and
each node's file service is rooted at its own directory — the names are read one
command at a time and `rollcall_names_file_unreadable` is recorded. On a large
level that is slow, and the event is there to explain why.

## 7. Compliance events

The connector absorbs a deviation and keeps working, then counts it. One entry
per kind with the most recent detail, never one per occurrence.

| Event | What it means |
|---|---|
| `rollcall_long_strings_refused` | A unit advertised the 32-bit generation and refused a call asking for it. We fell back. |
| `rollcall_menu_span_overruns` | A container claimed a subtree longer than the menu holding it. Clamped. |
| `rollcall_menu_count_mismatch` | A device announced one number of menu lines and sent another. |
| `rollcall_duplicate_command` | Two menu lines claimed the same command number. |
| `rollcall_access_gated` | A line was replaced by the placeholder a server substitutes above a user level. Not a fault. |
| `rollcall_value_mode_empty` | A value reply set no flag, so it carried nothing. |
| `rollcall_unsolicited_command` | A push named a command the walked menu does not list. Delivered anyway. |
| `rollcall_display_line_out_of_range` | A status line outside the four and the two priorities. |
| `rollcall_unlisted_node` | A slot was addressed that the enumeration did not mention. Addressed anyway. |
| `rollcall_names_checksum_mismatch` | A names file did not hash to its published checksum. Names kept. |
| `rollcall_names_file_unreadable` | A router named a names file it will not serve. Fell back to one command per name. |

The provider has its own set, in [provider.md](provider.md).

## 8. Integration runs

```
# loopback only
ansible-playbook -i inventory/hosts.ini playbooks/snell-rollcall-integration.yml

# with the vendor Centra emulator
ROLLCALL_SIM_HOST=127.0.0.1 ROLLCALL_SIM_PORT=2057 ansible-playbook ...

# with a live device, read-only
ROLLCALL_TEST_HOST=10.6.250.105 ansible-playbook ...
```

The emulator lives in [`assets/Emulator/RouterSimulator`](../assets/Emulator/RouterSimulator).
Unpack it somewhere writable — one copy per configuration — and run
`CentraController.exe`.

`CENTRA_SIMULATED_ROUTER` selects the chassis. Ids 0 to 3 all answer as a
`Nucleus 2` with fifteen nodes and are indistinguishable on the wire; only 4
differs, giving a `Vega Controller` with six nodes and no cards, which also
makes it ignore `CENTRA_SIMULATED_CARDS`. The zip ships persistence, so a
folder that has already run keeps the model it wrote: change the id in a fresh
copy, never in place. [oracle-centra.md](oracle-centra.md) §11 has the measured
table.

Several instances run side by side happily — that is how the table was
measured — but four ports in `config.xml` have to differ, not one:
`RollCall/SharePort`, `RollCall/BridgePort`, `IP/Adapter/Port` and
`Web/XPCPort`. Missing one leaves the second instance up and silently
crippled.

The IPShare wedges under connection churn: our CLI opens a fresh connection per
verb, and roughly the third in quick succession is accepted at TCP and never
answered. Leave a few seconds between verbs, or restart the controller.

It writes `FCSendRetValue: WARNING - command not valid` to stdout for every
routing command it refuses, which is a quick way to tell a Full Control refusal
from a session-layer one.

## 9. Known device quirks

| Device | What it does | What we do |
|---|---|---|
| Centra | No display service; refuses any call naming it | Ask only for what a peer advertises |
| Centra | Nodes differ by unit, not port | Address by the enumeration's own address |
| Centra | Cards answer command 100 with their model number as a string | Require a numeric interface version |
| Centra | Directory entries are variable-length, not the declared 13-byte field | Read the name as what follows the header |
| Centra | Names files named in its own filesystem, unreachable over RollCall | Fall back to one command per name |
| Centra | Enabling the back channel replays nothing | Read the state, then follow it |
| Any unit | Sessions are not timed out | Always send `SP_TERM`; a leaked session is held until reboot |
