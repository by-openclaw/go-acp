# ADR-0024 — Federation: mirror frame vs virtual frame

Status: accepted

This ADR is a **living document**. Add new facts in the Revisions
trailer at the end. Do not spawn a new ADR number unless the whole
record is being superseded.

## Context

ADR-0022 locks the per-device data model: a real device is one
manifest (`.cache/manifest/<device>.json`) whose slots reference DMs
under `.cache/dm/<proto>/`. That covers single-device deployments.

The federation layer sits on top — one or more dhs instances expose
composed views to external controllers. Two distinct modes exist:

1. **Mirror**: forward a real device's frame+slots+DMs unchanged over
   its native protocol. Same chassis identity, same wire shape.
2. **Virtual**: assemble a new device whose slots are virtual cards
   carrying objects pulled from any real device on any protocol.
   Exposed over a chosen protocol — may differ from the source
   protocols.

Both modes coexist in the same dhs instance and the same registry.

## Decision

### Mirror frame

A pass-through projection of a real device. The federation registers
the same manifest under a federation-owned endpoint and translates
every consumer request 1:1 to the upstream device. Identity, slot
layout, DM references and addressing tokens are preserved. The
external controller cannot distinguish the mirror from the original.

Used for:

- HA scale-out (cluster of dhs instances all mirror one device for N
  controllers).
- VLAN/network bridging without re-modelling.
- Capture/replay rigs (the mirror records every frame; the upstream is
  untouched).

### Virtual frame

A composed device synthesised from objects across one or more real
devices and possibly across protocols. Each virtual slot carries a
virtual card whose schema is declared at federation time, not walked
off a physical card.

Each object inside a virtual card carries an explicit reference back
to its real source:

```
{
  "id": <virtual-obj-id>,
  "label": "<friendly-name>",
  "kind": "<acp1-or-acp2-or-glow-or-...>",
  "source": {
    "device": "<source-manifest-name>",
    "slot": <int>,            // OR
    "oid":  "<dotted>",       // OR
    "path": "<dotted>",
    "obj_id": <int>
  }
}
```

The virtual frame manifest extends ADR-0022's shape with an
`objects` array per virtual slot (replacing the `dm: Model@SwRev`
reference). Standard cross-protocol address tokens — `slot` / `oid` /
`path` / `obj_id` — disambiguate the source.

Used for:

- LogicalView per operator (curated subset across the plant).
- Cross-vendor virtual device (e.g. a "Console" that fans audio to a
  Lawo mc² and SDI metadata to an EVS Neuron via one Ember+ endpoint).
- Test rigs that pin specific object combinations independent of
  underlying hardware.

### One registry, two modes

dhs exposes both mirror and virtual entries from the same federation
registry. External controllers see them indistinguishable from real
devices — only the manifest's `kind` (`mirror` | `virtual`) tells the
federation layer how to route requests.

## Consequences

- The "device" abstraction in ADR-0022 stays per-physical-unit. The
  virtual entity is a separate kind, not a sub-type of Device.
- Federation routing is the only layer that knows the mode; consumer
  plugins never see it.
- Virtual frames REQUIRE the manifest to be the single source of
  truth — no legacy `--tree` path can compose this.

## Forbidden

- Modelling a virtual frame by editing a real device's DM. DMs are
  per-physical-card schemas; never mutate to fake federation.
- Per-protocol mirror logic in the consumer plugin (it lives in
  federation only).
- Restating any of this in CLAUDE.md, per-connector CLAUDE.md, agent
  files, or memory — those point at this ADR per ADR-0015.

## Revisions

- 2026-05-14 — initial — yboujraf
