# ADR-0027 — Workflow contract during DOD windows

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

ADR-0014 (issue tracking discipline) defines the canonical workflow
chain: issue → branch → tests → PR → CI green → `@yboujraf` codeowner
approval → merge. That chain works well for steady-state contributions
where one issue resolves in days.

It fails during **DOD windows** — multi-week pushes to drive one
connector to the six-deliverable Definition of Done in ADR-0025. The
failure mode is concrete: opening a PR per atomic commit during a DOD
window produces a stream of half-shipped, partly-tested PRs the
operator has to triage individually, none of which clears the
ADR-0025 gate alone. The 12-hour Ember+ session of 2026-05-17 ended
with multiple open PRs against `main`, none of which advanced the
overall connector beyond `partial` on ADR-0025's six deliverables.

The fix is not to suspend ADR-0014 — it is to bind a tighter contract
that overrides only the timing of the PR-opening step, leaving every
other clause (codeowner approval, CI green, conventional commits) in
place.

## Decision

When a connector is in a DOD window — meaning at least one of the six
ADR-0025 deliverables is `missing` or `partial` — the rules below
override the matching steps of ADR-0014. Steps not mentioned here
remain exactly as ADR-0014 specifies.

### Autonomous without per-step approval

- Branch creation off `main`
- Local edits, builds, unit tests
- Atomic commit per tracked issue, conventional-commit subject
- Push branch to `origin` for visibility
- Reading code, running probes, running integration tests on the
  testbed

### Explicit operator approval required (`"go" / "approuved" / "ok"`)

- `gh pr create` — opening any PR on GitHub
- `gh pr merge` — merging any PR
- Closing any tracked issue
- Changing the scope of an issue
- Any operation that touches shared infrastructure beyond the
  feature branch

### One commit per tracked issue

ADR-0013 already requires "one approved unit = one commit." During a
DOD window this is tightened: each atomic commit closes the scope of
**one** tracked issue. Bundling two issues into one commit is
forbidden — the bundle blurs which acceptance criteria applied.

### Status reports are DOD-based

The status of a DOD window is reported against the six ADR-0025
deliverables, with evidence. Green unit tests are not progress. A
commit landing on the feature branch is progress only when it
advances a specific deliverable on a specific connector.

Report format: one line per atomic commit landing.

> feat(emberplus/provider): stream idle-TTL eviction — commit
> `abc1234` — R9 #472 done locally on `feat/emberplus-stream-idle-ttl-472`

No tables of alternatives. No "want me to do X next?" follow-ups.

### No PR until DOD complete

No `gh pr create` against `main` while a DOD deliverable on the
target connector remains `missing` or `partial`. Atomic commits
accumulate on the feature branches; the operator can pull / inspect
at any time. A single connector-wide PR opens at the end of the DOD
window per `feedback_pr_per_protocol`.

Override: explicit operator instruction in chat — e.g. "open the PR
for R9 right now" — opens the PR for that one item only.

### No release-engine clutter on `main`

The `.audit/` folder, `*-draft.md`, `*-wip.go`, and any other local
scratch never lands on `main`. release-please reads conventional-commit
subjects from `main` to bump the changelog; non-conventional clutter
breaks the version cadence.

### No option-table proposals during DOD

When direction is clear (DOD-blocker work), the agent picks the next
gap and executes. No "which gap first?" option tables. Selection
criterion: clearest scope + smallest effort.

## How to apply

1. **Identify the DOD window.** A DOD window starts when an operator
   names a target connector and ADR-0025 shows ≥ 1 `partial` / `missing`
   deliverable. It ends when all six deliverables are `done` and the
   connector PR merges.
2. **Treat ADR-0014 as the parent contract** — every clause not
   overridden here still binds.
3. **For non-DOD work** (doc hygiene, infra fixes, separate-connector
   bug fixes) ADR-0014 applies unchanged.
4. **Operator override always wins.** Explicit chat instruction
   ("open the PR for X now") supersedes any rule in this ADR.

## Consequences

- The PR triage queue during a DOD window stays small (zero) until the
  window closes.
- Operator time is spent on integration-test sign-off and scope
  decisions, not on per-PR review.
- The atomic-commit history on the feature branch is the audit trail
  for what landed when, and remains available for `git log` review
  before the consolidated PR opens.

## Forbidden

- Opening a PR for an atomic DOD commit without operator approval.
- Bundling two issues into one commit.
- Reporting "green tests" as progress instead of ADR-0025 deliverables.
- Committing `.audit/` or draft files to `main`.
- Proposing option tables to the operator during a DOD window.

## Revisions

- 2026-05-18 — initial proposal, derived from the
  `memory/feedback_workflow_contract` entry produced by the
  2026-05-17 Ember+ session.
