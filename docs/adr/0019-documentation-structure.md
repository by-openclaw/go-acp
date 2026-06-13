# ADR-0019 — documentation structure (split-aware)

Status: accepted

## Context

The project today is a monorepo holding many connectors plus the
shared rule documents (ADRs, connector contract, canonical schema).
Per ADR-0001, every connector eventually splits into its own Go module
and its own repository. The doc set must work both before and after
that split, with no rewrite — only the path container moves.

Without a binding rule, every new doc lands wherever it was authored,
agents and contributors waste time hunting for whether a topic is
"global" or "per-connector", and the split becomes painful because
docs that should travel with a connector are scattered into shared
folders.

## Decision

Two tiers. Each doc belongs to exactly one.

### Tier 1 — Per-connector docs

Live with the connector. Move with it on the per-repo split.

Today (monorepo):

```text
internal/<proto>/
├── CLAUDE.md                  atomic per-protocol wire facts (binding for that protocol)
├── COMPLIANCE.md              audit pack per ADR-0008 (ports, GDPR, NIS2, ISO 27001, CRA, etc.)
├── runbook-multi-os.md        per-OS install / dependency runbook per ADR-0016
├── docs/
│   ├── README.md              one-page overview
│   ├── consumer.md            consumer-side CLI walkthrough (only if connector implements consumer role)
│   ├── producer.md            producer-side CLI walkthrough (only if connector implements producer role)
│   └── registry.md            registry-side walkthrough (only if connector implements registry role)
└── assets/                    spec PDFs + vendor tools (binary, often LFS)
```

Tomorrow (each connector in its own repo per ADR-0001):

```text
dhs-<proto>/
├── CLAUDE.md
├── COMPLIANCE.md
├── runbook-multi-os.md
├── docs/{README,consumer,producer,registry}.md
└── assets/
```

Same shape, root-level. The repo path moves; the rule does not.

### Tier 2 — Shared meta docs

Cross-cutting rules and references that every connector implements
identically.

Today (monorepo):

```text
docs/
├── adr/                       Architecture Decision Records (binding)
├── CONNECTOR.md               connector contract (collated from ADRs per ADR-0015)
├── ARCHITECTURE.md            cross-cutting architecture overview
├── VISION.md                  long-term Fabric vision (reference, not commitment)
├── wireshark.md               Wireshark dissector install + filter guide
├── protocols/
│   ├── schema.md              canonical-tree contract (every connector emits this shape)
│   ├── use-cases.md
│   └── elements/*.md          per-element-type schema docs (parameter, matrix, node, ...)
├── deployment/                docker-compose + Grafana / Prometheus / Loki stack
└── examples/                  reserved (today empty after refactor)
```

Tomorrow (per-connector repo split per ADR-0001):

```text
dhs-spec/                      shared module every connector pins a version of
└── (same docs/ layout as above)
```

Connector repos either vendor a copy of the relevant ADR set at a pinned
version OR import / submodule the shared spec module. They never
duplicate ADR text — single source of truth (ADR-0015).

## Required per-connector doc set

Every connector MUST ship at minimum:

| File | Required | Authoritative for |
| --- | --- | --- |
| `CLAUDE.md` | yes | wire format, command catalogue, quirks, "what NOT to do" |
| `COMPLIANCE.md` | yes (ADR-0008) | ports, firewall, GDPR, NIS2, ISO 27001, CRA |
| `runbook-multi-os.md` | yes (ADR-0016) | per-OS install + dependency steps |
| `docs/README.md` | yes | one-page overview |
| `docs/consumer.md` | only if connector implements consumer role | CLI walkthrough |
| `docs/producer.md` | only if connector implements producer role | CLI walkthrough |
| `docs/registry.md` | only if connector implements registry role | CLI walkthrough |

## Reference material vs documentation

| Item | Where |
| --- | --- |
| Vendor / SDO spec PDFs | `internal/<proto>/assets/` (binary; LFS where size justifies) |
| Vendor tools (TS emulators, viewers) | `internal/<proto>/assets/` (binary) |
| Project's own product spec (kept outside the repo to save LFS quota) | not committed; agents reference by description in memory |
| Markdown analysis of a vendor / spec quirk | `internal/<proto>/docs/references.md` |
| Cross-cutting reference (e.g. wire-format primer used by multiple connectors) | `docs/<topic>.md` |
| Canonical-tree element schema (cross-cutting) | `docs/protocols/elements/<element>.md` |

Reference markdown that is genuinely cross-cutting lives at
`docs/<topic>.md` (or `docs/protocols/elements/`); reference markdown
specific to one connector lives under `internal/<proto>/docs/`.

## Forbidden

- Restating ADR rules in `CLAUDE.md`, `AGENTS.md`, `README.md`, or any
  per-protocol doc (ADR-0015).
- Putting per-connector content (wire facts, command catalogue,
  per-OS install) under `docs/`. It must live under `internal/<proto>/`.
- Putting cross-cutting rules (codec stdlib-only, plugin supervisor,
  CLI verb table) under `internal/<proto>/`. Those are ADRs, lived at
  `docs/adr/`.
- Adding a new top-level `docs/` subfolder without an ADR justification.

## Consequences

- New contributors find a doc in O(1): "is this fact protocol-specific?
  → `internal/<proto>/`. Else → `docs/`."
- Per-repo split (per ADR-0001) is mechanical: take the connector's
  subtree, drop it into a new repo, done. No grep-and-extract pass
  through `docs/`.
- Agents working on a single connector load `internal/<proto>/CLAUDE.md`
  plus the relevant ADRs and have everything they need without scanning
  the whole monorepo.
- The shared meta-doc set (Tier 2) becomes a versioned dependency
  after the split, just like the codebase modules.
