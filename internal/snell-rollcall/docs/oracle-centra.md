# Oracle run: the vendor Centra controller

What a real Snell router controller does on the wire, measured rather than
read. Every claim below was produced by driving
`assets/Emulator/RouterSimulator/Router2.zip` — the vendor's own
`CentraController.exe`, run as a Sirius 800
(`CENTRA_SIMULATED_ROUTER=3`) — with our own session layer.

This matters because the Full Control Command Set document lists the
per-matrix, per-level and per-entity commands "in a similar fashion" without
giving their numbers. Until this run, `codec/router` read those offsets out of
the order the document lists them in and said so in its package comment. They
are now measured.

Date: 2026-09-07. Controller: `Nucleus 2`, RollCall version 3.0 cs12,
IPShare port 2057.

---

## 1. A router is a different unit, not a port

The gateway answers as `0000-08-00`. Its device map lists fifteen nodes, and
they differ by **unit**, not by port:

| Address | Name | Type | Services |
|---|---|---|---|
| `0000-08-00` | `<not set>` | Nucleus 2 | Menus, Control, File, Map, LongStr |
| `0000-11-00` | Matrix 1 | Router Matrix | Menus, Control, File, Ports, LongStr |
| `0000-12-00` | Matrix 2 | Router Matrix | Menus, Control, File, Ports, LongStr |
| `0000-41-00` … `0000-44-00` | input cards | 5915, 5919, 4915 MADI, 4915 AES | … |
| `0000-61-00` … `0000-66-00` | output cards | 5925, 5949, 4929 MADI/AES, 4925 MADI/AES | … |
| `0000-80-00` | TIELINES | TIELINES | Menus, Control, File, LongStr |
| `0000-81-00` | XY Panel | XY Panel | Menus, Control, File, LongStr |

Consequences:

- A client that addresses only ports on the gateway's own unit sees one node
  and misses the other fourteen. Discovery has to walk the device map and
  address each node by its **unit**.
- An absent port is **not refused**: the controller says nothing at all, and
  the client waits out its own timeout. Our provider Nacks instead, which is
  friendlier and equally legal, but a client must not assume a reply.

## 2. The routing interface is on the XY Panel node

Command 100 answers on `0000-81-00` and is refused everywhere else:

| Node | Command 100 |
|---|---|
| XY Panel (`0x81`) | `13` — the interface version |
| Matrix 1 / Matrix 2 (`0x11`, `0x12`) | NACK, logged by the controller as `FCSendRetValue: WARNING - command not valid` |
| TIELINES (`0x80`) | answers 0 for commands 100–103, NACK above |
| Nucleus (`0x08`) | NACK |

This is what the design document means by connecting to the *PanelDevice*
rather than the RouterDevice. The Matrix nodes are the hardware; the panel node
is the routing interface. Probing command 100 on every node in the map is how a
client finds it, and the Matrix nodes' polite refusal is what makes that safe.

**Interface version 13** is one past the last revision our specification copy
documents (12, "updated error codes returned when making a route"). Commands
are only ever added, so everything documented is present; whatever 13 added is
not in the document we hold.

## 3. The root table, measured

| Command | Name | Value |
|---|---|---|
| 100 | `CMD_INTERFACE_VERSION` | 13 |
| 101 | `CMD_ROUTER_NAME` | `"<not set>"` |
| 102 | `CMD_NUM_MATRICES` | 2 |
| 103 | `CMD_MATRIX_BASE` | 121 |
| 104 | `CMD_MATRIX_STEP` | 15 |
| 105 | `CMD_NUM_CATEGORIES` | 0 |
| 106 | `CMD_CATEGORY_BASE` | 151 |
| 107 | `CMD_CATEGORY_STEP` | 6 |
| 113 | `CMD_NUM_SALVOS` | 4 |
| 117 | `CMD_NUM_DEVICES` | 100 |

`CategoryBase = MatrixBase + NumMatrices × MatrixStep` — 151 = 121 + 2 × 15 —
which is the arithmetic the whole command space is built from, confirmed by the
controller's own numbering.

## 4. Every sub-table offset, measured

Matrix 1's table at base 121, step 15. Each command was read individually and
its content identifies which field it is:

| Offset | Command | Name | Value read |
|---:|---:|---|---|
| 0 | 121 | `CMD_MATRIX_NAME` | `"R1 Matrix 1"` |
| 1 | 122 | `CMD_NUM_LEVELS` | 2 |
| 2 | 123 | `CMD_LEVEL_BASE` | 151 |
| 3 | 124 | `CMD_LEVEL_STEP` | 12 |
| 4 | 125 | `CMD_NUM_SRC_ASSOCS` | 10 |
| 5 | 126 | `CMD_SRC_ASSOC_BASE` | 175 |
| 6 | 127 | `CMD_SRC_ASSOC_STEP` | 5 |
| 7 | 128 | `CMD_NUM_DST_ASSOCS` | 10 |
| 8 | 129 | `CMD_DST_ASSOC_BASE` | 225 |
| 9 | 130 | `CMD_DST_ASSOC_STEP` | 5 |
| 10 | 131 | `CMD_ASSOC_NAMES_8_FILENAME` | `RC_Files\AssocNames_1_8.dat` + CRC |
| 11 | 132 | `CMD_ASSOC_NAMES_32_FILENAME` | `RC_Files\AssocNames_1_32.dat` + CRC |
| 12 | 133 | `CMD_ASSOC_NAMES_ALT_FILENAME` | `RC_Files\AssocNames_1_alt.dat` + CRC |
| 13 | 134 | `CMD_CONTROLLER_NUMBER` | 1 |
| 14 | 135 | `CMD_ASSOC_MAPPINGS_FILENAME` | `RC_Files\AssocMap_1.dat` + CRC |

