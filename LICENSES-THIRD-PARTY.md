# Third-Party Licenses

The authoritative inventory of every external Go module used by `dhs`
lives at [`docs/adr/0005-deps.json`](docs/adr/0005-deps.json) (per
**ADR-0005**). Each entry records owner, license, scope, and the ADR
that authorised the dependency.

This file is intentionally short — it points to the manifest rather than
restating it (per **ADR-0015**, single source of truth).

## Stdlib

| Library             | Version                        | License      | Owner  |
|---------------------|--------------------------------|--------------|--------|
| Go standard library | the version pinned in `go.mod` | BSD-3-Clause | Google |

## Codec layer

Codec packages under `internal/<proto>/codec/` import only the Go
standard library (per **ADR-0006**). They are lift-to-own-repo ready
and never carry a transitive dependency on any module listed in
`docs/adr/0005-deps.json`.

## Adding a dependency

Per ADR-0005:

1. Open an issue describing the need and the alternatives considered
2. Add an entry to `docs/adr/0005-deps.json` (id, module, url, owner,
   license, scope, reason, adr_refs)
3. Verify CVE history, transitive count, and license compatibility
4. PR review by `@yboujraf`

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
