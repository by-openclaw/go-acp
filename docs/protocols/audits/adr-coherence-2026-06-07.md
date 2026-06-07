# ADR Coherence Review — 27 ADRs

**Date:** 2026-06-07 · **Method:** 4 parallel cluster reviews (read-only) ·
**Companion to:** `repo-review-2026-06-07.md`.
**Verdict:** the ADR set is **structurally sound and not over-split** — each ADR is
genuinely one concern, the data-model cluster (0022/0023/0024) is exemplary. The
problems are: a few **real contradictions**, **status drift**, **duplication**
(ADR-0015 violations — ironically including 0015 itself), pervasive **asymmetric
cross-linking**, and several **binding rules that live outside the ADR system**.

---

## 1. Real contradictions / defects (must fix)

| # | Defect | Where | Fix |
|---|---|---|---|
| C1 | **Exit-code 2 collision** — `ensure` defines exit `2` = "success but changed"; error-codes.md defines exit `2` = usage/validation error. A successful idempotent change is indistinguishable from a validation failure. | ADR-0007 vs `docs/protocols/error-codes.md` | `ensure` signals change via its `changed:true` JSON field, **never** exit 2. Cross-ref error-codes.md. |
| C2 | **"five" vs "six" deliverables** — header/table say six; three prose sentences still say five (incomplete edit when #6 was added 2026-05-17). | ADR-0025 | Fix the three stale "five" mentions. |
| C3 | **SSOT ADR has a stale duplicate index** — 0015's internal index lists 0001–0019 only (missing 0020–0027); README's index is complete. Two indexes, one stale — the exact drift 0015 forbids. | ADR-0015 | Delete 0015's internal index; point to README. |
| C4 | **Wrong citation** — "key rotation per ADR-0009 / ADR-0014" (0009=plugin supervisor, 0014=issue tracking; neither covers rotation). | ADR-0003 line 79 | Repoint to ADR-0010 (or inline; rotation has no ADR home — see §4). |
| C5 | **Manifest drift** — `0005-deps.json` `adr_refs` don't surface in the rendered table; all `version`/`cve_last_check`/`cve_status` fields are `null` (skeleton, gates unenforced). | ADR-0005 / 0005-deps.json | Reconcile generator; populate or mark pre-implementation. |

## 2. Status drift

| ADR | Issue | Action |
|---|---|---|
| 0026, 0027 | `proposed` but treated as **binding** (CLAUDE.md "read first", CLAUDE.local.md enforces 0026). `proposed` = "under discussion". | Promote to `accepted` (codeowner) or stop citing as binding. *(= repo-review F7)* |
| 0012 | `accepted` while its only heavy-tier consumer (AMWA) is parked and its macOS/Windows backends are "TBD per 0016". | Scope to the Linux+stdlib surface, or park the NMOS-grade tier with AMWA. |
| 0020 | `accepted` but Consequences end on an unresolved fork ("Bucket 3 separate repo **or** stays in spec module"). | Resolve or mark deferred. |

## 3. Duplication (ADR-0015 single-source violations)

| # | Rule duplicated | Copies in | Designate owner |
|---|---|---|---|
| D1 | **Approval vocabulary** ("go/approuved/ok" + autonomous-vs-gated) — the worst offender, **4 copies** | ADR-0014 (gate), 0026, 0027, `docs/user.md` | **ADR-0014 step 8** owns; others link |
| D2 | `info` JSON schema (`os`/`arch`/`dnssd_backend`) | ADR-0016 + ADR-0018 | **0018** owns schema; 0016 owns backend *selection* only |
| D3 | Capture storage policy (paths, gitignore, no-LFS) | ADR-0020 + ADR-0021 | **0020** owns; 0021 → one-line pointer |
| D4 | AppRole / Transit rotation text | ADR-0003 + ADR-0010 | pick one, link the other |
| D5 | Scale floor `65 535²` restated inline | ADR-0023 + root CLAUDE.md | an ADR owns it (see §4 P5) |

## 4. Binding rules that are NOT ADRs (promote — the key structural gap)

ADR-0015 says CLAUDE.md/docs **link** rules, never **own** them. These binding rules
currently have no ADR home:

| # | Rule | Lives in | Leaned on by | Recommendation |
|---|---|---|---|---|
| P1 | **Error / exit-code contract** | `docs/protocols/error-codes.md` | 0007, 0025, every verb | **New ADR** (strongest case — already *contradicts* 0007, C1). |
| P2 | **DI / OOP / encapsulation / no-hidden-state** | root CLAUDE.md | 0001, 0009 | **New ADR** (binding "violations require sign-off", but CLAUDE.md-only). |
| P3 | **Wireshark dissector mandate** (full coverage, `dhs_` prefix) | root CLAUDE.md | **0025 gates on it** | **New ADR** (0025 gates a rule with no ADR). |
| P4 | **HA / lease-based controller election** | `internal/amwa/docs/ha.md` | 0022 + 0023 | New ADR, **or park with AMWA** (it's amwa-rooted). |
| P5 | **Scale targets** (65535², 20–100 matrices) | root CLAUDE.md | 0023 | Fold into an ADR (0023 or new). |
| — | **Test taxonomy** (unit + smoke + Ansible integration; oracle-per-tier) | nowhere yet | the connector bar | **Extend ADR-0025** (not a new ADR). |
| — | Key rotation | inline 0003/0010 only | 0003 (mis-cited) | Give it a home (fold into 0010). |

## 5. Over-split verdict: **no merges needed**

- `ensure` (0007) + `info` (0018) — keep separate; each carries a contract too heavy for 0002. 0018 sets the precedent that legitimizes 0007.
- Licensing (0003/0004/0010/0011) — keep split; four principled axes (format / trial-policy / network / business-record).
- Capture (0020/0021) — keep split (location vs line-schema); just fix the storage-policy dup (D3).
- Data model (0022/0023/0024) — correctly split, well-glued. **Model cluster.**

## 6. Cross-link (glue) additions — cheapest high-value fixes

Asymmetric linking is the most pervasive flaw. Add: 0002→0018 (mirror the 0002→0007
link) · 0001→0016 · 0014→0027 (its own override) · 0014→0025 · 0004↔0003 · 0004↔0011 ·
0023↔0024 · 0012↔0008 · 0012↔0022 · 0026↔0027 (shared approval vocab) ·
0009↔0002 (0002 must list the `core plugin` verb subtree 0009 claims).

## 7. Scope signal

The **licensing cluster (0003/0004/0010/0011) is design-ahead, not shipped**:
`deps.json` operational fields all `null`, no `internal/license/` or `internal/signer/`
code, absent from the live work inventory. Treat as a coherent **commercialization-phase**
design set; fix its 4 cross-link items, don't action it now. ADR-0005 is active-but-skeletal.
**AMWA parked** → park 0012's heavy tier + P4 with it.

---

## 8. Recommended actions (priority order)

1. **Fix the 5 contradictions** (§1) — C1 (exit-code) and C3 (0015 stale index) first; both undermine the governance system's own credibility.
2. **Resolve status drift** (§2) — accept 0026/0027; scope 0012.
3. **Promote 3 binding rules to ADRs** (§4): error-contract (P1), architecture/DI principles (P2), Wireshark mandate (P3). Extend ADR-0025 with the test taxonomy.
4. **Collapse the 5 duplications** (§3) — especially D1 (approval vocab, 4 copies).
5. **Add the cross-links** (§6) — mechanical, low-risk.
6. **Amend ADR-0002** to define verbs **per protocol-model** (Tree/DM · Matrix · Push · Bridge), per `verbs.md` §1 — the current "uniform set, stub everything" doesn't match reality.

All of this is doc-only; commit when Vault is up (no `--no-verify`).
