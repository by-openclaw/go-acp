# Per-verb test specification

**Status: proposed** (pending fold into ADR-0025). **Scope:** the Tree/DM generic
verb set (acp1 / acp2 / emberplus) — the template; Matrix/Push/Bridge verbs follow
the same three-tier shape (§4).

**Sourcing (strict):**
- **Definition** of each verb is owned by [`verbs.md`](verbs.md) / ADR-0002 — cited here, not restated (ADR-0015).
- **Test tiers** are the established rules applied per verb, not new invention:
  - **Unit** — CLAUDE.md "Testing": table-driven, **expected bytes from the spec**, `MockTransport` injected (DI), **no real sockets**.
  - **Smoke** — built binary runs the verb, parses flags, emits output, exit 0; fast, no real device.
  - **Integration** — **oracle-per-tier** (`repo-review` §7) + ADR-0025 #3: the **CLI** verb run against the **vendor emulator AND a real device**, result asserted; repeatable. A consumer is **never** validated against our own provider.

---

## 1. Tiers (definition)

| Tier | Proves | Oracle / driver | Determinism |
|---|---|---|---|
| **Unit** | codec + per-verb logic | the spec (expected bytes) + injected mock transport/clock | fully deterministic, no network |
| **Smoke** | the verb is wired, flags parse, output shape is right | built binary, loopback or trivial target | fast, CI-gating |
| **Integration** | the verb behaves correctly on the wire | **vendor emulator + real device** (acp1: Synapse Simulator `10.6.239.113:2071` + Axon device) | repeatable; idempotent verbs via Ansible run-twice |

---

## 2. Per-verb spec (Tree/DM: acp1 / acp2 / emberplus)

Definitions: see `verbs.md` §2. Below is **how each is tested**.

| Verb | Unit | Smoke | Integration (vs emulator + device) |
|---|---|---|---|
| `info` | decode a mock device-info / slot-info reply (expected bytes) → assert slot count + per-slot status | `info <host>` exit 0, prints slot table | slot count + statuses match the device fixture |
| `walk` | table-driven decode of **every object type** from mock replies → assert canonical objects (type, access, options, ranges) | `walk --slot 0` exit 0, >0 objects | object count + types match fixture for a known card |
| `tree` | golden-output: render a fixed canonical tree → assert exact ASCII **and** PlantUML | `tree --slot 0` exit 0, valid `@startmindmap` | renders the **full** slot tree (current gap: shallow render) |
| `get` | decode mock value reply per type (expected bytes) → assert `Value` (raw, kind, enum items, default) | `get … --label X` exit 0 | known value matches fixture |
| `set` | (a) encode request = expected bytes (valid); (b) **client-side validation** rejects out-of-range / wrong-type / bad-enum with **exit 2** (table-driven) | valid value → exit 0; bad value → **exit 2** | `set` then `get` readback equals; bad input rejected exit 2, **no wire write** |
| `watch` | decode mock announce frames → assert events; announce-gating logic (acp1 `Broadcasts` / acp2 `EnableProtocolEvents`) | `watch` binds + exits on `--timeout` | change a value on the peer → assert announce received |
| `export` | canonical→json/yaml/csv golden; **CSV lossless** round-trip | `export --out f.json` exit 0, file schema-valid | export a slot → schema-valid → re-import is idempotent |
| `import` | dry-run logic; **RW-only**; **mismatch skipped, non-blocking**; per-type pass/fail/skip tally | `import f.json --dry-run` exit 0, prints tally | import (dry-run then apply) → readback matches; mismatches skipped, exit 0 |
| `extract` | meta+wire+tree triple shape (ADR-0020 Bucket 3) | `extract …` writes the triple | triple captured into fixture layout from a real walk |
| `diff` | two canonical trees → expected text / CHANGELOG diff (golden); exit code reflects difference | `diff a.json b.json` exit code = diff? | offline — unit covers it |
| `convert` | json↔yaml↔csv round-trip golden | `convert in.json out.csv` exit 0 | offline — unit covers it |
| `discover` | parse mock discovery responses → device list | `discover` exit 0 | native subnet scan finds the emulator/our producer; mDNS path (optional) finds our producer |
| `profile` | compliance-counter logic → strict / partial classification | `profile` exit 0 | classification matches (emulator = STRICT, verified 2026-06-07) |
| `validate` | decode a fixture `frames.jsonl` → per-frame pass/fail | `validate fixture.jsonl` exit 0 | offline; optional re-decode of a captured real session |
| `health` | 3-layer state logic with mock conn | `health` exit 0, prints layers | reachable / connected / live all true vs a live peer |
| `ensure` *(to build)* | read→compare→decide; **idempotency** (2nd call = no change); `--check` performs no write; exit 0 + `changed` flag; validation → exit 2 | `ensure --check` exit 0, reports would-change | **Ansible playbook, run twice → 2nd run reports 0 changes** (the idempotency proof) |

---

## 3. Cross-cutting rules

- **Exit codes** per `docs/protocols/error-codes.md`: `0` ok · `1` runtime/wire · `2` usage/validation. Every smoke/integration assertion checks the exit code, not just stdout. (`set`/`ensure` bad-input MUST be exit 2 — current acp1 gap: exit 1.)
- **DI requirement:** unit tests substitute `MockTransport` (+ a mock clock for timeout/keep-alive verbs) — no real sockets, no real time.
- **Oracle rule:** consumer integration runs against the **vendor emulator + real device**, never our own provider. Provider integration is verified by our *trusted* consumer (after the consumer passes) and finally by the manufacturer's controller (manual).
- **Idempotency:** `ensure` (and any state-changing verb driven through Ansible) proves idempotency by the **run-twice = 0 changes** rule.

## 4. Protocol-specific verbs (same three tiers)

Matrix / Push / Bridge verbs use the identical Unit/Smoke/Integration shape; only
the oracle device changes. Examples:

| Verb | Unit | Smoke | Integration |
|---|---|---|---|
| probel `connect` | encode cmd-2 expected bytes; ACK/NAK handling | `connect --matrix 0 --level 0 --dst 5 --src 12` exit 0 | vs Commie/TS emulator + VSM → crosspoint routed, tally confirms |
| osc `watch` | decode each type-tag from mock frames | `watch --listen udp:8000` binds | vs osc.js peer → message received, line shape matches dissector |
| tsl `listen` | decode v3.1/v4.0/v5.0 frames (expected bytes) | `listen --bind :4000` binds | vs Miranda TSL emulator → tally frame decoded |
| cerebrum `route` | encode ROUTE XML (golden) | `route --dest 60 --srce 60 --level 1` exit 0 | vs Cerebrum NB → route applied (consumer-only; no provider) |
