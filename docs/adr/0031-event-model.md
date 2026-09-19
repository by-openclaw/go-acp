# ADR-0031 — Event model: notification, trap, alarm; schema vs stream; OID+kind spine

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

Every protocol tells us when something changes — SNMP by trap, Ember+ by
subscription update, ACP2 by announce, Probel by tally broadcast. The
words differ and got conflated: is a "notification" the same as a "trap",
is an "alarm" a kind of notification, does any of it belong in the device
model? Without one set of definitions, each protocol invents its own and
the monitor, the DM, and a future `dhs-srv` disagree.

## Decision

### 1. Two axes, never merged

- **Delivery** — how we learn of a change:
  - **poll** — we asked (a scheduled read);
  - **notification** — the device pushed it, unasked. **Trap** is SNMP's
    word for a notification; Ember+ subscription updates, ACP2 announces,
    Probel tally broadcasts are all notifications.
- **Semantic** — what the change is about:
  - an ordinary **value/attribute** moved, or
  - an **alarm**: a condition that carries a severity and has a lifecycle
    (raise → active → clear).

An alarm is not a kind of notification. An alarm transition can reach us
by poll or by notification; a notification may carry a value change or an
alarm change. Code names the two axes separately.

### 2. Schema vs stream

- The **device model (DM)** is the **schema**: what objects, parameters,
  matrices and alarms exist, their types, ranges, OIDs, and alarm
  definitions. Static, versioned, cached.
- The **monitor** is the **live stream**: values and changes against that
  schema. Dynamic, never persisted as trusted state.

So an alarm's *definition* lives in the DM; the alarm *event* lives in the
monitor stream. A trap's *meaning* (which OID is `alarmMajor`) is DM/MIB;
the trap *arriving* is a monitor event. Schema in one place, state in
another.

### 3. The spine is OID + canonical kind, not a protocol's groups

Every protocol is OID-addressed (Ember+ RelOID, ACP OID, SNMP OID), so
addressing is already generic. The generic structure is the **canonical
element tree** (`internal/export/canonical`): Nodes, Parameters,
Matrices, Functions, addressed by OID — the model that already spans
router, objects and assets.

The DM, monitor topics, and events therefore key on **OID + canonical
element kind**. A protocol's own categories — acp1's AxonNet groups
(identity / control / status / alarm / file / frame), a MIB subtree —
are **metadata that maps into** the canonical tree, an input, never the
spine. No connector's dialect is the universal taxonomy.

## Consequences

- The monitor emits one neutral event shape for every protocol; a
  subscriber cannot tell trap from announce from poll-delta except by the
  delivery field.
- seq-num topics (next) key on OID + kind, so one topic scheme serves all
  protocols and `dhs-srv`.
- The DM never carries live state; the monitor never redefines schema.
- acp1's groups stay as a per-device tag, not a cross-protocol structure.

## Revisions

- 2026-09-19 — Initial proposal. Pins the notification/trap/alarm
  definitions, the schema-vs-stream split, and the OID+canonical-kind
  spine, after a design discussion that kept conflating the terms.
