# ADR-0035 — RRCS: a stream in NMOS mode is not edited through RRCS

Status: proposed

This ADR is a living document. It records a rule taken on evidence that is
still incomplete; the cause is under investigation with the plant's
engineer. Add new facts in the Revisions trailer.

## Context

The RRCS connector edits the configuration of a Riedel Artist system
through `ConfigurationChange` (specification 9.0.1, §8.10). An AES67
sender or receiver is a port (`portex`) with a stream block
(`PortAes67Output`, `PortAes67Input`) that holds, among others, `Protocol`
(2 manual, 3 RTSP, 5 NMOS), `Multicast` and `MulticastPort`.

On the production system of the plant (RRCS 9.0.RR1-11, one Artist-1024,
node firmware 9.0.U1-46, AES67 client cards) the addresses of the streams
are given by the NMOS registry and controller, not stored in RRCS: every
sender in NMOS mode reads `Multicast 0.0.0.0` in RRCS.

Three edits of a stream block were sent to that system:

| Date | Request | Stream after the edit | Result |
| --- | --- | --- | --- |
| 2026-10-06 22:14 | `ConfigurationChangeEx`, multicast address of a sender in NMOS mode | NMOS | the RRCS process stopped; restarted by hand 9 minutes later |
| 2026-10-08 21:31 | `ConfigurationChange`, `Protocol` 5 → 2 with `Multicast` and `MulticastPort` | manual | accepted, read back |
| 2026-10-08 21:34 | `ConfigurationChange`, `Protocol` 2 → 5 with `Multicast` and `MulticastPort` | NMOS | the RRCS process stopped; it restarted by itself 21 seconds later |

In both stops the last line of the RRCS log is
`Configuration Update  Applying change: Edit PortEx`, with no error line.
The edit of 21:34 was not applied: the port read back unchanged.

What does not explain the stops:

- the form of the method: one stop with `ConfigurationChangeEx`, one with
  `ConfigurationChange`;
- configuration changes as such: on the same day, through
  `ConfigurationChange`, the creation, edit and deletion of a key
  function, the edit of a key, and the creation and deletion of a
  conference all passed; a request RRCS did not like (an empty key label,
  a property on a linked channel) was refused with fault 99 and changed
  nothing.

What the two stops share: stream fields given to a stream that is, or
becomes, NMOS.

## Decision

1. The connector does not send an edit of a `portex` stream block whose
   protocol is, or becomes, NMOS (`Protocol` 5). `set`, `import` and
   `ensure` refuse it before anything is sent, in the dry run as well, and
   say why.
2. On a plant where NMOS gives the addresses, the multicast address and
   port of a stream are out of the connector's scope. They are not part of
   an `ensure` file for such a plant.
3. An edit that leaves the stream in manual mode stays allowed. It is the
   one form seen to pass.
4. A stream that must return to NMOS mode is returned with the vendor's
   own tool (Director) until an edit towards NMOS has passed on an oracle
   (ADR-0034).
5. The refusal is lifted only by a revision of this ADR that names the
   request that passed and where.

## Consequences

- One stream of the production system (node 63, port 1064) was left in
  manual mode by the test of 2026-10-08 and has to be returned by hand.
- A request not yet tried — `Protocol` 5 alone, with no other field — may
  be the correct way back to NMOS. It is a test for the oracle, not for
  production.
- Card-level settings (NMOS registry address and mode, PTP domain) are
  edits of `client-card`, not of a stream block. This ADR does not cover
  them; none has been sent to a real RRCS yet.
- The guard is by observation, not by specification: the specification
  does not forbid the edit. If the vendor confirms a defect and fixes it,
  the rule narrows to the affected versions.

## Open questions for the vendor

1. Is editing `PortAes67Output` of a port in NMOS mode through
   `ConfigurationChange` supported on RRCS 9.0 with an Artist-1024?
2. If it is not, why does RRCS stop instead of answering a fault, as it
   does for other requests it refuses?
3. What is the supported request to return a port from manual to NMOS
   mode?

## Revisions

- 2026-10-08 — first version, from the two production stops and the
  passing edit of the same day.
- 2026-10-08 — a link is not a stream edit. Linking a port to the stream
  of another (`Mode` = the main port, `Selection` = the channel, nothing
  else in the block) carries no address and no protocol, and RRCS itself
  names it as a case of its own ("When linking to a port, no further
  property than 'Selection' is allowed to be set"). The guard of decision 1
  lets it through. No link has passed on a real RRCS when this is written.
