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
| `0x01` … `0xDF` | the cards, in tree order | one per child of the root |
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

## What it does not do

- **No writes to files.** `SP_FILEWRITE`, `SP_FILEDELETE`, `SP_MAKEDIRECTORY`
  and `SP_FILERENAME` are refused with `SP_INVCMD`. The files served are
  generated, and a gateway that lets a client write to its filesystem is a
  hazard rather than a feature.
- **No logging, streaming or drawing services.** They are advertised by neither
  the gateway nor its cards, so a call asking for one is refused rather than
  half-granted.
- **No router node yet.** A RollCall router serves its routing and its names as
  control variables rather than menu lines, so serving one is a different model
  from this one. It is the next unit.

---

## Testing

The provider is driven by a real client over a pipe — a `session.Link` speaking
the wire format, deliberately not the consumer plugin, because a provider and a
consumer that share an assumption agree with each other and with nothing else.
Nothing sleeps: the clock is injected and fake, and a timeout only happens where
a test asks for one.

```
go test ./internal/snell-rollcall/provider/    # 100% statement coverage, a CI floor
```

A loopback check against the shipped consumer:

```
dhs producer rollcall serve --tree bin/trees/our-dm/tree.json --port 2050 &
dhs consumer rollcall info 127.0.0.1:2050
dhs consumer rollcall walk 127.0.0.1:2050 --slot 1
dhs consumer rollcall set  127.0.0.1:2050 --slot 2 --id 1 --value "CAM 4"
```

Per ADR-0025 the loopback is the last tier, not the first: the vendor emulator
and a real device come before it.
