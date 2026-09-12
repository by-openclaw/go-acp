# Session (SP_CALL 2, SP_ACK 1, SP_TERM 3)

A session is opened per unit, per set of services, and the generation is
decided here — not by the device, and not per connection.

## Spec

RollCall Rev 14 section 9. `SV_LONGSTR` (bit 15) in the service mask of the
call is what asks for the 32-bit generation; a unit that grants it answers
every structure in the wider forms for the life of that session.

## What these bytes show

    CALL 0000-00-00:1 -> 0000-08-00:none   Map 16-bit      level=Supervisor
    ACK  0000-08-00:1 -> 0000-00-00:1
    CALL 0000-00-00:2 -> 0000-08-00:none   Menus|Control|LongStr 32-bit
    ACK  0000-08-00:2 -> 0000-00-00:2

Two sessions to **the same unit**, one in each generation, seconds apart. The
map service is asked for in the older forms and the menu and control services
in the newer ones, because the generation is a property of the session rather
than of the device. A dissector that decided the generation once per
connection would decode the second of these wrongly.

The destination index is `none` on a call and carries the granted index on the
acknowledgement: that number is how every later message says which session it
belongs to.

Services are all-or-nothing. Naming one the unit does not have refuses the
whole call with `SP_NACK` — see `../refusal/`.
