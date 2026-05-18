# `QualifiedFunction` fixture — APP tag 20

Spec page 91 — Ember+ Documentation v2.50 §5 "The DTD".

`QualifiedFunction` is the absolute-path variant of `Function` (APP
19): a function declaration that carries its full `path[]` rather
than relying on the consumer's walk position.

## Status

**Scaffolded; capture pending live device (#62)**. TinyEmber+ /
TinyEmberPlusRouter emit only `Function` wrapped in a `QualifiedNode`
path — never the standalone `QualifiedFunction`. Recapture requires
a Lawo / DHD / Riedel provider whose firmware ships functions at
absolute paths.

## What to produce when capturing

- `capture.pcapng` — single S101 frame carrying a `QualifiedFunction`
  with at least one path segment + a `description` + an `arguments`
  array of `TupleItemDescription` (paired fixture, see
  `tuple_item_description/`).
- `tshark.tree` — frozen `tshark -V` output.
- `README.md` — this file, updated with the device + slimming steps.

## Capture recipe

See [`stream_description/README.md`](../stream_description/README.md)
for the general capture flow. Filter the slim window to the GetDirectory
reply that carries the QualifiedFunction element.
