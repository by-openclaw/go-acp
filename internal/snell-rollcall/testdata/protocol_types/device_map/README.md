# Device map (SP_GETDEVLIST 19, SP_BLOCKHEADER 39, SP_GETNEXTPKT 35, SP_RETDEVINFO 20)

How a client learns what a device contains. It is a block transfer, not a
single reply, and the shape is worth knowing because it is the same for every
bulk read in the protocol.

## Spec

RollCall Rev 14 section 11.6. The header announces how many items and of what
type; the client then pulls them one at a time by index.

## What these bytes show

    RETDEVINFO  0000-08-00:none -> 0000-00-00:none   "<not set>" [present]
    GETDEVLIST  0000-00-00:1 -> 0000-08-00:1
    BLOCKHEADER 0000-08-00:1 -> 0000-00-00:1   15 items of GETDEVLIST
    GETNEXTPKT  0000-00-00:1 -> 0000-08-00:1   item 0 of GETDEVLIST

Fifteen nodes on this controller, pulled one per round trip. The first frame
is unsolicited — a unit announces itself on connection before anybody asks.

A `DEVICEINFO_STR` carries the node address **inside the payload** rather than
in the message header, because it describes a node other than the sender. That
is how a client learns the shape of a plant before it has spoken to any of it,
and it is why the dissector files what it learns here against the address in
the payload.
