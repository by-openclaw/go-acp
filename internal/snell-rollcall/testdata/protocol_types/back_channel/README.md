# Back channel (SP_BKCHNREADY 27, SP_REPFCHG 36, and unsolicited replies)

How a client is told about changes it did not make.

## Spec

RollCall Rev 14 section 11.7. The client opens the back channel, asks for
change reports, and the unit then pushes value messages with the back-channel
flag set in the header.

## What these bytes show

    BKCHNREADY 0000-00-00:16 -> 0000-81-00:48   open and flush
    REPFCHG    0000-00-00:16 -> 0000-81-00:48
    RETVALUE [back] 0000-81-00:48 -> 0000-00-00:16   cmd=316 0x01010004

The third frame is a crosspoint arriving with nobody having asked for it. That
is a tally.

## The trap

**Subscribing does not replay current state.** A panel that only subscribes
sees nothing at all until something moves, whatever the specification implies.
Every client here reads the level once and then follows it, which is what
`dhs consumer rollcall tally` does and why it prints before it waits.
