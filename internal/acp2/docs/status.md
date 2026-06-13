# ACP2 — feature & ADR-compliance status

Single-page, numbered status of the ACP2 connector: every feature, its state,
the evidence, and the ADR it satisfies. Strict — "verified live" means exercised
against a real **EVS Neuron** (CONVERT Hybrid, the vendor oracle, never our own
provider as the consumer oracle); "code" means implemented and unit-tested but
not in the live verify matrix.

- **As of:** 2026-06-13
- **Spec:** `internal/acp2/assets/acp2_protocol.pdf` + `an2_protocol.pdf` · wire ref: `internal/acp2/wireshark/dhs_acpv2.lua`
- **Transport:** AN2/TCP **only** (port 2072, AN2 proto=2) — no UDP, no direct TCP.
- **Oracle:** real EVS Neuron CONVERT Hybrid (lab, reached from a device-side host) + our provider emulator (`dhs producer acp2 serve`, validated against Cerebrum + Lawo VSM) for the loopback-regression tier.
- **Coverage:** codec **98.8%** · consumer **84.2%** · provider **90.0%** (CI floors enforce no-regression). Consumer < 90 because the residual gap is live-session / timer / device-response-shape branches reachable only via the integration tier (now present), not unit-coverable without a live peer.

Legend: ✅ verified live · 🟢 code + unit test · 🟡 partial · ⬜ not started · — N/A

---

## 1. Consumer CLI verbs

| # | Verb | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 1 | `info` | ✅ | live vs real Neuron `10.44.72.28` (AN2 v0.1, ACP2 v2, 2 slots present); integration test asserts slot count | 0002 |
| 2 | `walk` | 🟢 | integration test (full DFS over the served tree); consumer loopback `walk_loopback_test.go` | 0002 |
| 3 | `tree` | 🟢 | shared tree renderer over the walked tree | 0002 |
| 4 | `get` | 🟢 | integration get on a real leaf; `getset_loopback_test.go` (pid/idx, cold-cache, raw fallback) | 0002 |
| 5 | `set` | 🟢 | integration set + round-trip confirm; `getset_loopback_test.go` (typed + raw + no-access) | 0002 |
| 6 | `ensure` | 🟢 | converge-to-value, idempotent (`--check` dry-run) | 0007 |
| 7 | `watch` | 🟢 | announce dispatch + decode after EnableProtocolEvents; `subscribe_loopback_test.go` | 0002 |
| 8 | `export` | 🟢 | json/yaml/csv of a walked device | 0002 |
| 9 | `import` | 🟢 | `--dry-run` non-destructive apply | 0002 |
| 10 | `extract` | 🟢 | DM triple (meta + wire + tree) | 0022 |
| 11 | `diff` | 🟢 | canonical tree.json schema diff | 0002 |
| 12 | `convert` | 🟢 | json↔yaml↔csv (offline) | 0002 |
| 13 | `validate` | 🟢 | offline frames.jsonl decode through the codec | 0006 · 0021 |
| 14 | `diag` | 🟢 | ACP2 diagnostic probes (`diag_loopback_test.go`) | 0002 |
| 15 | `health` | 🟢 | 3-layer session health (reachable / connected / live) | 0002 |

`inc` / `dec` / `reset` are ACP1 setInc/Dec/Def methods — **N/A** for ACP2 (the
spec defines only get_version / get_object / get_property / set_property; writes
go through `set`). `matrix` / `invoke` / `stream` are Ember+-only — N/A.

## 2. Consumer transport + protocol completeness

| # | Item | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 16 | AN2/TCP (2072, proto=2) — the only ACP2 transport | ✅ | live handshake vs real Neuron; `connect_loopback_test.go` | 0001 |
| 17 | AN2 init sequence (GetVersion → GetDeviceInfo → GetSlotInfo → EnableProtocolEvents → ACP2 GetVersion) | ✅ | live + loopback; required before any ACP2 traffic | 0001 |
| 18 | 4 ACP2 functions driveable from the consumer | 🟢 | get_version / get_object (walk/meta) / get_property (get) / set_property (set) | 0002 |
| 19 | pid/idx addressing; preset idx=0 = ACTIVE INDEX (never "first slot") | 🟢 | `getset_loopback_test.go` (explicit-pid, idx) | 0006 |
| 20 | Keep-alive prober + watchdog; warm-restart reconnect (replays subs) | 🟢 | `keepalive_test.go`, `io_coverage_test.go` (reconnect) | 0001 |

## 3. Producer (`dhs producer acp2 serve`) — the emulator

