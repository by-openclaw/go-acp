# `StreamDescription` fixture — APP tag 12

Spec page 93 — Ember+ Documentation v2.50 §5 "The DTD".

`StreamDescription` carries the stream's format + per-element offset
information when several `Parameter`s share a single `streamIdentifier`
in CollectionAggregate mode. Without a StreamDescription, the
`streamIdentifier` is implicitly exclusive (one Parameter per stream).

## Status

**Scaffolded; capture pending live device (#62)**. TinyEmber+ /
TinyEmberPlusRouter — the providers we test on loopback — do **not**
emit StreamDescription. Recapture requires a live Lawo / DHD / Riedel
provider that publishes a CollectionAggregate stream.

## What to produce when capturing

- `capture.pcapng` — single S101 frame carrying a `Parameter` (or
  `QualifiedParameter`) whose `streamIdentifier` is shared with at
  least one other `Parameter`, and whose `streamDescriptor` field
  resolves to an APP-12 `StreamDescription` element.
- `tshark.tree` — frozen `tshark -V` output of the same frame, used
  by the CI parity test at `tests/unit/fixture_parity/` to assert
  the dissector decodes every field of APP-12.
- `README.md` — this file, updated with the device + the slimming
  steps used.

## Capture recipe

Once a CollectionAggregate-publishing provider is reachable:

```sh
# 1. Start the provider on its native port (varies by vendor).
# 2. From dhs-tools (.105):
tshark -i any -f "host <provider-ip> and tcp port <emberplus-port>" \
       -w /tmp/streamdesc-raw.pcapng
# 3. From the consumer side:
bin/dhs consumer emberplus walk <provider-ip>
bin/dhs consumer emberplus stream <provider-ip>  # exits after one frame
# 4. Slim to the QualifiedParameter + Stream frames, then save:
editcap -F pcapng /tmp/streamdesc-raw.pcapng capture.pcapng <frame#s>
tshark -V -r capture.pcapng > tshark.tree
```

Until that capture lands, the CI parity test treats this fixture as
soft-skipped via the `not covered (TinyEmber+ / TinyEmberPlusRouter
gap)` table in the parent [README.md](../README.md).
