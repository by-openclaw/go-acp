# `Template` fixture — APP tag 24

Spec page 93 — Ember+ Documentation v2.50 §5 "The DTD".

## Coverage gap (not captured from this repo's producer)

`Template` is the **relative-OID** form of a template — used when a
template appears as a child inside a parent element rather than at
the root. The sibling fixture `qualified_template/` covers the
qualified-OID variant (APP 25) which IS emitted by this repo's
producer when `Export.Templates[]` is populated.

The relative-form Template (APP 24) is uncommon in the wild:

- TinyEmber+ does not emit templates at all.
- Lawo / DHD / Riedel large-scale-router providers DO emit templates,
  generally in the qualified form (APP 25) for the same reason our
  producer does.
- Our producer's encoder (`internal/emberplus/provider/encoder.go::
  encodeQualifiedTemplate`) only emits APP 25; it has no code path
  for APP 24.

## What populating this fixture requires

Either:

1. A real Lawo / DHD / Riedel provider on the lab subnet emitting
   relative-form templates, with `dhs consumer emberplus walk
   <host> --capture <out.jsonl>` to capture and the same
   jsonl→pcap synthesis path as the sibling fixtures.
2. A new encoder branch (`encodeTemplateInChild`) added to our
   provider, plus a `coverage-tree.json` that nests templates as
   children rather than declaring them at root.

Either path is a separate effort tracked against this fixture
directory.

## Status

Scaffold only — captures pending. The strict-spec R18 / R12 paths
are unaffected; QualifiedTemplate (APP 25) coverage in the sibling
fixture is sufficient for dissector regression testing.
