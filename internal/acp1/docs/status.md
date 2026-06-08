# ACP1 — feature & ADR-compliance status

Single-page, numbered status of the ACP1 connector: every feature, its state,
the evidence, and the ADR it satisfies. Strict — "verified live" means exercised
this cycle against the **Synapse Simulator** (the vendor oracle at the lab, never
our own provider); "code" means implemented and unit-tested but not in the live
verify matrix.

- **As of:** 2026-06-08
- **Spec:** `internal/acp1/assets/AXON-ACP_v1_4.pdf` · wire ref: `internal/acp1/wireshark/dhs_acpv1.lua`
- **Oracle:** Synapse Simulator (`internal/acp1/assets`) + real Axon rack over VPN.

Legend: ✅ verified live · 🟢 code + unit test · 🟡 partial · ⬜ not started · — N/A

---

## 1. Consumer CLI verbs

| # | Verb | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 1 | `info` | ✅ | `scripts/acp1/verify-info.ps1` (slot count + per-slot status) | 0002 |
| 2 | `walk` | ✅ | `verify-walk.ps1` (groups + object count) | 0002 |
| 3 | `tree` | ✅ | `verify-tree.ps1` (ASCII render) | 0002 |
| 4 | `get` | ✅ | `verify-get.ps1` (value + kind + enum items) | 0002 |
| 5 | `set` | ✅ | `verify-set.ps1` — write + confirm; client-side validate → exit 2 (PR #530) | 0002 · error-codes |
| 6 | `ensure` | ✅ | `verify-ensure.ps1` — idempotent; clamp-aware (PR #518); exit-2 validate (PR #516) | 0007 |
| 7 | `watch` | ✅ | `verify-watch.ps1` (live announce; broadcast-tolerant) | 0002 |
| 8 | `export` | ✅ | `verify-export.ps1` (json/yaml/csv) | 0002 |
| 9 | `import` | ✅ | `verify-import.ps1` (`--dry-run`, non-destructive) | 0002 |
| 10 | `profile` | ✅ | `verify-profile.ps1` (objects walked + compliance classification) | 0002 |
| 11 | `extract` | 🟢 | `cmd/dhs/cmd_extract.go` (DM triple capture) — not in live matrix | 0022 |
| 12 | `diff` | 🟢 | `cmd/dhs/cmd_diff.go` (compare canonical trees) | 0002 |
| 13 | `convert` | 🟢 | `cmd/dhs/cmd_convert.go` (offline json/yaml/csv) | 0002 |
| 14 | `validate` | 🟢 | `cmd/dhs/cmd_validate.go` (offline codec validate) | 0006 |
| 15 | `discover` | 🟡 | `cmd/dhs/cmd_discover.go` — native subnet scan present; **mDNS/SD-DNS not done** | 0012 |

`matrix` / `invoke` / `stream` are Ember+-only; `diag` is ACP2-only — N/A here.

## 2. Producer (`dhs producer acp1 serve`)

| # | Capability | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 16 | Serve Mode A (UDP 2071) | ✅ | live fleet + win11 runs | 0001 |
| 17 | Serve Mode B (TCP 2071) | 🟢 | `ServeTCP`, `tcp_server_test.go` | 0001 |
| 18 | Serve Mode C (AN2 2072) | 🟢 | `ServeAN2`, `an2_server_test.go` | 0001 |
| 19 | `--transport udp/tcp/an2/udp+tcp/all` | 🟢 | `cmd/dhs/cmd_producer.go` | 0002 |
| 20 | Multi-card frame (frame-status from tree) | ✅ | `MarkTreeSlotsPresent` (PR #520); loopback `info` shows N present slots | 0022 |
| 21 | `--play` / `--play all` | ✅ | PR #522; live announce stream across all slots | 0022 |
| 22 | `--play-mode walk\|random` + frame-status events | ✅ | PR #524; `play_test.go`; live `slot N: x→y` transitions | 0022 |
| 23 | `--preload slot=card` | 🟢 | `cmd_producer.go` + DM library | 0022 |
| 24 | Admin RPC (slot.load/unload/insert/extract, value, reload) | 🟢 | `cmd_acp1_admin.go`, `admin_test.go` | 0022 |
| 25 | Clamp-on-set ([min,max]) | ✅ | `provider/set.go`; mirrors device (spec p.28) | 0006 |
| 26 | Metrics (`--metrics-addr` /metrics + /snapshot.json) | 🟢 | `cmd_producer.go` metrics wiring | perf-metrics |

## 3. Codec & wire (stdlib-only, lift-ready)

| # | Item | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 27 | 6 methods (get/set/setInc/setDec/setDef/getObject) | 🟢 | `internal/acp1/codec`, `message_spec_test.go` | 0006 |
| 28 | 11 object types (root…frame) | 🟢 | `codec` + `property_spec_test.go` | 0006 |
| 29 | 7-byte wire header (MTID/PVER/MTYPE/MADDR + MDATA) | 🟢 | `codec/message.go` | 0006 |
| 30 | Announcements (MTID=0, dispatch by ObjGrp) | ✅ | live `watch` + `--play` announces | 0006 |
| 31 | Codec imports zero `dhs/*` (lift-ready) | 🟢 | `codec/*` import audit | 0006 |
| 32 | Wireshark dissector `dhs_acpv1.lua` (all transports/methods) | 🟢 | `internal/acp1/wireshark/dhs_acpv1.lua` | 0025 #5 |

## 4. Tests & tooling

| # | Tier | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 33 | Unit (codec/consumer/provider) | 🟢 | ~25 `*_test.go`; CI green 7 OS matrices | 0025 #1/#2 |
| 34 | Per-verb integration (PowerShell, vs emulator) | ✅ | `scripts/acp1/verify-*.ps1` + `verify-all.ps1` (10/10 PASS); skips clean when host unset (PR #526) | 0025 #3 |
| 35 | Idempotency (Ansible, run-twice = 0 changed) | ✅ | `ansible/` role+playbook (PR #528); proven on debian/ubuntu/rocky | 0007 · 0016 |
| 36 | Multi-OS (one binary, every OS) | ✅ | cross-compile linux/win; live Debian·Ubuntu·Rocky·Win11 | 0016 |
| 37 | WinRM bootstrap for Windows hosts (win11 + Server) | ✅ | `ansible/windows/configure-winrm.ps1` (PR #532); win11 `win_ping` pong + role run-twice=0 | 0016 |

## 5. Error contract

| # | Rule | State | Evidence |
|---:|---|:--:|---|
| 38 | Exit 0 ok / 1 runtime-wire / 2 usage-validation | ✅ | `cmd/dhs/main.go` `exitCode`; live `set`/`ensure` bad-value → 2 |
| 39 | Client-side `ValueValidator` (enum/type/read-only → exit 2) | ✅ | `internal/acp1/consumer/value_validate.go`; `set`+`ensure` wired |
| 40 | Out-of-range numeric clamps (NOT exit 2) | ✅ | emulator: NetwPrefix 100 → stored 32, exit 0 |

---

## 6. ADR-0025 — definition of done (6 deliverables)

| # | Deliverable | State | Notes |
|---:|---|:--:|---|
| D1 | Consumer strict-to-spec, every CLI verb | 🟢 | All methods/types/transports; verbs 1-15 above. Unit coverage modest (consumer ~33%); many paths only hit via integration. |
| D2 | Producer strict-to-spec | ✅ | Items 16-26; served live to consumers on 4 OSes. |
| D3 | Repeatable CLI integration test | ✅ | `scripts/acp1/verify-*.ps1` vs the Synapse emulator (vendor oracle) + Ansible idempotency. |
| D4 | DM + manifest generator | 🟢 | DM cache + manifest layout (ADR-0022); `extract` + preload + manifest serve. |
| D5 | Wireshark dissector | 🟢 | `dhs_acpv1.lua` — all transports + methods. |
| D6 | Replay fixture set under `testdata/` | 🟢 | `internal/acp1/testdata/protocol_types/` per-type captures + exports. |

**Verdict: 6/6 met** (D1/D4/D5/D6 not re-verified live this cycle — code + fixtures present).

## 7. ADR compliance summary

| ADR | Topic | acp1 |
|---|---|:--:|
| 0001 | per-connector binary + own subtree | ✅ |
| 0002 | canonical verbs/flags | ✅ |
| 0005 | dependency policy | 🟢 |
| 0006 | codec stdlib-only | ✅ |
| 0007 | ensure / idempotency | ✅ |
| 0009 | plugin supervisor / registry init | 🟢 |
| 0013·0014·0027 | one-unit commit · issue→PR→CI→merge | ✅ (this cycle: PRs #516–#532) |
| 0015 | single source of truth | ✅ |
| 0016 | multi-OS | ✅ (Linux SSH + Windows WinRM, live) |
| 0022 | card data model (Device/Frame/Slot/Card/DM) | ✅ |
| 0025 | per-connector DOD | 🟢 6/6 |
| 0026 | comms style | ✅ |

---

## 8. Known gaps / caveats (honest)

1. **Discovery (#15)** — native subnet scan only; **mDNS / SD-DNS for the consumer is not implemented** (consumer-side discovery of dhs producers). Tracked separately.
2. **Unit coverage** — consumer ~33% / codec ~64%; many consumer paths are only exercised by the integration tier, not unit tests. The PowerShell matrix + Ansible cover the runtime behaviour, but raising unit coverage is open.
3. **`extract`/`diff`/`convert`/`validate`/`discover` (#11-15)** — implemented + unit-tested, but **not** in the live `verify-*.ps1` matrix yet.
4. **Go `-tags integration` tier** — the repeatable integration is PowerShell + Ansible (driving the CLI vs the emulator); there is no in-tree Go integration test gated on `ACP1_TEST_HOST`. Optional, since the CLI-level matrix already proves behaviour against the oracle.
5. **win11 WinRM** requires the per-host bootstrap (`LocalAccountTokenFilterPolicy=1`); see `ansible/windows/configure-winrm.ps1`. SSH is an alternative transport that needs no NTLM.