Level 1 of matrix 1, base 151, step 12:

| Offset | Command | Name | Value read |
|---:|---:|---|---|
| 0 | 151 | `CMD_LEVEL_NAME` | `"R1M1 Level 1"` |
| 1 | 152 | `CMD_LEVEL_TYPE` | 0 |
| 2 | 153 | `CMD_NUM_SRCS` | 10 |
| 3 | 154 | `CMD_SRC_BASE` | 275 |
| 4 | 155 | `CMD_SRC_STEP` | 3 |
| 5 | 156 | `CMD_NUM_DSTS` | 10 |
| 6 | 157 | `CMD_DST_BASE` | 305 |
| 7 | 158 | `CMD_DST_STEP` | 8 |
| 8 | 159 | `CMD_SRCDST_NAMES_8_FILENAME` | `RC_Files\SrcDstNames_1_1_8.dat` + CRC |
| 9 | 160 | `CMD_SRCDST_NAMES_32_FILENAME` | `…_1_1_32.dat` + CRC |
| 10 | 161 | `CMD_SRCDST_NAMES_ALT_FILENAME` | `…_1_1_alt.dat` + CRC |
| 11 | 162 | `CMD_SRCDST_MC_DATA_FILENAME` | `RC_Files\SrcDstMCData_1_1.dat` + CRC |

Source 1 of that level, base 275, step 3:

| Offset | Command | Name | Value read |
|---:|---:|---|---|
| 0 | 275 | `CMD_SRC_NAME_8` | `"R1M1L1S1"` |
| 1 | 276 | `CMD_SRC_NAME_32` | `"mtx01src000001"` |
| 2 | 277 | `CMD_SRC_ALT_NAME` | `"alt01l001s00001"` |

Destination 1 of that level, base 305, step 8:

| Offset | Command | Name | Value read |
|---:|---:|---|---|
| 0 | 305 | `CMD_DEST_NAME_8` | `"R1M1L1D1"` |
| 1 | 306 | `CMD_DEST_NAME_32` | `"mtx01dst000001"` |
| 2 | 307 | `CMD_DEST_ALT_NAME` | `"alt01l001s00001"` |
| 3 | 308 | `CMD_DEST_ROUTED_SRC` | DTP `[uint 0x01010001]` |
| 4 | 309 | `CMD_DEST_PROTECT_STATE` | mode `VALUE\|STRING`, 0, `""` |
| 5 | 310 | `CMD_DEST_MC_SRCS_ROUTED` | DTP, empty |
| 6 | 311 | `CMD_DEST_MC_PROTECTS` | DTP, empty |
| 7 | 312 | `CMD_DEST_MC_PROTECTED` | DTP, empty |

Level 2 of matrix 1 continues at 163, which is 151 + 12: the level step the
controller published. Sources for level 2 start at 385, destinations at 415.

**All four table sizes in `codec/router` are confirmed**: matrix 15, level 12,
source 3, destination 8.

## 5. The names-file checksum is confirmed exactly

The controller publishes a filename and a CRC as Data Transfer Params, e.g.
command 131 returns

```
02 06 1b "RC_Files\AssocNames_1_8.dat" 05 88 ac 9e b3 0e
```

— two items: a string, then a uint. Computing `NamesFile.CRC()` over the
matching file from the simulator's own `RC_Files` directory reproduces the
controller's value in every case:

| File | Controller | Computed |
|---|---|---|
| `AssocNames_1_8.dat` | `0xE6679608` | `0xE6679608` |
| `AssocNames_1_32.dat` | `0x673356C4` | `0x673356C4` |
| `AssocMap_1.dat` | `0x5C2378A6` | `0x5C2378A6` |

So the rule the specification describes — each name hashed with IEEE CRC-32
over its padded field followed by its zero-based index as a little-endian
uint32, and those hashes summed with 32-bit wrapping — is what the controller
actually does. A client can trust a cached names file whose checksum matches
and skip the fetch.

The file layouts decode too: names files are fixed-width, sources then
destinations, no header; association mappings are one 32-bit entry per level
per association; the multi-channel file is the variable-length record the
document describes.

## 6. Making a route

