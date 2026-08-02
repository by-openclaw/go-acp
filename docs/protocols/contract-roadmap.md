# Operator CLI "contract" roadmap — bring every protocol onto `ensure`/`--check`/`--output`

Status: planning · Owner: `@yboujraf` · Author: by-rune · Date: 2026-08-02

## Goal

Every supported connector exposes the **same declarative contract** so it drops
into any Ansible playbook (CLI-in-a-role, no Python) and into the future TUI /
REST-WS / gateway front-ends unchanged. The contract, per **ADR-0002**
(canonical verbs/flags) and **ADR-0007** (`ensure`):

- **idempotent** — read current → apply only the diff → run-twice = `changed:false`
- prints **`--output json`** with `{changed|would_change, previous, current, diff[]}`
- supports **`--check`** (dry-run, mutate nothing)

Scope now: **acp1, acp2, emberplus, probel-sw02p, probel-sw08p, tsl** (all TSL
versions; v3.1/v4.0 UDP, v5.0 UDP+TCP — v3.1/v4.0-over-TCP is out of spec scope).
Deferred: osc, cerebrum-nb (northbound), amwa.

## Audit — gap matrix (2026-08-02, read-only audit of the tree)

| Protocol | Neutral iface | Rides generic verbs | Scalar `ensure` | Real operator-state | Transports |
|---|---|---|---|---|---|
| acp1 | full 9/9 | yes | works | `inc`/`dec`/`reset` fire-and-forget | UDP · TCP · AN2 |
| acp2 | full 9/9 | yes | works | announce / subscribe / `event_delay` uncovered | AN2/TCP |
| emberplus | full 9/9 | yes | works (no `diff[]`) | matrix read-back model exists, CLI fire-and-forget; lock/salvo provider-only | TCP/S101 |
| probel-sw02p | stubbed | **bypasses** | unreachable | crosspoint/protect read-back exists, unwired; salvo f&f | TCP |
| probel-sw08p | stubbed | **bypasses** | unreachable | crosspoint/label/protect read-back exists but never read-before-write; salvo f&f | TCP |
| tsl | stubbed (push) | **bypasses** | N/A (push) | UMD tally: no producer read-back; `validate`/`canonicalize` built but not routed | v3.1/4.0 UDP · v5 UDP+TCP |

### Three patterns

1. **Shared gaps hit all six** (fix once, not per protocol): producer role is
   `serve`-only everywhere (`stop`/`status`/`tree`/`ensure`/`validate`/`replay`
   missing); consumer `status`/`replay` missing; flag split (`--json` vs
   `--format`, no canonical `--output`); `ensure` omits the ADR-0007 `diff[]`.
2. **Two camps.** *Interface-native* (acp1, acp2, emberplus) ride the generic
   verb table and scalar `ensure` already works. *Bypass* (sw02p, sw08p, tsl)
   short-circuit to their own dispatcher with neutral `GetValue`/`SetValue`/
   `Walk` stubbed, so `ensure` is unreachable.
3. **Real operator-state is fire-and-forget everywhere, but the read-back
   already exists** for emberplus/sw08p/sw02p. Making matrix/crosspoint/protect
   idempotent is **wiring existing read-back into the apply as a diff**, not new
   wire code.

## Decisions (confirmed with @yboujraf, 2026-08-02)

- **Phase 0 first** (shared foundation) before the matrix feature.
- **One canonical matrix-ensure** over a shared matrix abstraction (ADR-0023),
  implemented once and reused by emberplus/sw08p/sw02p — not reimplemented per
  protocol. Needs a small ADR-0007/0023 amendment.
- Ansible tests = **use-case acceptance tests** (Tier 3): drive the real CLI,
  assert converge + `--check` + run-twice=0. Complement Go unit tests (Tier 1),
  never replace them. No Python.

## Plan (each item: branch → Go tests @100% → Ansible use-case test → PR → CI green → merge)

