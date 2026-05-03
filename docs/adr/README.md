# Architecture Decision Records (ADR)

This directory holds the binding architectural decisions for **dhs**
(Device Hub Systems). Every rule that crosses protocols or constrains
how connectors are built lives here.

## Rules

1. **Single source of truth** — every architectural rule has exactly one
   ADR. Other docs (`CLAUDE.md`, `agents.md`, `docs/CONNECTOR.md`,
   per-protocol `CLAUDE.md`) reference an ADR by number; they never
   restate it. See ADR-0015.
2. **Permanent acceptance** — once an ADR is `accepted`, it is binding
   forever. There is no `superseded` status; agents that disagree must
   propose a NEW ADR for a NEW concern, never replace an existing one.
3. **Workflow** — every ADR proposal follows: GitHub issue → branch →
   ADR file + tests where applicable → PR → CI green → `@yboujraf`
   codeowner approval → merge. See ADR-0014.

## Status flow

| Status | Meaning |
|---|---|
| `proposed` | under discussion |
| `accepted` | binding, permanent |

There is no `superseded` / `deprecated` / `rejected-after-acceptance`.

## Index

| ID | Title | Status |
|---|---|---|
| [0001](0001-per-connector-binary-and-repo.md) | Per-connector binary and own repo | accepted |
| [0002](0002-canonical-cli-verbs-flags.md) | Canonical CLI verbs and flags per role | accepted |
| [0003](0003-license-jwt-eddsa-vault-transit.md) | License JWT-EdDSA signed by Vault Transit | accepted |
| [0004](0004-trial-fingerprint-binding.md) | Trial license fingerprint binding | accepted |
| [0005](0005-dep-policy.md) | External dependency policy + accepted-library manifest | accepted |
| [0006](0006-codec-stdlib-only.md) | Codec packages stdlib-only forever | accepted |
| [0007](0007-ensure-verb.md) | `ensure --state --check` verb contract | accepted |
| [0008](0008-compliance-audit-pack.md) | Per-connector COMPLIANCE.md template | accepted |
| [0009](0009-plugin-supervisor.md) | Plugin supervisor: `hashicorp/go-plugin` | accepted |
| [0010](0010-vault-internal-only.md) | Vault internal-only — never public | accepted |
| [0011](0011-odoo-record-of-truth.md) | Odoo as customer + license + asset record-of-truth | accepted |
| [0012](0012-shared-discovery-layer.md) | Shared discovery layer (mDNS / unicast / peer-list) | accepted |
| [0013](0013-no-commit-churn.md) | One approved unit = one commit | accepted |
| [0014](0014-issue-tracking-discipline.md) | Issue tracking discipline | accepted |
| [0015](0015-single-source-of-truth.md) | Single source of truth per concern | accepted |
| [0016](0016-multi-os-support.md) | Multi-OS support per connector | accepted |
| 0017 | File-header template | parked |
| [0018](0018-info-verb-build-identity.md) | `info` verb build identity | accepted |

## ADR-0017 parking note

ADR-0017 (file-header template) is parked pending two inputs from the
project owner:

1. license model pick (MIT / proprietary / BSL / hybrid)
2. corporate identity values (legal name, address, phone, email, URL)

The template structure, per-file-type variants, and tooling skeletons
are pre-staged in agent memory (`project_dhs_file_headers`); the ADR
file lands when the two inputs are supplied.

## Adding a new ADR

1. Open a tracking issue with labels `documentation` + relevant
   `proto:*` if applicable.
2. Branch off `main` named `docs/adr-NNNN-<short-title>`.
3. Copy the template skeleton (see existing ADRs for shape):
   `# ADR-NNNN — Title` / `Status` / `Context` / `Decision` /
   `Consequences`.
4. Open a PR. CI must be green; `@yboujraf` codeowner approval
   required to merge.
5. Update this index.
