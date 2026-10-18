# Architecture Decision Records (ADR)

This directory holds the binding architectural decisions for **dhs**
(Device Hub Systems). Every rule that crosses protocols or constrains
how connectors are built lives here.

## Rules

1. **Single source of truth** — one concern, one ADR, one current text.
   Other documents link an ADR by number and never restate it. The
   lifecycle of an ADR (rewrite in place, dated header, no appendix) is
   ADR-0015's.
2. **Workflow** — every ADR change follows ADR-0014: issue → branch →
   PR → CI green → `@yboujraf` codeowner approval → merge.

## Status

| Status | Meaning |
| --- | --- |
| `proposed` | under discussion |
| `accepted` | binding |

The **Implemented** column below is not a status. It records, as of
the date in this file's last change (2026-10-18), whether the shipped
product does what the ADR says: `yes`, `partial`, `no`. It is reviewed
when an ADR is rewritten.

## Index

| ID | Title | Status | Implemented |
| --- | --- | --- | --- |
| [0001](0001-per-connector-binary-and-repo.md) | Per-connector binary and own repo | accepted | no — one binary, one repo today |
| [0002](0002-canonical-cli-verbs-flags.md) | Canonical CLI verbs and flags per role | accepted | partial — `replay`, `license *` absent |
| [0003](0003-license-jwt-eddsa-vault-transit.md) | License JWT-EdDSA signed by Vault Transit | accepted | no |
| [0004](0004-trial-fingerprint-binding.md) | Trial license fingerprint binding | accepted | no |
| [0005](0005-dep-policy.md) | External dependency policy + accepted-library manifest | accepted | yes |
| [0006](0006-codec-stdlib-only.md) | Codec packages stdlib-only forever | accepted | yes |
| [0007](0007-ensure-verb.md) | `ensure --state --check` verb contract | accepted | yes |
| [0008](0008-compliance-audit-pack.md) | Per-connector COMPLIANCE.md template | accepted | no |
| [0009](0009-plugin-supervisor.md) | Plugin supervisor: `hashicorp/go-plugin` | accepted | no — compile-time registries |
| [0010](0010-vault-internal-only.md) | Vault internal-only — never public | accepted | no |
| [0011](0011-odoo-record-of-truth.md) | Odoo as customer + license + asset record-of-truth | accepted | no — Odoo parked 2026-10-15 |
| [0012](0012-shared-discovery-layer.md) | Shared discovery layer (mDNS / unicast / peer-list) | accepted | no — `discover` exists, the shared package does not |
| [0013](0013-no-commit-churn.md) | One approved unit = one commit | accepted | yes |
| [0014](0014-issue-tracking-discipline.md) | Issue tracking discipline | accepted | yes |
| [0015](0015-single-source-of-truth.md) | Single source of truth per concern | accepted | yes |
| [0016](0016-multi-os-support.md) | Multi-OS support per connector | accepted | partial — CI matrix yes, `runbook-multi-os.md` none |
| 0017 | File-header template | parked | — |
| [0018](0018-info-verb-build-identity.md) | `info` verb build identity | accepted | partial — ldflags yes, schema fields missing |
| [0019](0019-documentation-structure.md) | Documentation structure (split-aware) | accepted | partial — `COMPLIANCE.md`, `runbook-multi-os.md` missing |
| [0020](0020-capture-and-fixture-layout.md) | Capture and fixture file layout | accepted | partial — no `scenarios/`, `tests/fixtures/<proto>/` present |
| [0021](0021-wire-trace-jsonl-contract.md) | Wire-trace JSONL contract + replay semantics | accepted | partial — `validate` yes, `replay` no |
| [0022](0022-card-data-model.md) | Card data model — Device/Frame/Slot/Card/DM | accepted | partial — manifest keyed by name and by IP |
| [0023](0023-matrix-entity.md) | Matrix entity — matrix_id/level_id/size/behavior | accepted | yes |
| [0024](0024-federation-mirror-and-virtual-frame.md) | Federation — mirror frame vs virtual frame | accepted | no |
| [0025](0025-per-connector-definition-of-done.md) | Per-connector definition of done (six deliverables) | accepted | yes |
| [0026](0026-agent-communication-style.md) | Agent communication style | accepted | yes |
| [0027](0027-workflow-contract-dod-windows.md) | Workflow contract during DOD windows | accepted | yes |
| [0028](0028-artifact-layout.md) | Artifact layout — one deterministic home per artifact, keyed by IP | proposed | partial |
| [0029](0029-ci-event-step-matrix.md) | CI event→step matrix — no retries, single-pass coverage | proposed | yes |
| [0030](0030-neutral-device-monitor.md) | Neutral device monitor — actor-per-device, event-driven, per-request interval scheduler | proposed | partial |
| [0031](0031-event-model.md) | Event model — notification/trap/alarm, schema vs stream, OID+kind spine | proposed | yes |
| [0032](0032-transport-hardening.md) | Transport hardening — fuzzed decoders, gateway as protective proxy | proposed | partial — fuzz targets in 7 codecs |
| [0033](0033-alarm-template.md) | Alarm template — thresholds as per-model data, severity ladder, hysteresis, evaluated at the edge | proposed | yes |
| [0034](0034-oracle-testing.md) | Oracles — an independent implementation, run as a process, in both roles | proposed | partial — loopback oracle tests not in CI |

## ADR-0017 parking note

ADR-0017 (file-header template) is parked pending two inputs from the
project owner: the license model (MIT / proprietary / BSL / hybrid) and
the corporate identity values (legal name, address, phone, email, URL).
The file lands when both are supplied.

## Adding a new ADR

1. Open a tracking issue with labels `documentation` + relevant
   `proto:*` if applicable.
2. Branch off `main` named `docs/adr-NNNN-<short-title>`.
3. Write the file with the header shape of ADR-0015: `# ADR-NNNN — Title`,
   `Status:`, `Date:`, then `Context`, `Decision`, `Consequences`,
   `Forbidden` where useful. One clean text, no appendix.
4. Open a PR. CI must be green; `@yboujraf` codeowner approval
   required to merge.
5. Update this index, including the Implemented column.
