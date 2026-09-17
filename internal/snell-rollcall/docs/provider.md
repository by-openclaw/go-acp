# RollCall provider

`dhs producer rollcall serve --tree <file>` serves a canonical tree as a Snell
RollCall gateway: a unit whose ports are the cards in its frame, answering both
wire generations from one model.

```
dhs producer rollcall serve --tree tree.json --port 2050
```

---

## What a client sees

A RollCall gateway is a unit with ports. The tree's root becomes the frame, and
each child of the root becomes a port — a card slot — with everything below it
as that card's menu.

| Port | What answers | Notes |
|---|---|---|
| `0x00` | the gateway itself | identity, status, the local device map |
| `0x01` … `0xDF` | the cards | on the slots a `--manifest` names, with the identity each DM was filed under; from a `--tree`, one per child of the root, in order |
| `0xE0` … | the clients | ports the gateway stamps into a client's address |

Port zero is not a card. A client's first message is a device enquiry addressed
to unit zero, because it has nothing else to address, and what comes back names
both ends: the gateway's own address, and the address the gateway is assigning
the client. A gateway that answers as unit zero tells the client its address was
never assigned, so this one answers as unit 1 by default (`SetUnit` changes it).

Cards start at port one for the same reason a frame's slots do: port zero
already means "the unit", and a card there would answer "what are you?" twice
with two different identities.

---

## Both generations from one tree

Which generation a client gets is decided by the service mask in its `SP_CALL`,
not by what the provider is. The provider advertises `SV_LONGSTR` alongside the
rest; a client that asks for it gets 32-bit command numbers and long strings,
and one that does not gets the original 16-bit forms of the same objects.

Services are all-or-nothing. A call asking for something this provider does not
serve is refused outright rather than granted in part, because a partial grant
leaves a client believing it has something it has not.

| Message | 16-bit | 32-bit |
|---|---|---|
| menu size | `SP_GETFUNC` → block header | `SP_GETMENUCOUNT` → `MENUSIZE_STR` |
| menu line | `SP_GETNEXTPKT` → `FUNC_STR` | `SP_GETMENUITEM` → `MENUITEM_STR` |
| read | `SP_GETFSTAT` → `FUNCSTATUS_STR` | `SP_GETVALUE` → `VALUE_STR` |
| write | `SP_SETPARAM` → `FUNCSTATUS_STR` | `SP_SETVALUE` → `VALUE_STR` |
| push | `SP_SETPARAM` on the back channel | `SP_RETVALUE` on the back channel |

The 16-bit menu is the same tree with the same command numbers, not a different
one. Where a number will not fit — a command or a span past 65535 — the line is
served as a disabled display line with command zero rather than truncated,
because a truncated number addresses a different command; and a line whose
*index* will not fit is not offered at all, because the index is how a 16-bit
client asks for a line.

---

## The menu

The tree is flattened depth first into a flat array with nested spans, which is
the shape a client walks and the shape the vendor's Control Panel expects.

A container's `Step` is the size of its **whole subtree**, not the count of its
immediate children. Getting that wrong nests the tree wrongly rather than
failing, which is why it has its own test.

| Canonical | Style | Notes |
|---|---|---|
| node, matrix, function | `ST_LIST` | container; `Step` spans the subtree |
| `boolean` | `ST_CHECKBOX` | the "on" value lives in `MinRange` |
| `string` | `ST_EDITSTRING` | range is a length, not a value |
| `enum` | `ST_LIST` | a chooser |
| `integer`, `real` | `ST_NUMBER` | a real is scaled by its factor |
| anything else | `ST_DISPLAY` | shown, not edited |

A parameter nobody may write is served with `ST_DISABLED` set: the line is still
there and a client shows it greyed, which is the protocol's way of saying "you
may look at this". Labels are cut to the long-string ceiling when the model is
built, so both generations describe the same tree.

A real is carried as an integer scaled by its factor, because that is the only
numeric form the wire has, and the factor becomes the divisor a client divides
by to display. `gain = -6.0 dB` with `factor = 10` is `-60` on the wire.

---

