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

One row per path pattern (`*` one segment, or a glob inside a segment;
`**` any depth), first match wins. A leading `**` also absorbs a root
the plugin puts in front of every path (ACP2's `ROOT_NODE_V2`), so a
row matches the path the device reports and the shorter one the CLI
prints. Four row kinds cover what a plant alarms on:

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

A rule that waits — a `hold`, a `stalled_for` — is a statement about
time, and time does not arrive as an event. A poller sees a condition
persist because it reads the object again (ADR-0030; the sample is
marked `Event.Repeat` so a display can skip it). A device that
**pushes** says "loss" once and then says nothing, and a stream that
stops is silence by definition. So the evaluator also answers
`Sweep()`: what has time alone made true. It reads no device, sends
nothing, and is called on a ticker by whoever holds the evaluator. A
rule therefore means the same thing whether its values arrive by
announce or by poll — which is what makes one template language
enough for every protocol.

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

`dhs consumer <proto> alarm list | get | set | test | export | import
| suggest`.
`set` and `import` report `changed=true|false`; `export → import →
export` is byte-stable. `test` evaluates a value against the rules with
no device at all, which is how a template is authored and how a
production change (a new expected frequency, a new multicast range) is
checked before it is applied.

### 6. The verdict is exported; the threshold never leaves the template

Every watch can serve `/metrics` (`--metrics-addr`), and the four
series are the same for every protocol:

| series | question |
|---|---|
| `dhs_alarm_severity{device,slot,path,label,band,severity}` | what is wrong |
| `dhs_alarm_active{device,severity}` | how much is wrong |
| `dhs_alarm_transitions_total{device,severity}` | how often it changes |
| `dhs_alarm_rules{device,model}` | is anything judging this device |

`device` is the address the operator typed, and the structured log
carries the same label with the same name, so one Grafana variable
(`label_values(dhs_alarm_rules, device)`) drives Prometheus and Loki
alike. `dhs_alarm_rules` exists while a device is healthy, so the
plant is listable, and `rules == 0` is itself reportable: a device
watched with no template looks quiet and is not.

Prometheus alert rules route severity; they do not compute it. A rule
that re-derives a threshold in PromQL is a second source of truth, and
ADR-0015 rules that out.

### 7. A plant is Ansible, one unit per device

`ansible/playbooks/alarm.yml` deploys one `watch` unit per device from
an inventory list, whatever the protocol and whichever way its values
arrive; the template is a file the play owns (`--alarm`), not cache
state, so a second run is zero changes. `alarm-verify.yml` reports the
plant's state read-only and fails on a watch that judges nothing.

### 8. A device writes its own first draft

`alarm suggest <host>` walks a device and drafts the rows the device
itself can source: an enum whose item list names a state the industry
calls broken, a read-only measurement whose declared range is a real
range, an object carrying the device's own alarm metadata (ACP1's
priority and on/off text). Objects that differ only by an index fold
into one rule (`**.PSU.*.Fan Health *`) merged to the widest evidence.

It refuses more than it writes, and says so: a writable object is a
setting, not a symptom; an item list with nothing bad in it is a
choice; a range that is the type's own width is not a limit. Nothing
is invented — a state the device did not call bad stays normal, and
every row still names its source.

The draft is for a human to read and cut down, not a policy: it is why
a plant does not type a hundred templates, and it is not a substitute
for knowing what its own kit means.

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
- Devices carry internal alarm triggers of their own, and in the field
  most are unconfigured. The template is where a plant's policy lives,
  and where a device does publish its own grades or thresholds they
  become rows (ACP2 `NA|OK|Warning|Error`, SFP DDM points), so the two
  never disagree silently.
- What this ADR does NOT decide: notification routing (mail, Slack,
  Alertmanager) and alarm persistence across restarts. Both are
  `dhs-srv` concerns; the engine deliberately holds its state in memory
  and emits transitions.

## Revisions

- 2026-09-23 — first version, with `internal/consumer/alarm` and the
  `alarm` verb (issue #1110 branch). Engine at 100 % coverage; worked
  example is the FusioN6 (SFP temperature bands from the module's own
  DDM thresholds, packet-counter stall, PTP lock enum, multicast drift).
- 2026-09-23 — `alarm suggest`: a device drafts its own rows from its
  enum item lists, declared ranges and alarm objects. Proven live —
  the EVS Neuron shelf (214 objects → 18 sourced rules, 45 settings and
  97 unevidenced objects refused) and a Riedel FusioN6 over mnset
  (7183 objects → 5 rules, from the module's own SFP DDM thresholds).
- 2026-09-23 — Prometheus + Loki + Ansible: `watch --metrics-addr`
  exports the four `dhs_alarm_*` series for every protocol, the
  structured log gained the same `device` label, and
  `ansible/playbooks/alarm.yml` + `alarm-verify.yml` run the plant.
  Navigation (the "type an address" path through Grafana, Prometheus
  and Loki) is `docs/deployment/grafana/navigation.md`. Proven live on
  dhs-debian: both watches scraped, `label_values(device)` =
  10.6.255.102 + 10.6.40.53 in both stacks, play idempotent.
- 2026-09-23 — `Sweep()` added, so a hold and a stall no longer depend
  on a next sample that a push protocol may never send; `watch` sweeps
  every second. Second worked example is ACP2: the EVS Neuron shelf
  (`SHPRM1@6.0.4`), whose rows map the device's own grades
  (`NA|OK|Warning|Error`) and declared ranges onto the ladder. Proven
  live on 10.6.255.102 — a `25 C` announced once raised at +25 s, a
  power figure that stopped moving raised at +20 s, both with no
  device traffic.
