# Display (SP_DISPDATA 10)

A unit own character display, pushed to whoever is watching.

## Spec

RollCall Rev 14 section 11.8. A signed line number and up to twenty
characters.

## What these bytes show

    DISPDATA [back] 0000-81-00:48 -> 0000-00-00:16   line 0 ""
    DISPDATA [back] 0000-81-00:48 -> 0000-00-00:16   line 1 ""

Two blank lines, on the back channel, from a node with nothing to say. Blank
is a value: it is how a display is cleared.

These arrive unsolicited alongside tallies, which is why a client that is not
expecting them must skip them rather than treat them as a malformed reply.
