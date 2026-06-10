# ACP1 — feature & ADR-compliance status

Single-page, numbered status of the ACP1 connector: every feature, its state,
the evidence, and the ADR it satisfies. Strict — "verified live" means exercised
against the **Synapse Simulator** (the vendor oracle at the lab, never our own
provider) or a real Axon rack; "code" means implemented and unit-tested but not
in the live verify matrix.

- **As of:** 2026-06-10
- **Spec:** `internal/acp1/assets/AXON-ACP_v1_4.pdf` · wire ref: `internal/acp1/wireshark/dhs_acpv1.lua`
- **Oracle:** Synapse Simulator (`internal/acp1/assets`) + real Axon rack over VPN.
- **Coverage:** codec **100%** · consumer **90.6%** · provider **90.3%** (CI floors enforce no-regression).

Legend: ✅ verified live · 🟢 code + unit test · 🟡 partial · ⬜ not started · — N/A

---

## 1. Consumer CLI verbs

| # | Verb | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 1 | `info` | ✅ | `verify-info.ps1` (slot count + per-slot status) | 0002 |
| 2 | `walk` | ✅ | `verify-walk.ps1` (groups + object count) | 0002 |
| 3 | `tree` | ✅ | `verify-tree.ps1` (ASCII render) | 0002 |
| 4 | `get` | ✅ | `verify-get.ps1` (value + kind + enum items) | 0002 |
| 5 | `set` | ✅ | `verify-set.ps1` — write + confirm; client-side validate → exit 2 | 0002 |
| 6 | `inc` | ✅ | `verify-inc.ps1` — setIncValue; live 5→6 vs Synapse | 0002 |
| 7 | `dec` | ✅ | `verify-dec.ps1` — setDecValue; live 6→5 vs Synapse | 0002 |
| 8 | `reset` | ✅ | `verify-reset.ps1` — setDefValue; live 7→default(0) | 0002 |
| 9 | `ensure` | ✅ | `verify-ensure.ps1` — idempotent; clamp-aware; exit-2 validate | 0007 |
| 10 | `watch` | ✅ | `verify-watch.ps1` (live announce; broadcast-tolerant) | 0002 |
| 11 | `export` | ✅ | `verify-export.ps1` (json/yaml/csv) | 0002 |
| 12 | `import` | ✅ | `verify-import.ps1` (`--dry-run`, non-destructive) | 0002 |
| 13 | `profile` | ✅ | `verify-profile.ps1` (objects walked + compliance classification) | 0002 |
| 14 | `extract` | ✅ | `verify-extract.ps1` (DM triple: meta+wire+tree) | 0022 |
| 15 | `diff` | ✅ | `verify-diff.ps1` (schema diff: no-change + mutated) | 0002 |
| 16 | `convert` | ✅ | `verify-convert.ps1` (json→yaml) | 0002 |
| 17 | `validate` | ✅ | `verify-validate.ps1` (offline frame decode) | 0006 · 0021 |
| 18 | `discover` | 🟡 | `verify-discover.ps1` (native subnet scan ✅); **mDNS/SD-DNS deferred to the AMWA cycle** | 0012 |

`matrix` / `invoke` / `stream` are Ember+-only; `diag` is ACP2-only — N/A here.
The per-verb matrix is **18/18** under `scripts/acp1/verify-all.ps1` (auto-globbed).

## 2. Consumer transport + methods (protocol completeness)

| # | Item | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 19 | Mode A (UDP 2071) | ✅ | default; live vs Synapse | 0001 |
| 20 | Mode B (TCP direct 2071, MLEN) | 🟢 | `--transport tcp`; `tcp_client.go` + loopback test | 0001 |
| 21 | Mode C (AN2/TCP 2072) | 🟢 | `--transport an2`; `an2_client.go` + loopback test; **live AN2 pending a real Axon rack (emulator is UDP-only)** | 0001 |
| 22 | All 6 methods driveable from the consumer | ✅ | get/set + inc/dec/reset (setInc/Dec/Def); getObject internal (walk/meta) | 0002 |
| 23 | SET-timeout GET-confirm retry (no inc/dec double-apply) | 🟢 | `client.go` getConfirm; `retry_confirm_test.go` (spec p.12) | 0006 |

## 3. Producer (`dhs producer acp1 serve`)

| # | Capability | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 24 | Serve Mode A (UDP 2071) | ✅ | live fleet + win11 runs; real-UDP loopback test | 0001 |
| 25 | Serve Mode B (TCP 2071) | 🟢 | `ServeTCP`, `tcp_server_test.go` | 0001 |
| 26 | Serve Mode C (AN2 2072) | 🟢 | `ServeAN2`, `an2_server_test.go` | 0001 |
| 27 | `--transport udp/tcp/an2/all` | 🟢 | `cmd/dhs/cmd_producer.go` | 0002 |
| 28 | Multi-card frame (frame-status from tree) | ✅ | `MarkTreeSlotsPresent`; loopback `info` shows N present slots | 0022 |
| 29 | `--play` / `--play all` / `--play-mode walk\|random` | ✅ | `play_test.go`; live announce stream across all slots | 0022 |
| 30 | `--preload slot=card` · Admin RPC · clamp-on-set · metrics | 🟢 | `cmd_producer.go`, `admin_test.go`, `set.go`, metrics wiring | 0022 · 0006 |
| 31 | Re-serves real device objects byte-faithfully | 🟢 | `provider.TestReplayFidelity_GetObject` (244 real objects re-served) | 0006 |