Reading `CMD_DEST_ROUTED_SRC` returns DTP with one uint: the source pin, packed
`matrix<<24 | level<<16 | source`. Setting it takes DTP with a uint **array**
of one or two entries — the pin, and optionally a protect id.

Measured sequence, destination 1 of matrix 1 level 1:

```
read           dtp=[uint 0x01010001]              m1/l1/s1
set  s3        reply dtp=[uint 0x01010001, uint 0]  ← the value BEFORE the set, and result 0
               push  RETVALUE cmd=308 dtp=[uint 0x01010003]
read           dtp=[uint 0x01010003]              m1/l1/s3
```

Two things a client must get right:

- **The reply to a set carries the previous value.** The result code is
  appended and is 0 for success, but the pin in that reply is what was routed
  before. The new value arrives on the back channel as a `SP_RETVALUE` push for
  the same command. A client that trusts the reply's pin shows a stale
  crosspoint until something else refreshes it.
- **The tally follows tielines.** Setting source 7 on this configuration reads
  back as `0x02010001` — matrix 2, level 1, source 1 — because the route was
  completed through a tieline and the pin reports the final upstream source,
  exactly as the document says. A client that expects to read back what it
  wrote is wrong about this protocol, not about the router.

## 7. Sessions and services

The gateway advertises `Menus|Control|File|Map|LongStr`. Measured, one call per
fresh connection:

| Requested | Result |
|---|---|
| `Menus` / `Control` / `File` / `Map`, each alone | accepted |
| `Menus\|Control` | accepted, 16-bit |
| `Menus\|Control\|LongStr` | accepted, 32-bit |
| `Menus\|Control\|File\|Map\|LongStr` | accepted, 32-bit |
| anything including `Display` | **refused** |

So a subset is fine — services are all-or-nothing in the sense that the server
grants all of what was asked or none of it, not in the sense that a client must
ask for everything. What a client may not do is ask for a service the peer does
not advertise: this controller has no Display service and refuses the call
outright. The refusal was bracketed by the identical call without `Display`
succeeding immediately before and after it, so it is the service and not the
controller's mood.

That is the whole reason `dhs consumer rollcall info` could not talk to this
controller: the consumer asked for a fixed set including `Display`.

**A NACK to a Call is not necessarily about the mask.** During this run the
controller went through a phase of refusing every call, including ones it had
just accepted, and recovered on its own. Forty sessions opened on one link
without complaint, so it is not a small pool; whatever the cause, a client must
treat a refused call as worth retrying rather than as proof that a service is
missing. Note also that this controller answers a call it cannot satisfy with
`SP_NACK` rather than `SP_BUSY`, which is what the specification provides for
exactly this.

A request belonging to a service the session did not negotiate is answered
`SP_INVSESS`: walking the device map on a session opened for `Menus|Control`
fails that way rather than with a NACK. Our provider Nacks in the same
situation. Both are defensible; the vendor's is more precise.

## 8. What this changed in our code

- `codec/router`'s package comment no longer says the sub-table offsets are
  unvalidated, because they are not.
- **The consumer asked every peer for a fixed service set including Display.**
  This controller has no display service and refuses the whole call, so
  `dhs consumer rollcall info` could not open a session against it at all. The
  mask is now the intersection of what we want with what the peer advertised,
  taken per node: each entry in the device list carries its own services, and
  they differ between the matrices, the panel node and the gateway.
- **The consumer addressed a slot as a port on the gateway's own unit.** On
  this controller the nodes differ by unit, so fourteen of the fifteen were
  unreachable and reading one timed out rather than failing. A slot is now a
  position in the device's own enumeration and its address is whatever the
  device put there — a port on a frame, a unit on a controller — with the
  address reported in the slot's identity so an operator can see which node a
  number reached.
- **The device map has its own session.** A request belonging to a service the
  session did not negotiate is answered `SP_INVSESS`, so enumerating on the
  control session failed. The map service is asked for on its own and without
  the long-string bit, which this controller refuses in that combination; a
  gateway that will not open one is asked on the control session instead,
  because some units answer a list on any session.

After those three, every node of the controller is reachable from the command
line:

```
$ dhs consumer rollcall info 127.0.0.1:2057
slots        15
  slot  0   status=present    online=true
  …
  slot 14   status=present    online=true

$ dhs consumer rollcall walk 127.0.0.1:2057 --slot 14
slot 14 — 3 objects
```

Three objects, because slot 14 is the XY Panel: a router node serves its
routing and its names as control variables, not as menu lines, which is exactly
what the design document says and why a menu walk finds almost nothing there.

## 9. Reproducing it

```
# Unpack the vendor simulator somewhere writable and run it as a Sirius 800.
CENTRA_SIMULATED_ROUTER=3 CentraController.exe        # RollCall IPShare on 2057
dhs consumer rollcall info 127.0.0.1:2057
```

The simulator writes `FCSendRetValue: WARNING - command not valid` to stdout
for every routing command it refuses, which is a useful confirmation that a
NACK came from the Full Control layer rather than from the session layer.
