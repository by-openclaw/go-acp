# ADR-0035 — RRCS: how a stream block is edited safely

Status: accepted (2026-10-14). The rule of the first version, taken on two
production stops, is now narrowed to its proven cause; every form below
has passed on the production RRCS 9.0 of the plant.

## Context

The RRCS connector edits the configuration of a Riedel Artist system
through `ConfigurationChange` (specification 9.0.1, §8.10). An AES67
sender or receiver is a port (`portex`) with a stream block
(`PortAes67Output`, `PortAes67Input`) that holds `Protocol` (2 manual,
3 RTSP, 5 NMOS), the addresses (`Multicast`, `MulticastPort`, `SourceIp`),
`Channels`, and the link fields `Mode` (the main port a channel is taken
from) and `Selection` (the channel).

On the plant (RRCS 9.0.RR1-11, one Artist-1024, AES67 client cards) the
addresses of the streams are given by the NMOS registry, not stored in
RRCS: every stream in NMOS mode reads `Multicast 0.0.0.0`.

### What stopped RRCS, and what did not

| Date | Request | Result |
| --- | --- | --- |
| 2026-10-06 22:14 | `ConfigurationChangeEx`, multicast address of a sender in NMOS mode | RRCS stopped; restarted by hand |
| 2026-10-08 21:31 | `Protocol` 5 → 2 with `Multicast` and `MulticastPort` | accepted, read back |
| 2026-10-08 21:34 | `Protocol` 2 → 5 with `Multicast` and `MulticastPort` | RRCS stopped; restarted by itself |
| 2026-10-13 | `Protocol` 2 → 5 **alone**, no other field (In. and Out. -7.49) | accepted, read back, RRCS kept running |
| 2026-10-13 | `Protocol` 5 → 2 with `Channels` 8 | accepted, read back |
| 2026-10-13/14 | the same round trip on 19 main ports of card 4 | accepted, read back |

The two stops share one thing: **address fields handed to a stream that
is, or becomes, NMOS**. The mode alone never stopped anything. The guard
of the first version refused the mode alone too; that was caution, not
evidence, and it is lifted.

### What the hardware imposes on links (proven 2026-10-13, card 4, block 2)

| Side | Form | Why |
| --- | --- | --- |
| input | `Mode` alone, then `Selection` alone | a receiver may start on channel 1 beside others |
| output | `Mode` and `Selection` in **one** request | each output feeds its own channel into the main's stream; `Mode` alone collides with channel 1 of the main (fault 99), `Selection` alone exceeds a 1-channel stream (fault 99) |
| any | nothing else beside `Mode`/`Selection` | "When linking to a port, no further property than 'Selection' is allowed to be set" (fault 99) |
| a linked port | accepts `Selection` only; its own `Protocol` field is dormant and may read Manual without effect | to change anything else: unlink (`Mode` = its own port, `Selection` 1), edit, relink |
| the main | must carry the channel count first (`Channels` ≥ highest `Selection`) | a `Selection` above the main's count does nothing (the silent failure of 2026-10-10) |

### What a port is (proven 2026-10-14, -7.33 and -7.35)

| Change | Form |
| --- | --- |
| create | `ChangeType create, ObjectType portex, PortAddress{Node, Port, IsInput false}, PortType 3` — an unused port becomes 4-Wire Split (the types: `GetPortExTypeList`) |
| names | two `edit`s, one per direction: `Label` (8), `LongName` (32). RRCS auto-names nothing; Director's client composes "In. -7.N - Bay B - node name" |
| delete | `ChangeType delete` with the address alone |
| edit of an absent port | fault 99 "The specified port does not exist" |

## Decision

1. An edit that hands address fields (`Multicast*`, `SourceIp*`, `RTSPUri*`)
   to a stream that is, or becomes, NMOS is refused before anything is
   sent — in the dry run as well — by `set`, `import` and `ensure`. It is
   the proven cause of two stops.
2. The mode alone (`Protocol` as the only field of the block) is sent
   without a flag. `--nmos-test` of `set` stays accepted and changes
   nothing.
3. A main port's channel count on an NMOS stream is set by the round
   trip: one edit towards manual with the count, one back to NMOS with
   the mode alone.
4. Links follow the hardware's forms of the table above: inputs in two
   requests, outputs in one, nothing beside `Mode` and `Selection`.
5. A port is created from its type, named per direction, and deleted by
   its address; `import` creates from a `PortType` row and names by the
   Director pattern. Deletion stays a raw `configuration-change`, not an
   `import` semantic.
6. Everything above is the write contract of the stream rows in `import`
   and of the `streams` section of `ensure`; the dry run shows the exact
   requests.

## Consequences

- A whole card goes from `<unused>` to streaming blocks with one CSV:
  `PortType`, `Channels`, `Mode`, `Selection` rows — proven on card 4
  (352 rows, 271 applied, second run unchanged) and on -7.35.
- `list streams` prints the block's `Protocol` under PROTOCOL; the word
  Mode is reserved for the link. A linked port's dormant Manual is
  cosmetic; the inventory marks a port by its link, not by that field.
- Card-level settings (NMOS registry, PTP) are edits of `client-card`,
  not covered here; none has been sent to a real RRCS yet.
- The guard of decision 1 is by observation, not by specification. The
  vendor's answer to the question below narrows it to the affected
  versions, or removes it.

## Open question for the vendor

1. Why does RRCS 9.0.RR1-11 stop, instead of answering a fault, when a
   `portex` edit carries address fields for a stream in NMOS mode?

## Revisions

- 2026-10-08 — first version: two production stops, one passing edit.
- 2026-10-08 — a link (`Mode`, `Selection`) is not a stream edit; the
  guard lets it through.
- 2026-10-09 — the link works once Director's change notification is on
  (In. -7.58 → -7.57 channel 2).
- 2026-10-13 — the mode alone passed, twice; the round trip for the
  channel count; the input/output forms of a link; the linked-port rule.
- 2026-10-14 — port create, names and delete; card 4 provisioned by one
  CSV through import. Status set to accepted.
- 2026-10-21 — the forms of this ADR moved into the library
  (`internal/rrcs/consumer/change.go`: the change, the NMOS guard, the
  stream steps, the typed values, the Director-pattern names); set, import
  and ensure keep them through stand-ins, and the plugin's `SetValue`
  carries a property the lists report the same way — mode alone, the
  channel-count round trip, an input's `Mode` as the first request of a
  link; an output's link needs `Mode` and `Selection` in one request and
  stays with set, import and ensure. `import` checks the type of a port
  to create against the gateway's own type list.