### Phase 0 — shared foundation (benefits all 6)
- Unify **`--output json|yaml|text`** (keep `--json` as a deprecated alias).
- Add **`diff[]`** to `ensure` output (ADR-0007 requires it always).
- Add canonical consumer verbs **`status`**, **`replay`**, **`connect`**, **`disconnect`**.
- Add producer verb surface **`stop`/`status`/`tree`/`ensure`** (extend `dispatchProducer` + provider iface).

### Phase 1 — canonical matrix-ensure (headline)
- Shared converge: read snapshot → diff vs desired honoring behavior
  (oneToN/oneToOne/nToN + caps + lock) → minimal op → verify read-back →
  `--check`/`changed`/`diff[]`.
- Order: **emberplus** (`codec/matrix/state.go` model most complete) → **sw08p**
  (crosspoint → label → protect; salvo = documented fire-and-forget exception)
  → **sw02p**.

### Phase 2 — pull bypass protocols onto the contract
- sw08p / sw02p / tsl: implement neutral interface (or add contract verbs) so
  `ensure`/`--check`/`--output` are reachable.
- TSL: route the already-built `validate`/`canonicalize`; alias `watch`→`listen`;
  populate the per-INDEX `Canonicalize` model; **ratify TSL-ensure as N/A via an
  ADR-0007 amendment**, not an `ErrNotImplemented` stub.

### Phase 3 — per-protocol tails + external oracle
- acp1 `reset`→ensure (document `inc`/`dec` as non-idempotent); acp2
  announce/subscribe/`event_delay` converge; complete producer verbs.
- External-oracle Ansible tiers (real device/emulator, ADR-0025 Tier 2/3).
- Reconcile acp2 `status.md` coverage numbers vs the 100% CI floors.

## Per-protocol gap detail (from the audit)

- **acp1** — iface complete; `replay`/`status`/standalone `connect`/`disconnect`
  missing; `inc`/`dec`/`reset` not contract-wrapped; producer thin; `--transport`
  omits `an2` on consumer despite plugin support.
- **acp2** — iface complete and loopback-tested (the old "unvalidated / 69 funcs
  at 0%" handoff is **stale**); `replay`/`status` missing; `validate --out-tree/
  --out-params` partial; AN2/TCP only; producer thin.
- **emberplus** — iface complete; scalar `ensure` works but no `diff[]`;
  `connect`/`disconnect`/`status`/`replay` missing; **matrix CLI fire-and-forget
  despite a read-back-capable behavior model**; lock/salvo provider-only.
- **probel-sw02p** — bypasses generic table; neutral methods `ErrNotImplemented`;
  TCP-only (no UDP/serial); provider exists; crosspoint/protect read-back exists
  but unwired; salvo fire-and-forget.
- **probel-sw08p** — bypasses generic table; neutral methods stubbed; read-back
  primitives exist for crosspoint/label/protect/salvo but **no apply reads before
  writing**; `Plugin.Validate` exists but unrouted; no `--json` anywhere.
- **tsl** — all in-scope version×transport cells implemented; only `listen`/`send`
  verbs wired; `validate`/`canonicalize` built but unrouted; push-only so
  `ensure` is genuinely N/A pending an ADR ruling; producer has no read-back.

## ADR touch-points

- **ADR-0002** — implementing `status`/`replay`/`connect`/`disconnect`/`--output`
  and the producer verbs is *compliance* with the already-canonical set, not new
  ADR. Document the `--json`→`--output` migration.
- **ADR-0007** — amend to cover **declarative data/matrix desired-state** (today
  it reads as session/service lifecycle + scalar value convergence) and to
  ratify **TSL-ensure = N/A**.
- **ADR-0023** — the shared matrix abstraction the canonical matrix-ensure sits on.
- **ADR-0025** — each phase item ships its six DOD deliverables incl. Ansible
  integration + idempotency proof.