## 4. Codec & wire (stdlib-only, lift-ready)

| # | Item | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 32 | 6 methods · 11 object types · 7-byte header · announcements | 🟢 / ✅ | `message_spec_test.go`, `property_spec_test.go`; live watch/play | 0006 |
| 33 | Codec imports zero `dhs/*` (lift-ready) | 🟢 | import audit | 0006 |
| 34 | Wireshark dissector `dhs_acpv1.lua` (all transports/methods) | 🟢 | `internal/acp1/wireshark/dhs_acpv1.lua` | 0025 #5 |
| 35 | Unit coverage **100%** of statements | 🟢 | EOF-guard refactor removed dead branches; CI floor = 100 | 0025 |

## 5. Tests & tooling (oracle-per-tier)

| # | Tier | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 36 | Unit (codec 100% / consumer 90.6% / provider 90.3%) | 🟢 | CI per-package coverage floor (no-regression gate) | 0025 #1/#2 |
| 37 | Codec **integration replay** (real device frames, CI, device-free) | 🟢 | `codec.TestReplay_RealFrames` — 600 real frames decode + byte-exact round-trip; corpus `testdata/replay/<device>/` (INDEX.md + CAPTURING.md) | 0025 #3/#6 |
| 38 | Provider **fidelity replay** (re-serve real objects) | 🟢 | `provider.TestReplayFidelity_GetObject` — 244 objects | 0025 #3 |
| 39 | Per-verb integration (PowerShell, vs emulator) | ✅ | `scripts/acp1/verify-*.ps1` 18/18 PASS; skips clean when host unset | 0025 #3 |
| 40 | Go `//go:build integration` tier (gated `ACP1_TEST_HOST`) | ✅ | `internal/acp1/smoke` — info/walk/get + set/inc/dec/reset; AN2 skips on UDP-only host | 0025 #3 |
| 41 | Idempotency — **Ansible test tier** (run-twice = 0 changed, asserted) + multi-OS + WinRM | ✅ | `ansible/playbooks/test-idempotency.yml` (`make test-ansible`) asserts the 2nd `ensure` is 0-changed; `site.yml` converges debian/ubuntu/rocky/win11 | 0007 · 0016 |

## 6. Error contract

| # | Rule | State | Evidence |
|---:|---|:--:|---|
| 42 | Exit 0 ok / 1 runtime-wire / 2 usage-validation | ✅ | `cmd/dhs/main.go` `exitCode`; live `set`/`ensure` bad-value → 2 |
| 43 | Client-side `ValueValidator` (enum/type/read-only → exit 2) | ✅ | `consumer/value_validate.go`; `set`+`ensure` wired |
| 44 | Out-of-range numeric clamps (NOT exit 2) | ✅ | emulator: NetwPrefix 100 → stored 32, exit 0 |

## 7. ADR-0025 — definition of done (6 deliverables)

| # | Deliverable | State | Notes |
|---:|---|:--:|---|
| D1 | Consumer strict-to-spec, every CLI verb | ✅ | All 3 transports + all 6 methods driveable; verbs 1-18; 90.6% unit. |
| D2 | Producer strict-to-spec | ✅ | Items 24-31; served live on 4 OSes; fidelity replay. |
| D3 | Repeatable CLI integration test | ✅ | `verify-*.ps1` 18/18 + Go integration tier + Ansible idempotency. |
| D4 | DM + manifest generator | 🟢 | DM cache + manifest (ADR-0022); `extract` + preload + manifest serve. |
| D5 | Wireshark dissector | 🟢 | `dhs_acpv1.lua` — all transports + methods. |
| D6 | Replay fixture set under `testdata/` | 🟢 | Per-type captures + the real-frame replay corpus (codec + provider oracle). |

**Verdict: 6/6 met.**

## 8. Known gaps / caveats (honest)

1. **Discovery (#18)** — native subnet scan works; **mDNS / SD-DNS consumer discovery is deferred to the AMWA cycle** (DNS-SD is core there; shared implementation). The one open protocol *feature*.
2. **Live Mode C (AN2) consumer** — unit/loopback-tested; not yet proven against a real Axon rack (the Synapse emulator is UDP-only).
3. **Provider Tier-3 (real controller)** — our provider is loopback- + fidelity-tested; a live walk by a real controller (Cerebrum / Lawo VSM) is the remaining quality gate (lab/VPN-bound, not CI-runnable).
4. **win11 WinRM** requires the per-host bootstrap (`LocalAccountTokenFilterPolicy=1`); SSH is the no-NTLM alternative. See `ansible/windows/configure-winrm.ps1`.
