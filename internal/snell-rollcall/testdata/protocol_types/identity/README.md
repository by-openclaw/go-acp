# Identity (SP_GETID 6, SP_RETID 7 — ID_STR)

What a unit says it is. On a router this is the single most load-bearing
exchange in the protocol.

## Spec

RollCall Rev 14 section 11.3.2. Twenty-eight bytes: a service mask, a type id,
a four-byte VERSION_STR (major, minor, an alpha character, the command set),
and a twenty-byte name.

## What these bytes show

    GETID 0000-00-00:2 -> 0000-08-00:2
    RETID 0000-08-00:2 -> 0000-00-00:2   <not set>

A unit that has never been named answers `<not set>`, which is a name and not
an error.

## Why it is load-bearing

The type id decides what every command number on that node means. 100 is the
interface version on a node typed 734 and the selected destination on one
typed 637, and nothing in a value message distinguishes them. The dissector
records the type from this reply and names commands through it; a capture that
misses this exchange cannot name the node's command space at all, and says so
rather than guessing.

Router node types: 636 Router Matrix, 637 Router Level, 731 TIELINES,
734 XY Panel (the Full Control tables).