| # | Capability | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 21 | Serve AN2/TCP (2072) from a canonical tree | 🟢 | `serve_loopback_test.go` (full Serve accept loop) | 0001 |
| 22 | Validated against real controllers (Cerebrum, Lawo VSM) | ✅ | prior live runs — legitimizes use as the consumer's loopback oracle | 0025 #3 |
| 23 | Per-object-type encode (node/preset/enum/number/ipv4/string) | 🟢 | `encoder_test.go`, `encoder_neuron_test.go`, `set_enum_test.go` | 0006 |
| 24 | Announce broadcast on value change (after EnableProtocolEvents) | 🟢 | `serve_loopback_test.go` (broadcastAnnounce) | 0006 |
| 25 | Multi-slot frame status from the served tree | 🟢 | loopback `info` shows N present slots | 0022 |

## 4. Codec & wire (stdlib-only, lift-ready)

| # | Item | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 26 | AN2 framer (magic 0xC635, 8-byte header) + ACP2 4-byte header | 🟢 | `framer_test.go`, `codec_test.go` | 0006 |
| 27 | Property codec (pid/plen headers, 4-byte alignment, all vtypes) | 🟢 | `property_codec_test.go`, `property_spec_test.go` | 0006 |
| 28 | Stringers + value helpers + error types | 🟢 | `stringers_value_test.go` | 0006 |
| 29 | Codec imports zero `dhs/*` (lift-ready) | 🟢 | import audit | 0006 |
| 30 | Wireshark dissector `dhs_acpv2.lua` (AN2 + ACP2, all funcs/types) | 🟢 | `internal/acp2/wireshark/dhs_acpv2.lua` | 0025 #5 |

## 5. Tests & tooling (oracle-per-tier)

| # | Tier | State | Evidence | ADR |
|---:|---|:--:|---|---|
| 31 | Unit (codec / consumer / provider) | 🟢 | CI per-package coverage floor (no-regression gate) | 0025 #1/#2 |
| 32 | Consumer integration — CLI-driving, loopback-regression | 🟢 | `internal/acp2/integration` — provider emulator + consumer over real AN2/TCP; info/walk/get/set | 0025 #3 |
| 33 | Consumer integration — real device (env-gated) | 🟡 | same test honours `ACP2_TEST_HOST`; `info` verified live vs Neuron `10.44.72.28`; full verb matrix vs device pending a device-side runner | 0025 #3 |
| 34 | Replay fixtures under `testdata/` | 🟢 | `protocol_types/` per-type captures (pcapng + tshark.tree) + `fixtures/` + `exports/` | 0025 #6 |
| 35 | Idempotency — Ansible test tier (run-twice = 0 changed) | 🟡 | shared `ansible/playbooks/test-idempotency.yml` covers the `ensure` verb; acp2 emulator deploy to the fleet pending | 0007 · 0016 |

## 6. Error contract

| # | Rule | State | Evidence |
|---:|---|:--:|---|
| 36 | Exit 0 ok / 1 runtime-wire / 2 usage-validation | 🟢 | `cmd/dhs/main.go` `exitCode` |
| 37 | ACP2 error stat codes 0-5 surfaced (protocol/obj-id/idx/pid/access/value) | 🟢 | `ACP2Error` + `stringers_value_test.go`; loopback error-reply paths |
| 38 | Client-side value validation (range/enum/type) | 🟢 | `value_validate.go`; `value_validate_test.go` |

## 7. ADR-0025 — definition of done (6 deliverables)

| # | Deliverable | State | Notes |
|---:|---|:--:|---|
| D1 | Consumer strict-to-spec, every CLI verb | 🟢 | AN2/TCP + all 4 functions driveable; verbs 1-15; unit + loopback + live `info`. |
| D2 | Producer strict-to-spec | 🟢 | Items 21-25; served loopback; validated live vs Cerebrum/VSM. |
| D3 | Repeatable CLI integration test | 🟢 | `internal/acp2/integration` (loopback-regression) + `ACP2_TEST_HOST` real-device path. |
| D4 | DM + manifest generator | 🟢 | DM cache + manifest (ADR-0022); `extract`. |
| D5 | Wireshark dissector | 🟢 | `dhs_acpv2.lua` — AN2 + ACP2, all functions + object types. |
| D6 | Replay fixture set under `testdata/` | 🟢 | Per-type captures + fixtures + exports. |

**Verdict: 6/6 deliverables present; live real-device verification is the remaining quality gate (see gaps).**

## 8. Known gaps / caveats (honest)

1. **Real-device verb matrix** — only `info` is verified live against the real Neuron (`10.44.72.28`) so far; walk/get/set/watch are unit + loopback-verified and run against the device via `ACP2_TEST_HOST` once a device-side runner is available (the dev host is firewalled from the device net by the manufacturer firewall).
2. **acp2 emulator on the fleet** — the LXC + win11 fleet currently serve ACP1; standing up `dhs producer acp2 serve` there (the fleet emulator) is the remaining deploy step for the Ansible idempotency tier.
3. **Provider Tier-3 (real controller)** — our provider was validated against Cerebrum / Lawo VSM previously; a fresh live walk by a real controller is lab/VPN-bound, not CI-runnable.
