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

### No superseding

Once an ADR is `accepted`, it is permanent. If a new concern arises,
write a new ADR for the new concern. Never overwrite or supersede.
This prevents the slow drift that "supersede" patterns enable.

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
- Modifying an ADR after it is `accepted` (status flip is forbidden;
  the rule itself can be amended only via a new ADR documenting a
  new concern).
- Introducing `superseded` / `deprecated` / `rejected-after-acceptance`
  status — the only valid states are `proposed` and `accepted`.
