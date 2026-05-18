# `TupleItemDescription` fixture — APP tag 21

Spec page 91 — Ember+ Documentation v2.50 §5 "The DTD".

`TupleItemDescription` declares one argument or one result of a
`Function`/`QualifiedFunction`: name + type code (Integer / Real /
String / Boolean / Octets). Every position in `Function.arguments[]`
and `Function.result[]` is a `TupleItemDescription`.

## Status

**Scaffolded; capture pending live device (#62)**. TinyEmber+
exposes `Function` elements with no `arguments` metadata — the
function ships as a bare invocation point, not a typed signature.
Recapture requires a Lawo / DHD / Riedel provider whose functions
publish typed arg lists.

## What to produce when capturing

- `capture.pcapng` — single S101 frame carrying a `Function` (or
  `QualifiedFunction`) whose `arguments[]` array contains at least
  two `TupleItemDescription` entries (mix of types is preferable for
  decoder coverage).
- `tshark.tree` — frozen `tshark -V` output.

## Capture recipe

Same general flow as [`stream_description/README.md`](../stream_description/README.md).
Slim to the GetDirectory reply containing the typed function.
