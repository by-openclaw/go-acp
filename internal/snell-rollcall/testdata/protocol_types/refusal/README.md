# Refusals (SP_NACK 0, SP_INVCMD 14, SP_BUSY 15, SP_INVSESS 23)

Four ways of saying no, and they mean different things.

| Type | Meaning |
|---|---|
| `NACK` 0 | I understood and will not |
| `INVCMD` 14 | I do not know this message |
| `BUSY` 15 | try again |
| `INVSESS` 23 | not on this session |

## What these bytes show

    NACK 0000-08-00:18 -> 0000-00-00:2
    NACK 0000-11-00:19 -> 0000-00-00:3

Both answer a `GETVALUE` for command 100 on a node that does not serve the
routing interface. That is the correct answer and not a fault: the Full
Control tables live on one node of a plant, and finding it means asking
several and being refused by most.

## Two things the Centra does that the specification does not

- It sends `NACK` where the specification has `BUSY`, including when it has
  run out of sessions. So a refusal is worth retrying before it is believed.
- Units do not time idle sessions out. The vendor own note is that they
  "don't time them out very well (at all), and then run out of available
  sessions" — a unit talked to by a crashed client stays that way until it
  reboots.
