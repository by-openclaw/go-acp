# `QualifiedTemplate` fixture — APP tag 25

Spec page 93 — Ember+ Documentation v2.50 §5 "The DTD".

`QualifiedTemplate` is the absolute-path variant of `Template`
(APP 24): a template declaration that carries its own `path[]` rather
than relying on the consumer's walk position. Pairs with the
[`template/`](../template/) fixture.

## Status

**Scaffolded; capture pending live device (#62)**. Same TinyEmber+
gap as `Template`: recapture requires a Lawo / DHD / Riedel provider
that emits `QualifiedTemplate` elements at absolute paths.

## What to produce when capturing

- `capture.pcapng` — single S101 frame carrying a `QualifiedTemplate`
  plus one referencing element (`Parameter` / `QualifiedParameter`
  with `templateReference` resolved to the QualifiedTemplate's path).
- `tshark.tree` — frozen `tshark -V` output.

## Capture recipe

Same general flow as [`stream_description/README.md`](../stream_description/README.md).
Slim to the GetDirectory reply that carries the QualifiedTemplate.
