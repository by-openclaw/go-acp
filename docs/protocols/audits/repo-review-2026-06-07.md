# Complete Repo Review — dhs (Device Hub Systems)

**Date:** 2026-06-07 · **Reviewer:** audit session (Opus 4.8) · **Repo:** `by-openclaw/go-acp`
**Purpose:** the single anchor. Read this before any repair/agent work so we stop
circling, stop bypassing ADRs/rules/agent files, and have one agreed source of
truth. Supersedes the scratch `CLAUDE.local.md` handoff and `AUDIT-PUNCHLIST.md`.

---

## 0. Operating rules (binding this session forward)

| Rule | Source |
|---|---|
| Windows host → **PowerShell only** (git-bash spam hides output) | `docs/user.md` |
| Real devices/emulators reachable **only over VPN** to DMZ `10.100.0.0/24` | `docs/testbed.md` |
| Commits are **Vault-signed** → when Vault is down: edit freely, **do not commit**, never `--no-verify` | observed 2026-06-07 |
| Gated actions need explicit `go`/`approuved`/`ok`: PR create/merge, issue close, CI changes, infra/creds | `docs/user.md` |
| Comms: terse, tables for facts, no option menus, no emojis | ADR-0026 |
| **Oracle rule:** never validate our consumer against our own provider | codeowner, 2026-06-07 |

Secrets: device creds live in **Vault KV v2** (ADR-0010), VPN-gated. Local
`.secrets/` holds only the CI GitHub token. SSH to fleet uses `~/.ssh/by-rune_lxc`.

---

## 1. Governance model — what is authoritative

```
ADRs (docs/adr/, binding, permanent)              ← win all conflicts (ADR-0015)
  └─ root CLAUDE.md (auto-loaded)                  ← cross-cutting Go/wire/scale rules
       └─ internal/<proto>/CLAUDE.md (per-proto)   ← atomic wire spec, on cd
  docs/user.md  · docs/testbed.md                  ← roles, host, fleet
  MEMORY.md (acp project store) = "active memory EMPTY; do not load archive/"
```

**Root cause of "Claude bypasses my rules":**
1. Sessions were run from the **home folder**, not the repo → no governance auto-loaded. Fix: always open `Downloads/acp` as the workspace root.
2. Three layered generations contradict: `MEMORY.md` says memory is empty, but ~76 places across the repo still cite `memory/feedback_*` / `project_*` as live sources (dead pointers).
3. `agents.md` is **not auto-loaded** (Claude reads `CLAUDE.md`). Its still-live rules are invisible to a fresh session.

