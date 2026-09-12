# RollCall consumer

The outbound connector: `dhs consumer rollcall <verb> <host>`. It implements
the neutral `consumer.Protocol` on top of the session layer, and adds the
router command space, which nothing neutral has a shape for.

---

## Connecting

One TCP connection to an IPShare gateway carries everything. The first message
on it is a device enquiry addressed to the broadcast address, because a client
knows neither its own address nor the gateway's until the gateway answers:

```
→ GETDEVINFO  0000-00-00:FF → 0000-00-00:FF
← RETDEVINFO  0000-08-00:FF → 0000-01-E0:FF
```

The reply's **destination** is the address being assigned to us. The reply's
**source** is the gateway. Both come from the frame header rather than from the
payload, because the address inside a `DEVICEINFO_STR` is never rewritten as a
message crosses a bridge and so means nothing about the route.

## Sessions

A session is a set of services at a user level, and the services are granted
all-or-nothing: a call naming one the peer does not have is refused entirely.
So the mask asked for is the intersection of what we want with what the peer
advertised — measured against a controller that has no display service and
refuses every call naming one.

Sessions are per node and kept, because opening one costs a round trip and
closing one is what a unit runs out of. There are three kinds:

| Session | Services | Why separate |
|---|---|---|
| control | menus, control, display where offered, long strings where offered | the one everything ordinary uses |
| file | file | a unit without a file service must still be controllable |
| map | map | a request outside a session's services is answered `INVSESS` |

Two indices matter and confusing them is the defect the session layer exists to
prevent. Ours goes in the source of everything we send; the peer's goes in the
destination. A push arrives addressed to ours.

## Slots

A slot is a position in the device's own enumeration, and its address is
whatever the device put there — a port on a frame, a unit on a controller. The
address appears in the slot's identity, because on a controller a slot number
is a position in a list rather than a place in a frame.

A slot past the end of the enumeration is still addressed rather than refused:
a gateway ages a map entry out after sixty seconds of silence, so a node
missing from the list is one that has gone quiet.

## Walking a menu

A menu is a flat array with nested spans: a container's step is the size of its
whole subtree, not the count of its children. The walk turns that into a tree,
and the tree is what makes `--label` and `--path` work without the caller
knowing a command number.

Both generations produce the same tree. A 16-bit client fetches a block header
and then each line by offset; a long-string client asks for a count and then
each line by absolute index. What arrives differs in width, not in meaning.

Menus are cached per slot. A `FUNCLISTCHG` or `FUNCSTYLECHG` push drops the
cache, because a line that became hidden or disabled is a line whose access we
would otherwise report wrongly.

## Values

A read answers with the value; a write answers with the value that was
**stored**. That is worth relying on: a device clamps silently, and only the
reply says what it clamped to.

| Kind | Wire form | Note |
|---|---|---|
| number | `ModeValue`, scaled by the line's divisor | a real is an integer over a factor; the format string is where the unit lives |
| boolean | `ModeValue`, 0 or 1 | on a checkbox the *minimum* field is the "on" value, not a bound |
| string | `ModeString` | 19 usable bytes in the older generation, 63 in the newer |
| raw | `ModeData` | reported, never written |

Both `ModeValue` and `ModeString` set at once is normal rather than
exceptional: a device answers a checksum with the number -1686180113 *and* the
string "0x9B7EEEEF", and a display wants the string.

## Subscriptions

Two messages open a value stream and the second is easy to miss: opening the
back channel makes a session able to receive pushes, and asking for change
reporting is what makes a device send value pushes at all. With only the first,
a subscription looks live and nothing arrives.

Every push is a request. The acknowledgement is what asks for the next, so it
goes after the callback has run: sending it first throws away the only flow
control the protocol has.

## Files

The file service is not an extra. A router publishes its names through it, and
the vendor's Control Panel will not render a device without reading
`TEMPLATE.ZIP` from it.

Two fields change meaning between a request and its own reply, and getting it
wrong reads a file as a stream of empty blocks rather than failing:

| Message | `rOffset` | `rExtra` |
|---|---|---|
| `SP_FILEOPEN` | unused | the open flags |
| `SP_RETFILEOPEN` | the peer's block size | the error |
| `SP_FILEREAD` | where to read from | how many bytes |
| `SP_RETFILEREAD` | how many were read | the error |

Always open binary. Text mode makes a peer translate line endings, which
corrupts an archive or a names file silently: the read succeeds and the bytes
are wrong.

## Routers

A router has no menu. See [runbook.md](runbook.md) §6 for the operational view;
the shape of the client is:

```go
r, _ := p.FindRouter(ctx)                     // probes command 100 on each node
names, _ := p.LevelNames(ctx, r, 1, 1, 32)    // bulk file, checksum-verified
p.WatchRoutes(ctx, r, func(x Crosspoint) {…}) // tally
p.SetRoute(ctx, r, 1, 1, 4, src, 0)           // make a route
p.Protect(ctx, r, 1, 1, 4)                    // who holds it
p.FireSalvo(ctx, r, 2)
```

`FindRouter` probes because nothing in a device list says which node serves the
interface. The test is strict: the interface version must be a number in a
plausible range, because a card is not obliged to refuse command 100 — on a
Centra the input cards answer it with their own model number as a string.

## Compliance

Every deviation is absorbed and counted rather than worked around. The list is
in [runbook.md](runbook.md) §7. `ComplianceEvents()` returns them, one entry per
kind with a count and the most recent detail: a device that misbehaves on every
one of sixty-five thousand destinations must not fill memory with the evidence.
