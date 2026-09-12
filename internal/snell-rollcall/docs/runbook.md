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
| One matrix, one level | `dhs_snell_rollcall.matrix == 1 && dhs_snell_rollcall.level == 2` |
| One destination, everywhere | `dhs_snell_rollcall.destination == 40` |
| Routes that were refused | `dhs_snell_rollcall.route_result > 0` |
| Which nodes are router nodes | `dhs_snell_rollcall.router_node` |
| Tally, not replies | `dhs_snell_rollcall.flags.back_channel == 1` |
| Refusals | `dhs_snell_rollcall.type in {0,14,15,23}` |

A refusal type says which kind: `NACK` is "I understood and will not",
`INVCMD` is "I do not know this message", `INVSESS` is "not on this session",
`BUSY` is "try again".

**Start the capture before the client connects.** A command number here means
nothing on its own: 100 is the interface version on the node serving the Full
Control tables and the selected destination on a level, and everything above
119 is addressed by bases and steps the controller publishes once, as the
session opens. The dissector reads both out of the capture — each node's type
from the RETID it answers with, each table from the reply carrying it — so a
capture that joins a session already in progress has missed them, and those
commands then read as `unresolved` rather than as a plant. That is the
dissector being honest; the fix is to capture from the start.

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

A run with nothing configured still exercises the router verbs. Two producers
are served from committed trees — a frame of cards, and a plant of two matrices
with four levels each and tielines between them — so `router`, `route`, `tally`
and `salvo` are covered on every run rather than only where an emulator happens
to be configured. Both producers are ephemeral and torn down in `always`, so
the play leaves nothing behind and run-twice is the same result.

### Convergence

Three things converge on this connector, and they are not the same thing:

| What | Reached by | Verb |
|---|---|---|
| a value on a card | slot + label | `consumer rollcall ensure` |
| a crosspoint on a router | matrix + level + destination | `consumer rollcall route --source` |
| the producer itself | the pidfile it wrote | `producer rollcall ensure --state` |

A crosspoint gets its own verb because it has no slot and no label and never
did. All three answer in the same shape — `{changed | would_change, previous,
current, target, diff[]}`, with `diff` always present even when empty — so one
play reads any of them.

```
ansible-playbook -i inventory/hosts.ini playbooks/snell-rollcall-ensure.yml
```

runs the ADR-0007 three-step contract over all three: converge → `changed`,
converge again → unchanged, `--check` → `would_change=false` having sent
nothing.

For a crosspoint, `changed` is a comparison of the destination read *before*
the take with the destination read *after* it — a measurement, not a
prediction. It has to be: a route across a tieline reads back as the far end
of the cable rather than as the source that was asked for, so any rule
comparing the request with the reading would call a converged route
unconverged and take it again for ever. The same caveat applies to `--check`
across matrices, where the question is about the reading rather than about the
plant.

Converging a producer to `absent` by its pidfile is also how these plays tear
themselves down. It beats hunting for a process with a pattern, and converging
one that has already gone is a clean no-op — so the teardown is safe after a
failure anywhere, including before the producer existed.

### The dissector

```
ansible-playbook -i inventory/hosts.ini playbooks/snell-rollcall-dissector.yml
```

Captures a real conversation on the loopback interface and decodes it with
`dhs_snell_rollcall.lua`. The assertion that matters is that **command 100
resolves two different ways in one capture** — the interface version on the
panel node, the selected destination on a level — because nothing in the bytes
distinguishes them and only the node's type does. It also asserts that all four
router node types were identified, that commands above 119 resolved to a matrix,
level and destination, that a crosspoint set decoded as a source pin, that no
frame raised an expert warning, and that a second pass draws every frame the
same as the first.

Wireshark on RHEL and Rocky is built without Lua. Capture there with `dumpcap`
and decode on a host that has it, the way
[`acp1-capture.yml`](../../../ansible/playbooks/acp1-capture.yml) relays an
ACP1 pcap.

The emulator lives in [`assets/Emulator/RouterSimulator`](../assets/Emulator/RouterSimulator).
Unpack it somewhere writable and run `CentraController.exe`; set
`CENTRA_SIMULATED_ROUTER=3` to make it a Sirius 800. It writes
`FCSendRetValue: WARNING - command not valid` to stdout for every routing
command it refuses, which is a quick way to tell a Full Control refusal from a
session-layer one.

## 9. Testing through a proxy

A RollCall client reaches one chassis per connection. The **RollCall IP Proxy**
is what a plant uses to get past that: it holds a connection to each chassis
and publishes them all through one port. The manual is
[`assets/Protocol/Docs/RollCall_IP_Proxy_Operation[1].pdf`](../assets/Protocol/Docs);
§2.1.1 is blunt about why it exists — *"needed in a system to enable connection
to more than one Ethernet enabled IQ chassis."*

Three paths reach the same plant, and all three are worth testing:

| Path | What only it proves |
|---|---|
| direct | our session layer against the vendor's own code |
| through the vendor proxy | that we behave like a RollCall Control Panel |
| through our own bridge | that our aggregation is indistinguishable from theirs |

### 9.1 Emulators, one per chassis

Unpack one copy of the simulator per instance and give each its own four ports
— see [oracle-centra.md](oracle-centra.md) §12, which lists all four and why
`SharePort` alone is not enough.

### 9.2 Point the proxy at them

The proxy installs as the Windows service `RollIPProxy` and starts
automatically. Open its GUI from the system-tray icon, or
`Start > All Programs > SAM > RollCall > RollCall Proxy Service`.

