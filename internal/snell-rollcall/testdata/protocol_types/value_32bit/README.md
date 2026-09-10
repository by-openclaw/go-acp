# Value, 32-bit generation (SP_GETVALUE 69, SP_SETVALUE 70, SP_RETVALUE 71 — VALUE_STR)

Reading and writing one control. The mode word says which of the three
payloads are present: a number, a string, a block of data — or any
combination of them.

## Spec

RollCall 2014 long-string extension. Twelve fixed bytes — a 32-bit command, a
match unit type, the mode, a 32-bit value — then the string and the data.

## What these bytes show

    GETVALUE 0000-00-00:16 -> 0000-81-00:32   cmd=102
    RETVALUE 0000-81-00:32 -> 0000-00-00:16   cmd=102 ... 2
    GETVALUE 0000-00-00:16 -> 0000-81-00:32   cmd=103 CMD_MATRIX_BASE
    RETVALUE 0000-81-00:32 -> 0000-00-00:16   cmd=103 CMD_MATRIX_BASE 121

Two matrices, and their tables start at command 121. Everything else in the
router command space is arithmetic from numbers like these.

## What this slice also shows, by omission

Command 102 reads as "CMD_NUM_MATRICES, **or** DstNameIndex on a level". The
identity exchange for `0000-81-00` is not in this slice, so its type is
unknown, and without the type the number is genuinely ambiguous — 102 is the
matrix count to the node serving the Full Control tables and a name index to a
level.

That is not a defect in the fixture. It is the reason the dissector reads
identities at all, visible in four frames: see `../identity/`.
