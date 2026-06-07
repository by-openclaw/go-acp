# ADR-0014 — Issue tracking discipline

Status: accepted

## Context

Past sessions have produced extensive terminal output documenting
how features were investigated, what bytes were on the wire, what
worked, what failed — all of which was lost when the terminal closed.
GitHub issues exist precisely to be the durable record. Terminal
output is not authoritative.

## Decision

Every connector command / feature / bug fix follows a strict issue
discipline.

### One issue per command (or coherent unit)

For protocols structured per-command (Probel SW-P-08, SW-P-02, ACP1,
ACP2 alike): one tracker issue per command. The issue is the running
log of work for that unit.

For non-command-oriented protocols, one issue per coherent feature
unit.

### Update before close

Every command / feature that lands → progress comment on the
tracking issue containing:

- File paths touched
- Key bytes / wire shape (if codec change)
- Real-peer evidence (Commie / VSM / live device captures)
- Refs to commits + PR
- Spec citations (section / page / paragraph)

The issue is updated **as work progresses**, not retrospectively at
close time.

### Close summary required

Before closing any issue, post a final comment with:

- Final scope (what landed, what didn't)
- Test count (unit + integration)
- Commits / PR refs
- Caveats / known limitations
- Spec deviations (if any) → cross-link to the corresponding
  compliance event in code

### Workflow chain (every change — code, ADR, doc)

| Step | Detail |
|---|---|
| 1. Issue | new tracker issue with title + scope + acceptance + labels (`proto:<name>` + `bug` / `enhancement` / `documentation` + optional `cli`) |
| 2. Branch | new branch off `main`, named after the work (`feat/<proto>-<verb>` / `fix/<proto>-<bug>` / `docs/adr-NNNN-<title>`) |
| 3. Code | one coherent unit per ADR-0013; always `go build` to `bin/` before committing |
| 4. Unit tests | table-driven, byte-exact against the spec |
| 5. Integration tests | real peer / live device — pass on the rig before opening PR (symmetric self-tests don't count) |
| 6. PR | opened against `main` (or the long-running protocol branch per ADR-0014) with results table + `Closes #N` |
| 7. CI green | all checks pass; never merge a red PR |
| 8. Codeowner approval | **`@yboujraf` codeowner approval required** — `.github/CODEOWNERS` enforces |
| 9. Merge | only after green + approval |
| 10. release-please | bumps tag if the change is user-visible (feat/fix); no manual changelog edits |

### Labels

Per `gh label list`. Existing labels in this repo:

- `bug`, `enhancement`, `documentation`, `question`
- `cli`
- `proto:acp1`, `proto:acp2`, `proto:emberplus`, `proto:probel-sw08p`,
  `proto:probel-sw02p`, `proto:tsl`, `proto:osc`, `proto:cerebrum-nb`,
  `proto:nmos`, `proto:blackmagic-hyperdeck`

Always check `gh label list` before opening; never invent labels.

### "Closes #N" placement

Put `Closes #N` in the **PR body**, not only in the commit body.
Squash merges drop per-commit `Closes` lines.

## Consequences

- Issues become the durable history of every change.
- Reviewers can read one issue and understand the full
  investigation.
- New contributors learn from past issues, not from chasing
  terminal output.
- release-please gets clean conventional-commit titles.

## Forbidden

- Closing an issue without a close summary.
- Merging a PR without `@yboujraf` approval.
- Merging a red CI.
- Inventing labels not in `gh label list`.
- Treating terminal output as authoritative.
