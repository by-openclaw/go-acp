# dhs per-protocol audit archive

Each `<proto>-audit-<date>.md` here is a point-in-time, code-driven
audit of one connector's consumer ↔ provider feature parity for the
use-case set documented in `docs/protocols/use-cases/<proto>.md`.

R19 #484 defines the per-UC audit shape and the methodology
(inventory → wire verify → compliance verify → reconcile).

| Date | Protocol | File | Last live SHA |
| --- | --- | --- | --- |
| 2026-05-18 | Ember+ | [emberplus-audit-2026-05-18.md](emberplus-audit-2026-05-18.md) | `c875e34` (DOD branch) |

## Audit cadence

- Per-connector audit runs whenever its ADR-0025 deliverable count
  changes (a new UC lands, a deliverable closes, a divergence is
  discovered against a real peer).
- Each audit file is immutable once committed. A fresh `<proto>-audit-<date>.md`
  is added rather than mutating the previous one — preserves the
  historical record of where the connector stood at each cut.

## Divergence handling

Audits that find consumer ↔ provider divergence file a separate issue
tagged `audit-followup` + the relevant `proto:<name>` label. The
audit notes the issue number inline so a reader can trace the
investigation.
