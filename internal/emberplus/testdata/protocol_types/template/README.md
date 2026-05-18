# `Template` fixture — APP tag 24

Spec page 93 — Ember+ Documentation v2.50 §5 "The DTD".

`Template` carries a reusable Node / Parameter / Matrix declaration
that other elements reference via `templateReference`. Templates let
a provider declare one prototype and point many concrete elements at
it, keeping the wire payload bounded on devices with many similar
endpoints.

## Status

**Scaffolded; capture pending live device (#62)**. TinyEmber+ does
not expose templates — every element is a fully-described instance.
Recapture requires a Lawo / DHD / Riedel provider that emits
`Template` elements (typically large-scale routers / DSPs).

## What to produce when capturing

- `capture.pcapng` — single S101 frame carrying a `Template` plus at
  least one referencing element (`Parameter` / `QualifiedParameter`
  with `templateReference` populated). Both ends of the
  reference exercise the dissector path that resolves pointers.
- `tshark.tree` — frozen `tshark -V` output.

## Capture recipe

Same general flow as [`stream_description/README.md`](../stream_description/README.md).
Slim to the GetDirectory reply that carries the template definition.
