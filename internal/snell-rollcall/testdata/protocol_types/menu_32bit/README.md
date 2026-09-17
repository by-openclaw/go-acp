# Menu, 32-bit generation (SP_GETMENUCOUNT 65 / SP_RETMENUCOUNT 66 / SP_GETMENUITEM 67 / SP_RETMENUITEM 68)

A node control surface: one line per control, each naming the command behind
it.

## Spec

RollCall 2014 long-string extension. `MENUITEM_STR` is twenty-two bytes of
fixed fields — index, style, a **32-bit** command, min, max, step, divisor —
then the label and the format string as NUL-terminated strings.

The 16-bit generation carries the same information in `FUNC_STR` with a 16-bit
command and fixed-width text (section 11.4.1).

## What these bytes show

    GETMENUCOUNT  0000-00-00:2 -> 0000-11-00:50   from 0
    RETMENUCOUNT  0000-11-00:50 -> 0000-00-00:2   3 lines from 0
    GETMENUITEM   0000-00-00:2 -> 0000-11-00:50   from 0
    RETMENUITEM   0000-11-00:50 -> 0000-00-00:2   line 0 cmd=0 List "Menu"

`0000-11-00` is a Router Matrix, and three lines is its whole menu: a root, a
way back out, and a notice. That is not a truncated read — a matrix node
controls nothing, and routing is not in the menu. A client looking for
crosspoints here finds nothing and is meant to.

## The selector convention

A list is a `StyleList` container whose children all carry **one** command,
each with its own value in the minimum-range field. Eight lines with the same
command number is a source list, not a duplicate.
