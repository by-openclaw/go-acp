# ADR-0037 — Connector parity: the generic verbs answer through the plugin, extensions beside them

Status: accepted (codeowner's go on the numbered decisions P1–P9, 2026-11-02)

This ADR is a **living document** for its trailer only: the Revisions
trailer names the commit of each decision as it lands, so one decision
can be rolled back with `git revert <commit>` without touching the
others. The decisions themselves are permanent (ADR README rule 2).

## Context

Every connector registers a `consumer.Protocol` (Connect, Walk,
GetValue, SetValue, Subscribe). The generic verbs, the alarm evaluator,
the health check, the metrics endpoint, the neutral monitor, `validate`
and the Ansible `dhs_verb` role all work on that interface. A connector
may add verbs of its own beside the generic ones (ccm, snmp, mnset do).

rrcs did not follow that shape on the command line. Its plugin exists
and implements the whole contract (ADR-0035 carries the proven write
forms into SetValue), but a private dispatcher caught every
`dhs consumer rrcs …` call: `health`, `status`, `alarm`, `diff` and
`validate` answered "unknown verb", and `get`, `set`, `walk`, `export`
were private copies that opened their own client. Nothing on the command
line used the plugin. `docs/adr-audit.md` §3 and
`internal/rrcs/docs/mapping.md` recorded the gap.

On 2026-11-02 the codeowner set four requirements for every connector:

1. one generic `get` and `set`, the same for every connector;
2. a dhs client must learn the device model of an answer — kind, access,
   minimum, maximum, default, unit, named values — not only the value;
3. one controller may front several devices (an RRCS gateway fronts an
   Artist ring of several nodes);
4. site rules narrower than the factory limits (ADR-0036) must be
   enforceable, and a role such as a stream block or a panel must become
   something a client sets.

And two conditions: every step committed with a rollback path, and this
ADR as the contract.

## Decision

The decisions keep the numbers the codeowner confirmed them under.

### P1 — A connector's verbs first, then the generic table

`dispatchConsumer` gives a connector with verbs of its own the first
look; a verb it does not know falls through to the generic verb table
with `--protocol <proto>` injected, as ccm, snmp and mnset already do.
No connector may answer "unknown verb" for a generic verb.

A connector may shadow a generic verb with one of its own only while the
Revisions trailer of this ADR names the reason and the condition under
which the shadow is lifted.

### P2 — get, set, walk, export answer through the plugin

For rrcs the private `get`, `set`, `walk` and `export` retire. The
generic verbs answer through the registered plugin, which declares
`PathNative` (its paths are the device's own addresses, no walk is
needed to resolve them) and attaches the ADR-0028 recorder like every
other plugin, so `--capture` keeps working.

| Kept as rrcs extension | Why |
|---|---|
| `list`, `tree`, `xp`, `call`, `coverage`, the one-verb-per-method ops | no generic equivalent |
| `snapshot` (the former private `walk`) | everything readable in one document — 4,000 requests on a plant — for `tree --from`, `list --from`, `extract --from` |
| `import`, `ensure` | they carry roles: creates with names, channel count by round trip, links of outputs in one request of two members — what one property cannot carry (ADR-0035). Lifted by P8 |
| `watch` | it owns the callback endpoint RRCS calls back on, answers GetAlive, re-reads after ConfigurationChange. Lifted when the plugin picks its callback endpoint by itself |

A generic `get --path` of an object without a property (a port, a card)
answers the object's properties as one JSON document, so "show me this
port" stays one command.

### P3 — The documentation states what is built

`internal/rrcs/consumer/plugin.go`, `internal/rrcs/docs/mapping.md` and
`internal/rrcs/docs/adr-audit.md` say what the code does, not what it
did. A document that describes a plan marks the plan's status in its
first lines.

### P4 — Every get and set answer names its device model

The JSON of `get` and `set` (`--output json`) carries, beside the value:

```json
"meta": {"kind": "int", "access": "rw", "min": 1, "max": 16, "default": null,
         "unit": "", "enum_items": [], "max_len": 0},
"dm":   {"model": "AES67", "sw_rev": "9.0.RR1-11",
         "template": "FOUR_WIRE_SPLIT",
         "path": "FOUR_WIRE_SPLIT.in.PortAes67Input.Channels"}
```

Text mode prints the same meta lines after the value for `--path` as it
did for `--label`.

A plugin answers it through the optional `consumer.Describer` contract:
`Describe(ctx, req) (Object, DMRef, error)`. A plugin without it gets
what the CLI can find: the object of the walked tree for the meta, the
identity probe for model and software revision, no template. The DM
reference is a pointer into the device model file of ADR-0022
(`.cache/dm/<proto>/<Model@SwRev>.json`), never a copy of it.

### P5 — One write guard, the same words on every connector

`set`, `import` and `ensure` take `--dry-run` (ensure's `--check` of
ADR-0007 is the same switch) and `--write-to HOST`, which names the
target a second time, exactly as the host argument was given.

A plugin declares that its writes need the guard by implementing
`consumer.WriteGuard` (`WriteGuardReason() string`). For such a plugin a
write without `--write-to` is refused with a validation error that
names the host and the way to confirm it; a `--write-to` that differs
from the host is refused. rrcs declares the guard: on 2026-10-06 a
configuration write stopped a production gateway (ADR-0035).

A plugin without the declaration keeps writing at once when neither flag
is given, so the existing plays of the other connectors do not change
behaviour by this ADR. The codeowner may declare the guard on any plugin
with one method; the flag words and the refusal text are already there.

### P6 — The rrcs plugin validates offline

The plugin implements `consumer.ValueValidator`: path grammar, writable
through a configuration change, type, range and named values of the
specification's tables. The generic dry runs refuse before anything is
sent, as acp2's do.

### P7 — Site limits built (ADR-0036)

ADR-0036 stays the rule. This ADR decides its two open points:

| Point | Decision |
|---|---|
| Where | `snapshots/<proto>/<host>/site-limits.csv` by default; `--site-limits FILE` overrides, `--site-limits off` disables |
| Format | CSV, header `path,min,max,allowed`; `path` is a property path or a suffix starting with a dot (`.PortAes67Input.Channels` applies to every port); `allowed` is `a|b|c` |

The shared validation loads the file, refuses a row wider than the
factory limit when it is loaded, and refuses a value with a message that
names both ranges: `refused by the site limit (site 2..8, factory
1..16)`. It applies where all validation applies: `set`, `import`,
`ensure` dry runs and writes, generic and rrcs-own.

### P8 — A role is one object a client sets (design)

A role is what the manifest says a port is for: `stream-block` (a main
with N channels), `stream-linked` (channel K of main M), `panel`. Today
only `ensure --manifest` writes it. The target shape, built after P2 and
after the plant has agreed on the grammar:

- one object per port, path `<port>.role`, value a short text:
  `stream-block:8`, `stream-linked:1096/2`, `panel`, `port`;
- `get` of it answers the role derived as `extract` derives it (D11 of
  the design document: never guessed silently);
- `set` and `ensure` of it run the proven steps of ADR-0035, including
  the one request of two members an output link needs;
- the manifest stays the inventory of roles; this object is how one of
  them is read or changed without a file.

Nothing of P8 is in the code yet. Its commit, when it lands, is named in
the trailer.

### P9 — Several devices behind one controller

The generic flags take `--node N`. A plugin whose target fronts several
devices (rrcs: an Artist ring with several nodes behind one gateway)
counts the slots of that node only. With several nodes and no `--node`
the plugin refuses and lists the nodes; with one node the flag is
optional. The manifest holds one device entry per node; the device
models are shared by model and version, as ADR-0022 keys them.

### Rollback

- Tag `parity-base-20261102` marks the last commit before this ADR (the
  build that provisioned the plant's three AES67 cards on 2026-11-02).
- One decision, one commit, named in the trailer. `git revert <commit>`
  rolls one decision back; `git checkout parity-base-20261102` is the
  whole way back.

## Consequences

- `dhs consumer rrcs health|status|alarm|diff|validate` work, and every
  future generic verb works on rrcs the day it lands.
- A client reads one JSON record and knows the value, its limits and
  which device model file describes it, on every connector.
- Writes are confirmed the same way everywhere; a plant can enforce its
  own limits without touching the binaries (ADR-0036).
- `walk` changes meaning on rrcs: it is the generic slot walk; the
  whole-system document is `snapshot`. `set --prop A=1 --prop B=2` (two
  properties in one request) is `import` of a two-row file until P8.
- A verb that stays rrcs-own has a stated reason and a lifting
  condition; the audit of `docs/adr-audit.md` §3 is closed by this list.

## Revisions

| Date | Decision | Commit | Note |
|---|---|---|---|
| 2026-11-02 | contract, P3 | (this commit) | ADR written; plugin.go, mapping.md, adr-audit.md state what is built |
