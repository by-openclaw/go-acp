# Status (SP_GETSTAT 4, SP_RETSTAT 5 — STATUS_STR)

Which services are busy, and what state the unit is in.

## Spec

RollCall Rev 14 section 11.3.1. A busy mask over the same service bits the
call uses, then a status word.

## What these bytes show

    GETSTAT 0000-00-00:2 -> 0000-08-00:2
    RETSTAT 0000-08-00:2 -> 0000-00-00:2   online|present

`online|present` is the ordinary answer from a unit that exists and is
answering. The status word is what a frame-level view reports per slot, and it
is read once per node during an enumeration.
