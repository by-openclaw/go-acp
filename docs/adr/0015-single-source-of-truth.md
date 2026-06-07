# ADR-0015 — Single source of truth per concern

Status: accepted

## Context

When the same architectural rule lives in multiple files (root
`CLAUDE.md`, `agents.md`, per-protocol `CLAUDE.md`, ad-hoc docs),
they drift over time and start contradicting each other. Agents
(human or AI) reading one file get one rule; reading another get a
different rule; and the ground truth becomes unrecoverable.

## Decision

Every architectural concern has **exactly one** source of truth.
Other docs reference it by path/number; they never restate it.

### Where rules live

| Concern | Single source of truth |
|---|---|
| Per-connector binary + repo | ADR-0001 |
| CLI verbs + flags | ADR-0002 |
| License signing model | ADR-0003 |
| Trial fingerprint | ADR-0004 |
| Dep policy | ADR-0005 + `0005-deps.json` |
| Codec stdlib-only | ADR-0006 |
| `ensure` verb | ADR-0007 |
| Compliance audit pack | ADR-0008 |
| Plugin supervisor | ADR-0009 |
| Vault internal-only | ADR-0010 |
| Odoo record-of-truth | ADR-0011 |
| Shared discovery layer | ADR-0012 |
| No commit churn | ADR-0013 |
| Issue tracking | ADR-0014 |
| **Single source of truth rule** | **ADR-0015** (this one) |
| Multi-OS support | ADR-0016 |
| File-header template | ADR-0017 (parked) |
| `info` verb build identity | ADR-0018 |
| Documentation structure | ADR-0019 |
| Capture + fixture layout | ADR-0020 |
| Wire-trace JSONL contract | ADR-0021 |
| Card data model | ADR-0022 |
| Matrix entity | ADR-0023 |
| Federation (mirror / virtual frame) | ADR-0024 |
| Per-connector definition of done | ADR-0025 |
| Agent communication style | ADR-0026 |
| Workflow contract (DOD windows) | ADR-0027 |
| ADR amendment / lifecycle | ADR-0015 (this one, §Amendment policy) |
| Per-protocol wire facts | `internal/<proto>/CLAUDE.md` |
| Per-protocol audit answers | `internal/<proto>/COMPLIANCE.md` |
| Customer + license + asset records | Odoo (per ADR-0011) |
| Connector contract assembly | `docs/CONNECTOR.md` (collates ADR references; never duplicates) |

### Permitted content per file

| File | What is permitted |
|---|---|
| `docs/adr/NNNN-*.md` | the **only** place the decision is written |
| `docs/CONNECTOR.md` | summary table + links to ADRs; never restates ADR content |
| `CLAUDE.md` (root) | links to ADRs; never restates |
| `agents.md` | session bootstrap; links to ADRs; never restates |
| `internal/<proto>/CLAUDE.md` | wire facts only (codec, frame format, device quirks); never restates architectural rules |
| `internal/<proto>/COMPLIANCE.md` | audit answers (per ADR-0008); never restates architectural rules |
| `README.md` | one-line public summary; never restates |

### Amendment policy

Once an ADR is `accepted`, its **decision** is stable — but the file is
**amendable in place** via a dated entry in a `## Revisions` trailer, for:

- **errata** — typos, stale indexes, broken cross-references, wrong citations;
- **clarifications** that do not change the decision;
- **living-document additions** that extend (never reverse) the decision.

A **substantive reversal** of an accepted decision still requires a **new ADR**
documenting the new concern. ADRs are never deleted; there is no `superseded`
status. Every amendment requires `@yboujraf` approval per ADR-0014.

This legitimizes the existing living-document ADRs (0023 / 0025 / 0026) and lets
defects be corrected without spawning a clarifying ADR per typo. (Replaces the
former "accepted = permanent, never modify" rule — see Revisions.)

### Enforcement

When reviewing any change:

1. Find the rule in question.
2. If it appears in more than one file, that's a violation. Fix by
   keeping the ADR and replacing the duplicate with a link.
3. If a doc contradicts an ADR, the ADR wins; fix the doc.
4. If two ADRs contradict, that's a defect requiring a tracker
   issue and a third ADR clarifying — but only after explicit
   `@yboujraf` review per ADR-0014.

## Consequences

- One file to update when a rule changes.
- Cross-file contradictions become impossible by construction.
- Onboarding agents (human or AI) read the ADR index, not scattered
  docs.
- Old terminal/chat memory becomes irrelevant — the ADRs are the
  contract.

## Forbidden

- Restating an ADR's content in another doc.
- **Reversing** an accepted ADR's decision by in-place edit — a reversal
  requires a new ADR. (Errata / clarifications / living-document additions
  via a dated `## Revisions` entry are permitted; see Amendment policy.)
- Flipping an ADR's status backwards (e.g. `accepted` → `proposed`).
- Introducing `superseded` / `deprecated` / `rejected-after-acceptance`
  status — the only valid states are `proposed` and `accepted`.

## Revisions

- 2026-06-07 — Amendment policy added: accepted ADRs may be amended in
  place via a dated Revisions entry for errata / clarification /
  living-document additions; decision reversals still require a new ADR.
  Replaces the former absolute "accepted = permanent, never modify" rule,
  which blocked even typo/stale-index/cross-reference fixes and contradicted
  the living-document pattern already used by ADR-0023/0025/0026. Also
  completed this ADR's stale "Where rules live" index (was 0001–0018; now
  0001–0027). Authorized by `@yboujraf`. — by-rune
