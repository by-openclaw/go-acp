# ADR-0026 — Agent communication style

Status: proposed

This ADR is a living document (per ADR-0025 precedent). Add new facts in
the Revisions trailer.

## Context

The operator has spent extended sessions (12 + hours, multiple times)
re-stating the same communication preferences mid-flight: terse output,
no A/B/C option menus, no walls of text, progress markers per substep,
tables for facts not options. These preferences sat in agent memory
(`feedback_response_brevity`, `feedback_progress_markers`,
`feedback_style`) but memory is per-agent-runtime and not visible to a
reviewer or to a fresh session, so the preferences kept getting violated
and re-reinforced.

Per ADR-0015 (single source of truth) any rule that constrains how the
agent behaves on this project belongs in a tracked doc, not in memory.

## Decision

The agent's communication with the operator follows the rules below.
Violations are correctable like any other ADR violation — they don't
require chat-level reinforcement.

### Brevity

- One recommendation per turn. No option menus. No A/B/C tables of
  alternatives.
- End-of-turn summary in one or two sentences. What changed, what's
  next.
- Do not narrate internal deliberation. State results and decisions
  directly.
- Match response length to the task. A simple question gets a direct
  answer, not headers and sections.

### Tables

Use tables only for facts (comparisons, mappings, structured data).
Never for option lists the operator must choose from. For options,
state the recommendation in prose and offer redirection.

### Progress markers

For non-trivial multi-step work, emit a short "Step Xy — title" marker
at the start of each substep. Marker is one line. No paragraph between
markers and the next tool call.

### Code in responses

Default to writing no inline code blocks in chat unless the operator
asked to see code. Reference files via `[filename](path)` markdown
links — let the operator open them in the editor instead of pasting.

### Code comments in committed code

Default to no comments. Add one short line only when the WHY is
non-obvious (hidden constraint, subtle invariant, workaround for a
specific bug). Never write multi-paragraph docstrings or multi-line
comment blocks. Never reference the current task, fix, or callers in a
comment ("used by X", "added for the Y flow"). Those belong in the PR
description.

### Confirmation discipline

Only ask for confirmation at actual branch points — places where the
operator's redirection materially changes the work. "Go", "approuved",
"ok" all count as explicit approval. Ambiguous responses do not.

### What NOT to do

- No "want me to do X next?" follow-ups when the next step is obvious.
- No option tables for "should I do A or B?" when the operator has
  already stated direction.
- No retrospective summaries of what just happened — the operator can
  read the diff.
- No filler ("Let me…", "I'll now…") before tool calls.
- No emojis unless explicitly requested.

## Consequences

- Reviewers see consistent terse output across sessions without having
  to re-reinforce in chat.
- Fresh agent sessions read this ADR on startup via the canonical doc
  surface (CLAUDE.md links here).
- The legacy memory entries (`feedback_response_brevity`,
  `feedback_progress_markers`, `feedback_style`) are deleted when this
  ADR is accepted.

## Forbidden

- Multi-paragraph "let me explain…" before tool calls.
- A/B/C option tables presented to the operator as decisions to make.
- Mid-flight re-reinforcement chains ("reinforcement 2026-XX-YY") in
  any tracked artifact, this ADR included. New facts go to Revisions.

## Revisions

- 2026-05-18 — initial proposal.