**Permanent fix (do in the repair):** make `CLAUDE.md` the single auto-loaded
spine (fold `agents.md`'s live rules in, or reduce `agents.md` to a stub),
purge dead memory pointers, and add a CI markdown link-checker so it can't rot.

---

## 2. Repair branch `docs/md-reference-graph-repair` — audit verdict

15 commits, one per finding. Build **green**. All `.go` changes verified
**comment-only** (no logic touched). No new dead links introduced. Commits atomic.

| Finding | Status |
|---|---|
| F4 wrong ADR filenames | ✅ done |
| F8 dissector naming (`dhs_<proto>.lua`) | ✅ done |
| F2 links to 4 missing amwa docs | ✅ removed |
| F11 amwa→probel **core** (matrix-compliance "Mirror" block, CLAUDE.md:260) | ✅ done |
| F3/F6 agents.md (abs paths, ADR index 0026/0027) | ✅ done |
| D2 ARCHITECTURE.md stale package names | ✅ done |
| D3 ADR-0002 verb set | ✅ done |
| D5 tsl "placeholder" unflagged | ✅ done |
| F10 README NMOS wall-of-text | ✅ collapsed |
| F1 dead `memory/*` pointers | ⚠️ **~⅓ done — see §3** |
| F9 remaining dead relative links | ⚠️ **incomplete — see §4** |
| F11 soft remainder | ⚠️ **3 mentions left — see §5** |

**Not mergeable yet.** F1 and F9 have a large tail.

---

## 3. Dead-reference inventory (F1 + D7) — the real scope

`git ls-files` scan (tracked only, excl. legacy): **76 raw hits / 47 files.**
That number is raw — it must be triaged, not blindly purged:

| Bucket | Action | Notes |
|---|---|---|
| **Genuine dead pointers** ("see/per `feedback_X`/`project_X`/`memory/`") | **FIX** | repoint to the live ADR / doc, or delete stale self-bookmarks |
| **Intentional historical mentions** | **LEAVE** | `docs/adr/0026:14,15,88,89` deliberately names the legacy `feedback_*` entries it *replaced* — editing corrupts the ADR |
| **False positives** | **LEAVE** | the word "reference", or code identifiers, in many `errcode.go`/`csv.go`/ADR hits |

**Confirmed genuine dead pointers to fix (high-value, agent/governance + docs):**
`README.md:41` · `CONTRIBUTING.md:39` · `.github/PULL_REQUEST_TEMPLATE.md:29` ·
`docs/VISION.md:428,444` · `docs/protocols/error-codes.md:10,80,128,155,215,216,217` ·
`internal/amwa/CLAUDE.md:96,123,171` · `internal/amwa/docs/dependencies.md:401,403` ·
`internal/amwa/docs/ha.md:281` · `internal/amwa/docs/runbook-multi-os.md:9` ·
`internal/amwa/codec/is04/v13/codec.go:13` · `internal/emberplus/docs/consumer.md:99,100` ·
`internal/emberplus/docs/runbook.md:116,716,746,767,768` ·
`internal/probel-sw08p/CLAUDE.md:285` · `internal/probel-sw08p/provider/session.go:137,138` ·
`internal/probel-sw08p/provider/server.go:222` · `internal/export/canonical/common.go:63` ·
`docs/adr/0022-card-data-model.md:196` · `docs/adr/0023-matrix-entity.md:153`

**Needs a 1-line per-file read before deciding fix vs leave (~15 ambiguous):**
`cmd/dhs/main.go:377` · `cmd/dhs/exitcode_test.go:16` · `internal/acp1/consumer/cache.go:191` ·
`internal/acp2/consumer/session.go:539` · `internal/acp2/provider/encoder.go:326,tree.go:242` ·
`internal/acp2/docs/runbook.md:48` · `internal/amwa/provider/node.go:96` ·
`internal/amwa/provider/registry_watcher.go:39` · `internal/amwa/registry/helpers.go:25` ·
`internal/amwa/session/dnssd/mdns.go:153` · `internal/amwa/codec/dnssd/types.go:16` ·
`internal/cerebrum-nb/codec/doc.go:21` · `internal/consumer/errors.go:13` ·
`internal/emberplus/codec/ber/errors.go:30,matrix/state.go:176` ·
`internal/emberplus/consumer/{compliance_events.go:157,errcode.go:12,plugin.go:1117}` ·
`internal/errcode/errcode.go:7,65` · `internal/transport/errcode.go:8` ·
`internal/export/{csv.go:123,read_csv.go:67,roundtrip_test.go:120}`

**Replacement guide:** `feedback_no_workaround` → root CLAUDE.md "Spec-strict
posture"; `feedback_codec_isolation` → ADR-0006; `feedback_logging` → docs/logging.md;
`feedback_error_contract_cross_os` → docs/protocols/error-codes.md (it IS the
canonical home now); `project_dhs_data_model` self-bookmarks → delete the line;
`feedback_amwa_strict_all_versions`/`feedback_pr_per_protocol` → root CLAUDE.md
"AMWA NMOS strict" + ADR-0014.

---

## 4. Dead relative links (F9) — still broken (~10)

| File | bad link | fix |
|---|---|---|
| `internal/acp1/assets/README.md`, `acp2/assets/README.md` | `../../CLAUDE.md` | `../CLAUDE.md` |
| `internal/acp1/docs/consumer.md`, `acp2/docs/consumer.md` | `../schema.md` | `../../../docs/protocols/schema.md` |
| same two | `../../../tests/{unit,fixtures/exports}/<proto>/` | repoint to `internal/<proto>/testdata/…` or remove (verify) |
| `internal/amwa/docs/dns-sd-unbound.md` | `../../protocol/compliance/` | `../../consumer/compliance/` |
| `internal/emberplus/docs/runbook.md` | `.audit/walks/demo.jsonl` | low-sev gitignored artifact — reword |

**Do not touch (false positives):** `docs/adr/0026 → "path"`, `internal/acp1/CLAUDE.md → "group, id"`.

---

## 5. amwa ↔ probel (F11) — soft remainder (codeowner decision)

Core coupling removed. These 3 remain; codeowner said "amwa has zero relation
with probel" → default = remove/reword:
- `internal/amwa/docs/architecture.md:444` — parked NMOS→Probel mux mention
- `internal/amwa/docs/dependencies.md:402` — protocol example list incl. Probel
- `internal/amwa/dependencies_test.go:169` — comment example path

---

## 6. Per-protocol completeness vs ADR-0025 DOD (6 deliverables)

| Proto | Consumer | Producer | Integration test | DM/manifest | Dissector | Replay fixtures | Score |
|---|---|---|---|---|---|---|---|
| **acp1** | ✅ (cover 33%) | ✅ (58%) | ⚠️ plugin-level smoke, env-gated, no CLI-verb / verify script | ✅ +9 product fixtures | ✅ | ✅ | **~5/6** |
| **acp2** | ✅ (cover **24%**) | ✅ (50%) | ❌ **`t.Skip("ACP2 not implemented yet")`** | ⚠️ 0 product fixtures | ✅ | ⚠️ | **~3.5/6** |

acp2/consumer has **69 functions at 0.0% coverage** incl. `Connect/Walk/Get/Set`
— core logic effectively unvalidated because integration is a skip placeholder.
Other protocols (emberplus, probel×2, osc, tsl, cerebrum-nb) have **no `docs/` set** —
only CLAUDE.md. No protocol has its own sequence diagram (only the 9 shared ASCII
flows in `docs/protocols/use-cases.md`).

---

## 7. Test taxonomy — oracle per tier (extend ADR-0025 with this)

| Tier | Proves | Oracle (ground truth) | VPN? |
|---|---|---|---|
| 1 Unit | codec ↔ spec bytes | the spec (expected-byte tables) | no |
| 2 Consumer integration | consumer drives a real peer | **vendor emulator + real device** (never our provider) | yes |
| 3 Provider integration | our provider serves a real controller | real external controller (e.g. Lawo VSM) | yes |
| 4 Loopback regression | nothing regressed | our trusted consumer ↔ our trusted provider — **only after 2+3 pass** | no |

Emulators on hand: acp1 = **Synapse Simulator** (software, repeatable) + device;
**acp2 = no emulator** (real Neuron only — open question: does any Neuron
emulator exist?); emberplus = TinyEmber+; probel = Commie/TS emulator + VSM;
osc = osc.js; tsl = Miranda emulator; amwa = sony/nmos-cpp + AMWA Testing tool + Cerebrum.

"Consumer done" gate = every CLI verb (info/walk/get/set/watch/export↔import/profile)
asserted against emulator AND device, repeatable.

---

## 8. Controlled vocabulary

**Verbs:** `audit` = read-only review · `implement` = branch+code+tests, stop before PR ·
`approve seq N` = unblock a queued probel-sw02p command · `go`/`approuved`/`ok` =
execute the gated action · `ship` = open PR (gated).
**Nouns:** `ADR` = binding decision (wins all) · `memory` = dead/archived, never a
source of truth · `connector` = one protocol plugin · `DOD` = the 6 ADR-0025 deliverables.

---

## 9. Open decisions (codeowner only — not auto-decided)

1. ADR-0026 + ADR-0027 `proposed` → `accepted`?
2. F1 scope: fix the genuine pointers + leave intentional/false-positive (recommended) vs full 47-file grind?
3. F11 soft remainder (§5): remove vs keep?
4. acp2 emulator: exists, or is acp2 forever device-gated?
5. agents.md: fold into CLAUDE.md (recommended) vs keep as separate stub?

---

## 10. Ordered path forward

1. **Finish the repair branch** — F1 genuine pointers (§3), F9 links (§4), F11 soft (§5). Commit when Vault's up (signed). → re-audit → `ship`.
2. **Single agent spine** — fold `agents.md` live rules into `CLAUDE.md`; add the startup read-gate; add CI markdown link-checker (closes the loop permanently).
3. **Extend ADR-0025** with the §7 test taxonomy + a CI coverage floor.
4. **acp1 to green** — Synapse Simulator CLI-verb integration matrix; raise consumer coverage.
5. **acp2 parity** — real integration (pending emulator answer); kill the `t.Skip`.
6. Per-protocol `docs/` sets for the connectors that lack them.

---

*Status when this was written: on branch `docs/md-reference-graph-repair`; working
tree clean except untracked `.audit/`, `CLAUDE.local.md`, `AUDIT-PUNCHLIST.md`,
cerebrum PDF, `snell-rollcall/` (deferred). Vault down → no commits pending.*
