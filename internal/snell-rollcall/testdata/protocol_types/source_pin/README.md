# Source pin — a crosspoint (Full Control section 5.2.1)

The busiest command on a live router, and the one most often decoded wrongly.

## Spec

A routed source is **Data Transfer Params**, not a number. The parameters
carry a packed pin and a result:

    bits 31..24  matrix
    bits 23..16  level
    bits 15..0   source

Source numbers count from one, so a source of zero means nothing is routed.

## What these bytes show — the whole transaction

    SETVALUE        cmd=316   0x01010004     take m1/l1/s4
    RETVALUE        cmd=316   0x01010006, 0  the pin from BEFORE, and the result
    RETVALUE [back] cmd=316   0x01010004     the tally: s4 is on it now

The middle frame is the part everybody gets wrong, and this is a vendor
controller answering in its own bytes: **a route is answered with the
crosspoint as it was before the change**, plus a result code. What was actually
routed arrives afterwards, unsolicited, on the back channel.

A client that treats the reply as confirmation reports the previous source as
the new one. Our own provider had this backwards once.

## Why command 316 is not named here

316 belongs to a level destination table, and a controller publishes the bases
and steps those tables are addressed by once, as a client connects. This
capture holds five TCP streams and none of them carries the level tables — the
client already had them.

So the dissector says it cannot name the command, and offers the pin as a
reading rather than recording it as a fact. `dhs_snell_rollcall.source_pin` is
deliberately not set: a protect word is the same width and the same shape, and
nothing in these bytes tells them apart. A complete capture, where the tables
do go past, names it — see `ansible/playbooks/snell-rollcall-dissector.yml`.