For each emulator, in **Map Connections to Ethernet Chassis or IP Share**, click
**Add** and give it three things:

| Field | Value |
|---|---|
| IP Address | the host running the emulator, or a resolvable name |
| Port | that emulator's `RollCall/SharePort` |
| Substitution address | a four-digit RollCall net — see below |

**The substitution address is the `rNet` field**, and the manual states the rule
our codec already enforces in `Address.ValidRoute`: four digits, non-zero from
the leftmost digit. `1000`, `1200`, `1230` and `1234` are valid; `0100` is not.
Its **first digit is the bridge unit** the proxy publishes that chassis as —
`1000` becomes node `0000-01-00`, `2000` becomes `0000-02-00`.

That is also the organising handle. The manual (§3.4) says to rename each node
"to indicate their location or network type", so the substitution address is
where a site, a rack row or a function belongs.

Watch the **Status** column: `Connected` means it found the chassis, `Calling`
means it did not.

### 9.3 Ports the proxy listens on

| Port | For |
|---|---|
| 2050 | RollCall control clients — the Control Panel, and us |
| 2053 | RollMap / RollView. The default 2052 collides with LogServer on the same host, and the manual recommends moving this one |

### 9.4 What a proxied read looks like today

```
dhs consumer rollcall info <proxy-host>:2050
```

The proxy answers as `RollProxy Service` on unit `0xFF`, advertises **Map**
alone, and speaks the **16-bit** generation. Its map lists one `Proxy Virtual
Node` per configured chassis, each carrying the **Net** service:

```
slot 0  0000-02-00  Proxy Virtual Node  "Example IQ frame"
slot 1  0000-01-00  Proxy Virtual Node  "Local RollNet"
```

Those are bridges, not the equipment behind them. Reading *through* a bridge
needs the Net service — spec §7.7, and note that `SP_GETLOCDEVMAP` means "map"
or "net" depending on the session's services, so it needs a Net-without-Map
session exactly as the map needs Map-without-Net. That is not implemented yet,
which is why a proxied read reports two nodes where a direct read reports
fifteen.

**Two limits worth knowing before designing a topology.** `rNet` is four
nibbles of 1–15, so one proxy fronts at most **15 chassis** and routes cross at
most **four** bridges. And the proxy wedges under connection churn like every
other RollCall gateway — leave seconds between verbs.

### 9.5 Running the contract over every path

Every path is asserted the same way, by the same task file, and each path's
node table is kept so two can be diffed. From the control node:

```
cd ansible
DHS_BIN=/root/acp/bin/dhs ROLLCALL_SIM_HOST=<emulator>   ROLLCALL_SIM_PORT=2050 ROLLCALL_PROXY_HOST=<proxy>    ROLLCALL_PROXY_PORT=2050 ROLLCALL_SETTLE_SECONDS=12   ansible-playbook -i inventory/hosts.ini playbooks/snell-rollcall-integration.yml
```

`ROLLCALL_BRIDGE_HOST` adds our own bridge when there is one, and
`ROLLCALL_TEST_HOST` adds real hardware, read-only — the IQ 3U frame at
`10.6.255.113` is the one on the fabric today:

```
ROLLCALL_TEST_HOST=10.6.255.113 ROLLCALL_TEST_PORT=2050 ROLLCALL_TEST_SLOT=1
```

It matters more than its node count suggests. It advertises no long strings, so
it is the **16-bit generation** — the one a proxy also speaks and the one the
emulator never exercises, because the emulator is 32-bit throughout. A path with no host is
skipped rather than faked. `DHS_BIN` is for a control node with Ansible and no
Go toolchain, which is what the designated one is.

The run ends with the comparison, which is the point of the shape:

```
direct        10.6.239.107:2050  15 node(s) [ 0000-08-00 ... 0000-81-00 ]
vendor-proxy  10.6.250.105:2050   2 node(s) [ 0000-01-00 0000-02-00 ]
loopback      127.0.0.1:22050     2 node(s) [ 0000-01-01 0000-01-02 ]
```

It reports rather than asserts. A proxy publishes one node per chassis, so
those counts differing is correct on both paths; asserting they matched would
be asserting a bug. The report is there for the difference nobody predicted.

## 10. Known device quirks

| Device | What it does | What we do |
|---|---|---|
| Centra | No display service; refuses any call naming it | Ask only for what a peer advertises |
| Centra | Nodes differ by unit, not port | Address by the enumeration's own address |
| Centra | Cards answer command 100 with their model number as a string | Require a numeric interface version |
| Centra | Directory entries are variable-length, not the declared 13-byte field | Read the name as what follows the header |
| Centra | Names files named in its own filesystem, unreachable over RollCall | Fall back to one command per name |
| Centra | Enabling the back channel replays nothing | Read the state, then follow it |
| IQ 3U frame | Keeps its cards behind the **port** service and ignores a port list asked on a map session | Ask for `SvcPorts` when a peer advertises it; the Centra, which does not, is still asked on the map session |
| IQ 3U frame | Lists a port (`0000-0C-8E`) that then refuses a session | Carried as that slot's error rather than failing the enumeration |
| IQ audio cards | Advertise `Menus|Control|File` and no `SV_LOC1` | No thumbnails from an audio card; the service is a video one |
| Any unit | Sessions are not timed out | Always send `SP_TERM`; a leaked session is held until reboot |
