# ADR-0015 — Single source of truth per concern

Status: accepted
Date: 2026-10-18

## Context

When the same architectural rule lives in several files (root
`CLAUDE.md`, `AGENTS.md`, per-protocol `CLAUDE.md`, ad-hoc docs, agent
memory), the copies drift and contradict each other. A reader of one
file gets one rule, a reader of another gets a different one, and the
ground truth becomes unrecoverable.

The same drift happens inside one ADR when its text grows by appendix:
a decision at the top, dated amendments and revision trailers below it,
and a reader who has to merge them in their head to know what binds
today.

## Decision

Every architectural concern has **exactly one** source of truth, and
that source holds **one current text**.

### Where rules live

| Concern | Source of truth |
| --- | --- |
| A cross-cutting decision | one ADR under `docs/adr/`, listed once in the index of [`README.md`](README.md); this ADR keeps no copy of that index |
| Per-protocol wire facts | `internal/<proto>/CLAUDE.md` |
| Per-protocol audit answers | `internal/<proto>/COMPLIANCE.md` (ADR-0008) |
| The connector contract | `docs/CONNECTOR.md`, which links the ADRs and never restates them |

### Permitted content per file

| File | Permitted |
| --- | --- |
| `docs/adr/NNNN-*.md` | the **only** place a decision is written |
| `docs/adr/README.md` | the index, the status table, how to add an ADR; no rule text |
| `docs/CONNECTOR.md` | summary table + links to ADRs |
| `CLAUDE.md` (root) | links to ADRs; never restates |
| `AGENTS.md` | session bootstrap; links to ADRs; never restates |
| `internal/<proto>/CLAUDE.md` | wire facts only (codec, frame format, device quirks) |
| `internal/<proto>/COMPLIANCE.md` | audit answers (ADR-0008) |
| `README.md` | one-line public summary |
| agent memory | nothing load-bearing: a rule that constrains the work is written into one of the files above |

### Lifecycle of an ADR

- Two statuses: `proposed` (under discussion) and `accepted` (binding).
  Acceptance is the codeowner's, per ADR-0014. An ADR never goes back
  from `accepted` to `proposed` and is never deleted.
- An accepted ADR is **the current decision for its concern**. When it
  is reviewed, it is **rewritten in place as one clean text**: errata,
  clarifications and extensions that keep the decision. The header
  carries the date of the current text; the reason for the change is in
  the commit and the pull request; git history holds the earlier
  versions. No "Revisions" trailer, no "Amendment" section, no dated
  appendix of any kind, no "living document" preamble.
- A **reversal** of an accepted decision is **always a new ADR**
  (codeowner rule). The ADR it reverses is rewritten in the same unit to
  drop the reversed text and to point at the new ADR, so no two ADRs
  bind the same point differently. A **new concern** also gets a new
  ADR.
- There is no `superseded` or `deprecated` status: the reversed ADR
  keeps its number and the decisions it still holds; what it lost
  points forward.
- Header shape of every ADR: title, `Status:`, `Date:`, then `Context`,
  `Decision`, `Consequences`, and `Forbidden` where useful.

### Enforcement

When reviewing any change:

1. Find the rule in question.
2. If it appears in more than one file, that is a violation: keep the
   ADR, replace the copy with a link.
3. If a document contradicts an ADR, the ADR wins; fix the document.
4. If two ADRs contradict each other, that is a defect: open a tracker
   issue. If one of them is simply wrong, rewrite it. If the decision
   itself changed, the new decision is a new ADR and the old one is
   rewritten to drop the reversed clause. Codeowner approval per
   ADR-0014 either way. Never a third ADR that only arbitrates.

## Consequences

- One file to change when a rule changes, and one text to read to know
  the rule.
- Cross-file contradictions are impossible by construction; in-file
  contradictions are impossible because there is no appendix to
  contradict the body.
- Onboarding, human or agent, reads the ADR index, not scattered docs
  and not memory.

## Forbidden

- Restating an ADR's content in another document or in agent memory.
- Appending to an ADR: revision trailers, amendment sections, dated
  notes. Rewrite the text instead.
- Reversing an accepted decision by rewriting it in place. A reversal is
  a new ADR.
- Flipping a status backwards, deleting an ADR, or introducing a
  `superseded` / `deprecated` / `rejected` status.
- A third ADR written to reconcile two that contradict each other.