## Values

A write is answered with the value that was **stored**, not with an
acknowledgement. A numeric write outside the line's range is clamped, exactly as
a device does, so a client learns what it got without reading back. A checkbox
is not clamped: its `MinRange` is the "on" value rather than a bound, and
clamping against it would make the box impossible to clear.

`SP_SETVALUE` with `MO_PRESET` asks for the device's own default; the numeric
field is ignored.

A write that names a unit type in `MO_MATCHID` applies only if the type is ours
or zero. That is how a controller writes one command to a whole frame without
knowing which slots hold which cards: the cards it does not mean answer with
what they already had.

A command that is not in the menu is refused with `SP_NACK` and recorded as a
compliance event: a client asking for one has either cached a menu that has
since changed or invented a number, and both are worth counting.

---

## Pushes

Two messages open a value stream and the second is easy to miss:

1. `SP_BKCHNREADY` with `1` opens the back channel **and** flushes every value
   the slot holds, which is what makes a client's first screen correct without
   it having to read every object.
2. `SP_BKCHNREADY` with `2` opens it for future changes only.
3. `SP_BKCHNREADY` with `0` closes it.

Every push is a request, not a notification: the client must acknowledge it
before the next may be sent. So changes are queued per subscriber, one goroutine
sends them in turn, and a subscriber more than 64 changes behind loses pushes
rather than holding up whichever goroutine made the change — on a frame with a
hundred cards, one slow panel must not stop the device. A dropped push is a
compliance event; the value it carried is still readable.

The session that made a change is not told about it: it already has the answer
in its reply, and pushing it back makes a client that echoes what it hears loop.

---

## Files

The file service is not an extra. The vendor's Control Panel reads
`TEMPLATE.ZIP` over it before it draws anything, so a provider without one is a
device the Control Panel will not render at all.

`TEMPLATE.ZIP` is generated from the served tree and is byte-identical for the
same tree, so a client caching it by checksum does not re-fetch it after a
restart. `AddFile` adds anything else — a router's names file, for instance.

What is served is an in-memory set of files rather than a directory on disk: a
gateway exposing its own filesystem to every client that connects is a hazard
nobody asked for.

The two numeric fields of `FILE_STR` change meaning between a request and its
reply, which is the mistake this service punishes silently:

| Message | `rOffset` | `rExtra` |
|---|---|---|
| `SP_FILEOPEN` | unused | the open flags |
| `SP_RETFILEOPEN` | our maximum block size | the error |
| `SP_FILEREAD` | where to read from | how many bytes |
| `SP_RETFILEREAD` | how many were read | the error |

A file that is not there is answered with `ENOENT` in the reply rather than
refused: "the file is absent" and "the service is broken" are different answers.
A path is matched by its last component, case-insensitively, so a client that
prefixes a device root finds the same file.

---

## Displays

A unit's status lines are not menu objects: they are numbered, and two of the
numbers are priorities rather than positions.

| Line | Meaning |
|---|---|
| `0` … `3` | the front panel's four lines |
| `-1` | the error line |
| `-2` | the warning line |

`SetDisplay` sets one and pushes it to every subscriber on that slot.

---

## Compliance events

The provider absorbs what a client gets wrong and keeps serving it, and counts
it. One entry per kind with a count, never one per occurrence: a client that
misbehaves on every one of sixty-five thousand destinations must not fill memory
with the evidence.

| Event | What it means |
|---|---|
| `rollcall_unserved_service` | a call asked for a service this device does not supply |
| `rollcall_unknown_command` | a read or write named a command that is not in the menu |
| `rollcall_unknown_file_handle` | a file operation named a handle this link never issued |
| `rollcall_push_dropped` | a subscriber fell far enough behind to lose a change |
| `rollcall_invalid_user_level` | a call named a level outside the four the spec defines |

---

## As a RollCall IP Proxy

