# ADR-0033 — Alarm template: thresholds are per-model data, evaluated at the edge

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

ADR-0031 fixed the vocabulary: an **alarm** is a condition carrying a
severity and a lifecycle (raise → active → clear), and it may reach us
by poll or by notification. It did not say where the numbers live —
which value is "too hot", how long it must stay there, who decides.

The plant made the gap concrete. A Riedel FusioN6 publishes values and
nothing else: no severity, no rule, no MIB (its REST API has none, and
the module has no SNMP agent). An ACP1 card publishes its own alarm
objects with a priority and an on/off text. An Ember+ provider
publishes ranges. Three protocols, three amounts of help, and one
operator who needs the same answer from all of them: **is this
normal?**

Two wrong turns were available:

- put the thresholds in each connector, so `mnset` grows a temperature
  rule and `acp1` grows another, and the plant's policy lives in Go;
- put them in the observability stack (Prometheus alert rules, Grafana
  thresholds), where they are one query language away from the device,
  cannot see a value the poller did not export, and cannot be shipped
  with the card.

## Decision

### 1. A template is per model, and it is data

Thresholds live in a JSON template keyed by the ADR-0022 identity
(`FusioN6@0x68cd783f`), beside the DM they describe:

```
.cache/alarm/<proto>/<Model@SwRev>.json   this card type
.cache/alarm/<proto>/_default.json        every card of this protocol
```

One row per path pattern (`*` one segment, `**` any depth), first
match wins. Four row kinds cover what a plant alarms on:

| kind | the rule |
|---|---|
| `number` | low and high bands, each with its own raise and clear point |
| `counter` | must keep rising; a decrease is a wrap or a reset, not a stall |
| `enum` | value → severity |
| `text` | drift from the expected value (literal or regex) |

A row names its `source` — a device threshold, a manual, a site rule.
A row without one is a guess, and this repo does not ship guesses. An
object no row covers is `info`: its changes are reported, never alarmed.

### 2. The severity ladder is X.733, and it maps to RFC 5424

`normal / minor / major / critical`, plus `info` for "no rule" and
`error` for "cannot be judged" (unreadable value, wrong type). The
ladder is ordered, so a worsening is a comparison. Every emitted line
carries the RFC 5424 code (`normal`→notice, `minor`→warning,
`major`/`error`→error, `critical`→critical), so one syslog line is
enough for any collector.

### 3. Evaluation happens at the edge, on samples we already have

`internal/consumer/alarm` consumes the neutral `consumer.Event` every
connector already produces — pushed by the device, or polled by the
ADR-0030 monitor — and returns a transition. It reads no device, starts
no goroutine, and keeps state only for objects a row covers. A
connector needs no code to gain alarms; a connector that never
subscribes gains them as soon as it does.

This is deliberate: the observability stack receives verdicts, it does
not compute them. Prometheus and Loki hold history and routing; the
decision belongs where the value, its unit, its range and its model are
all in hand.

### 4. Anti-flap is part of the rule, not of the operator's patience

Two stages, both per row:

- **hysteresis** — raise and clear points differ, so a value hovering
  on a threshold raises once (`clear` on each band; absent means none);
- **hold** — a candidate verdict must persist before it is adopted,
  with its own `clear_hold` coming back.

A `flap_cap` bounds transitions per minute: beyond it the object is
reported once as flapping and silenced until it settles, while its
verdict keeps tracking the device.

### 5. Editing is a verb, and it is idempotent

`dhs consumer <proto> alarm list | get | set | test | export | import`.
`set` and `import` report `changed=true|false`; `export → import →
export` is byte-stable. `test` evaluates a value against the rules with
no device at all, which is how a template is authored and how a
production change (a new expected frequency, a new multicast range) is
checked before it is applied.

## Consequences

- One implementation, every protocol. A plant writes its policy once
  per card model, not once per connector.
- A device that publishes its own thresholds (SFP DDM alarm points,
  ACP1 alarm objects) feeds them into the same rows through the DM, so
  the template states only what the device does not.
- The alarm severity travels on the existing syslog/structured-log path
  (`watch`), so Loki and Grafana need no new mechanism, only a label.
- Templates are cache-bucket artifacts (ADR-0020): gitignored, shipped
  per site, restorable by `import`.
- What this ADR does NOT decide: notification routing (mail, Slack,
  Alertmanager) and alarm persistence across restarts. Both are
  `dhs-srv` concerns; the engine deliberately holds its state in memory
  and emits transitions.

## Revisions

- 2026-09-23 — first version, with `internal/consumer/alarm` and the
  `alarm` verb (issue #1110 branch). Engine at 100 % coverage; worked
  example is the FusioN6 (SFP temperature bands from the module's own
  DDM thresholds, packet-counter stall, PTP lock enum, multicast drift).
