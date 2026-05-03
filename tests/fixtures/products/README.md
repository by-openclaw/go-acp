# Per-product DM fixture library

The full layout, naming, and `meta.json` schema for this directory are
defined in [ADR-0020](../../../docs/adr/0020-capture-and-fixture-layout.md)
(Bucket 3 — Per-product DM library).

## Path shape

```text
tests/fixtures/products/<manufacturer>/<product>/<proto>/<role>/<version>/
├── meta.json          provenance + capture context
├── wire.jsonl         wire-trace per ADR-0021
├── tree.json          canonical tree
└── capture.pcapng     optional OS-socket capture
```

`<role>` ∈ `{consumer, producer, registry}` per ADR-0001.

## CHANGELOG

Each `<manufacturer>/<product>/<proto>/<role>/` folder ships a
`CHANGELOG.md` summarising DM evolution across versions, in
[Keep a Changelog](https://keepachangelog.com/) format.

## Generating a fixture

Once the `replay` verb (ADR-0002 + ADR-0021) lands and the `extract`
verb is implemented:

```text
dhs consumer <proto> extract <host> \
    --role consumer \
    --out tests/fixtures/products/<manufacturer>/<product>/<proto>/consumer/<version>/
```

For now, fixtures are populated manually — capture with `--capture`,
hand-stamp `meta.json` per the schema in ADR-0020.