The provider can present as the vendor **RollProxy** instead of as a frame:
a `RollProxy Service` on unit `FF` offering the Map service, whose map lists
a `Proxy Virtual Node` per hop of a network address, and behind the last hop a
frame's gateway and its cards at that route (`provider/proxy.go`). The chain is
derived from the subnet: `2100` is two hops, so two virtual nodes, and the
frame's gateway sits at `2100-<unit>-00`. What a client sees is the vendor
box's, byte for byte, measured on 2026-09-16
([captures/vendor-proxy-walk-2026-09-16.txt](captures/vendor-proxy-walk-2026-09-16.txt)):
the proxy answers the handshake from `0000-FF-01` as a `RollProxy Service`
4.6 cs0 and assigns the client no address, so every client of a proxy stays
`0000-00-00`; a virtual node is listed at its net-zero address with session
index 0, status present only, and a client composes the route as it descends
(`codec.Address.Compose`, shared with the consumer); the last hop lists the
frame's **whole segment**, already routed — the vendor's net list is "a
delegate list which reflects the SV_MAP list served by the remote device"
(`MapServer.c`), so an IQ frame is one entry and a Centra is its controller,
matrices, tielines and panels, each at `3000-<unit>-00` — read from the
frame's map once at the probe (network nodes only, port zero); and the proxy
sends no `SP_IAM` at all.

Two things can sit behind a route, and a proxy fronts several frames at once,
one subnet each — the vendor box exists "to enable connection to more than one
Ethernet enabled IQ chassis", and this is the same shape:

| Flags | Behind the route | What it proves |
|---|---|---|
| `--proxy-subnet 1100 --proxy-frame 0C` with `--tree` / `--manifest` | the served tree, as an emulated frame | a client walks our chain the way it walks the vendor's |
| `--proxy-subnet 2100 --proxy-upstream 10.6.255.113:2050` | a **real frame** on the network — our own IPShare | a client reaches real hardware through our proxy |
| `--proxy-upstream 2100=10.6.255.113,3000=10.6.250.105:2057,4000=10.6.250.104:2052` | several real frames, one subnet each; add `--proxy-subnet`/`--proxy-frame` for the served tree beside them | one connection list for a whole plant, as the vendor box gives |

```
dhs producer rollcall serve --proxy-subnet 2100 --proxy-upstream 10.6.255.113 --port 2050
dhs producer rollcall serve --proxy-upstream 2100=10.6.255.113,3000=10.6.250.105:2057,4000=10.6.250.104:2052 --port 2050
```

Fronting real frames, no tree is served: every request a client addresses to
a route is carried to that frame and its answers carried back
(`provider/proxy_relay.go`). Each frame is probed at start to learn its unit
and identity. One that does not answer is kept: its chain is listed, its far
side is empty, and asking for the far side calls it again — the vendor box's
"Calling" column, which turns to "Connected" when the chassis appears. Two
frames may not share the first digit of their subnet, since the map lists one
virtual node per frame.

Everything on a real frame's subnet is that frame's, whatever unit it is: a
Centra puts every node on a unit of its own, and `3000-11-00` reaches its
first matrix the way `3000-08-00` reaches its gateway. The served tree is one
unit, so only its unit resolves on its subnet.

**One connection to each frame per client.** The vendor box multiplexes every
client over one connection and so has to renumber sessions; this proxy opens a
connection of the client's own to each frame, so the frame's session indices
and the client's are exactly what each chose, and the frame sees each client
as a client. It costs one of the frame's connection slots per client, which is
what a client costs it directly. The connection is dialed on the first routed
request and redialed if the frame drops it; while the frame cannot be reached
a routed request is refused with `SP_NACK "frame unreachable"` rather than
left to time out, and the proxy unit and its nodes keep answering.

**Every client of the proxy is a connection to every frame it touches.** That
is the price of the per-client relay, and on the IQ frame it is not free:
the frame wedges under connection churn — TCP accepted, `GETDEVINFO` never
answered, cleared only by restarting its gateway board (`docs/testbed.md`).
Measured 2026-09-17: a dozen `dhs consumer rollcall` verbs in a row, each a
fresh client of the proxy and so a fresh connection to the rack, wedged it.
A Control Panel holds one connection for hours and is fine. So: drive a
frame through the proxy the way a panel does, one long client, and leave
seconds between CLI verbs against it, as the integration play already does.
The vendor box avoids this by holding one connection per chassis and
renumbering every client's sessions onto it; doing the same here is the
follow-up if churn from many short clients turns out to matter in a plant.

