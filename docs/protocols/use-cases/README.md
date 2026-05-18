# dhs use-case matrix index

Per-protocol use-case matrices live under this directory. Each file is
the permanent companion to that protocol's runbook
(`internal/<proto>/docs/runbook.md`) and stays the canonical answer to
"what does dhs do for `<proto>` today?" even if the runbook
restructures.

| Protocol | File | Status |
| --- | --- | --- |
| Ember+ | [emberplus.md](emberplus.md) | maintained (R20 #485) |
| ACP1 | _placeholder_ | TODO — file lands when ADR-0025 bar reached |
| ACP2 | _placeholder_ | TODO |
| Probel SW-P-08 / SW-P-88 | _placeholder_ | TODO |
| Probel SW-P-02 | _placeholder_ | TODO |
| AMWA NMOS | _placeholder_ | TODO |
| TSL UMD | _placeholder_ | TODO |
| OSC | _placeholder_ | TODO |
| Cerebrum-NB | _placeholder_ | TODO |
| Blackmagic HyperDeck | _placeholder_ | TODO |

## Column conventions

| Column | Meaning |
| --- | --- |
| **UC** | Stable identifier (`UC-NN`). Survives runbook restructuring. |
| **Use case** | Short verb-shaped label matching the runbook section. |
| **Consumer** | `✅` if `dhs consumer <proto> <verb>` is wired and exercised; `❌` with issue ref if pending. |
| **Provider** | `✅` if `dhs producer <proto>` honours the verb; `❌` with issue ref if pending. |
| **Implementation** | Link(s) to the primary source file(s). |
| **Tests** | Link(s) to the primary test file(s). `n/a` only when the verb is offline-pure (e.g. `convert`) and has no live integration coverage. |
| **Wireshark** | Section name in `internal/<proto>/wireshark/dhs_<proto>.lua` that decodes the verb's wire shape. `n/a` for offline verbs. |
| **Notes** | Outstanding scope, pending PRs, edge cases. |

## Editing rules

- Add new rows at the bottom and assign the next `UC-NN`. Never renumber.
- A row with `❌` on both consumer/provider columns is **pending** — mention the tracking issue in Notes.
- Implementation / Tests / Wireshark cells link to **real paths**. CI may add a link-checker step in a future pass.
- "Last verified: <SHA>" trailer at the top of each per-protocol file is bumped manually until the CI step lands (R20 spec out-of-scope item).