**Where the time goes.** Measured 2026-09-16 with the vendor Control Panel
opening a Nodal card through this proxy on dhs-tools (5 548 relayed requests):

| Leg | p50 | p95 |
|---|---|---|
| relay, panel to frame (our overhead) | 0.18 ms | 0.22 ms |
| relay, frame to panel (our overhead) | 0.18 ms | 0.24 ms |
| the frame answering | 4.9 ms | 19.3 ms |
| the panel before its next request | 1.3 ms | 4.0 ms |

The proxy adds a third of a millisecond per round trip. What an operator feels
is the protocol: one active message per session, so a menu of 4 200 lines is
4 200 serial round trips of the frame's five milliseconds. The vendor box has
the same shape. Answering menu lines and template reads from a cache at the
proxy is the enhancement that would change it, and it is not implemented.

**What crosses each leg is what the vendor library does** — read from
`IPShare.c` and `IPShClient.c` under `assets/Protocol/Source`, not guessed:

- Toward the frame, the destination route is consumed hop by hop
  (`Address.Forward`, spec 5.3) and the source device zeroed the way an
  IPShare client zeroes its own; the frame spoofs the source to its own unit
  and the connection's port regardless. Session indices are untouched.
- Toward the client, the source has the route composed hop by hop
  (`Address.ForwardSource`) so `0000-0C-01` arrives as `2100-0C-01`; the
  destination, which a frame zeroes on everything it sends an IPShare client,
  is written back as the address this proxy assigned the client at its
  handshake. A broadcast keeps the broadcast address.
- Nothing inside a payload is touched (spec 11.3.4).

A route is one frame deep: an address beyond a frame's own subnet — a device
behind a bridge the frame itself holds — is not resolved and its session is
refused. The IQ frame holds none.

---

## What it does not do

- **No writes to files.** `SP_FILEWRITE`, `SP_FILEDELETE`, `SP_MAKEDIRECTORY`
  and `SP_FILERENAME` are refused with `SP_INVCMD`. The files served are
  generated, and a gateway that lets a client write to its filesystem is a
  hazard rather than a feature.
- **No logging, streaming or drawing services.** They are advertised by neither
  the gateway nor its cards, so a call asking for one is refused rather than
  half-granted.
- **One unit only.** A vendor Centra serves each matrix as a unit of its own,
  with that matrix's levels as its ports. This provider serves every
  matrix, level, tieline and panel node as a port of one gateway, so a client
  walking the plant finds the same nodes at different addresses.

---

## Testing

The provider is driven by a real client over a pipe — a `session.Link` speaking
the wire format, deliberately not the consumer plugin, because a provider and a
consumer that share an assumption agree with each other and with nothing else.
Nothing sleeps: the clock is injected and fake, and a timeout only happens where
a test asks for one.

```
go test ./internal/snell-rollcall/provider/    # 100% statement coverage, a CI floor
go test -tags integration ./internal/snell-rollcall/integration/ -run 'Proxy|IPShare'
ROLLCALL_TEST_HOST=10.6.255.113 go test -tags integration ./internal/snell-rollcall/integration/ -run IPShare
```

The first integration run walks our proxy with our consumer, fronting the
committed IQ frame served in-process; the second fronts the real frame,
read-only, which is what proves the relay against a frame that rewrites
addresses the way the vendor library does rather than the way this provider does.

A loopback check against the shipped consumer:

```
dhs producer rollcall serve --tree bin/trees/our-dm/tree.json --port 2050 &
dhs consumer rollcall info 127.0.0.1:2050
dhs consumer rollcall walk 127.0.0.1:2050 --slot 1
dhs consumer rollcall set  127.0.0.1:2050 --slot 2 --id 1 --value "CAM 4"
```

Per ADR-0025 the loopback is the last tier, not the first: the vendor emulator
and a real device come before it.
