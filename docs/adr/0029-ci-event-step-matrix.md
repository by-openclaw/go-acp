# ADR-0029 — CI event→step matrix, no retries, single-pass coverage

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

The CI pipeline grew by patching symptoms and drifted from its
original clear/fast shape (issue #694, owner audit request
2026-08-17). Two accretions dominated:

- **3-attempt retry loops** around the unit-test step (main matrix
  and the RHEL container jobs), added after
  `TestRunDiagnostics_ConnectionClosed` reddened a main push on
  rocky9 (2026-08-17) that every other platform passed.
- **5-attempt coverage UNION** in the per-package coverage-floor
  step, added because statement coverage of goroutine/timer select
  arms depended on scheduling, so a genuinely-coverable line could
  miss the 100% floor on a loaded runner.

Both mechanisms treat nondeterminism as weather instead of as a bug.
The owner flagged the union twice: it is slow (up to 5 test runs per
package × 27 packages) and it can mask a real regression — a test
that fails 1 run in 3 sails through a retry loop forever.

The root causes were fixed on the same branch that lands this ADR:

- **acp2/consumer** — request waiters raced a ready reply channel
  against a closed `done` channel in one `select`; Go picks a ready
  arm pseudo-randomly, so "reply" vs "connection closed" (and the
  coverage of each arm) was a coin flip when the peer replied and
  hung up in one burst. Fixed with a deterministic waiter-death
  protocol: the read loop's exit sweeps the waiter table under the
  registration mutex, delivering a nil sentinel to every waiter (a
  reply already delivered survives), and later registrations fail
  fast. Every waiter receives exactly one value — no racy select arm
  remains, and the diag probe/announce timings became injectable so
  the tests run in milliseconds.
- **probel-sw02p/provider** — the salvo integration test grabbed a
  free port by listen-close-relisten; a parallel test sweep can steal
  the port inside that window. Fixed by pre-binding the listener and
  handing it to the provider (`ServeListener` on the concrete type).
  A repo-wide sweep converted every other CI-run instance of the same
  pattern (emberplus consumer loopback fixtures via an emberplus
  `ServeListener` seam; the acp1 UDP zero-byte test serves on `:0`
  directly and reads the bound address off the server).

With the causes gone, the retries and the union are dead code, and
the pipeline can be written down as one auditable step chain per
event type so it cannot drift again.

## Decision

### Event→step matrix

`.github/workflows/ci.yml` is the **only** workflow file. Each event
type has exactly the steps below — adding a step, a retry, or a new
workflow file requires updating this ADR in the same PR.

| Event | Where | Steps (in order) |
|---|---|---|
| **COMMIT** (local) | `.githooks/pre-commit` | `go vet ./...` → `golangci-lint run ./...` |
| **PR** (`pull_request` → main) | ci.yml | test matrix (ubuntu/windows/macos): tidy → build → vet → unit `-race` **once** · RHEL containers (rocky9/ubi9): build → vet → unit **once** · lint · cross-compile. Concurrency: a new push cancels the in-progress run on the outdated commit. |
| **MERGE** (`push` → main) | ci.yml | same verification as PR, **never cancelled** (every merge commit keeps its own complete run) + coverage profile + per-package coverage floor + release-please bookkeeping behind the full gate. |
| **RELEASE** (release-please PR merged → tag) | ci.yml `build-release` | cross-compile all targets → Windows version resource → optional Authenticode signing → package archives → SHA-256 manifest → upload to the GitHub Release. Changelog is owned by release-please only. |

The coverage steps also run on PRs (ubuntu leg) so a floor regression
is caught before merge, not after.

### No retries — a red is a red

Retry loops around test steps are **forbidden**, in CI and in local
scripts. A test that needs a retry is flaky, and a flaky test is a
bug in the test or the code under test: root-cause it (the two fixes
above are the worked examples) instead of hiding it. The only
acceptable response to a transient-infrastructure failure (runner
died, network blip fetching modules) is re-running the job by hand —
visibly, not silently in a loop.

### Single-pass, deterministic coverage

- The per-package floor is computed from the **one** whole-repo
  coverage profile (`go test -coverprofile ./...`) — no per-package
  re-runs, no union. Package rows are selected by
  `^dhs/<pkg>/[^/]+\.go:` so nested packages (e.g.
  `cerebrum-nb/codec` vs `cerebrum-nb/codec/ws`) stay separate.
- A floor package with **no rows** in the profile is a hard failure
  (guards against a renamed package silently passing).
- On failure the step prints the exact uncovered blocks — a gap is
  named, never a mystery retry.

**Determinism rule for new tests:** statement coverage of every
branch must not depend on goroutine scheduling. The patterns that
achieve this, proven on this branch:

1. **No racy select arms** — when two select arms can be ready
   simultaneously, restructure so each outcome has exactly one
   deterministic delivery path (acp2 nil-sentinel waiter protocol).
2. **Injectable timings** — production timeouts stay; tests inject
   millisecond values through an options struct, never by editing
   globals (acp2 `diagTimings`).
3. **No listen-close-relisten** — tests bind `127.0.0.1:0` once and
   pass the listener to the server under test.

Evidence at adoption: all 27 floor packages measured 100.0% on
consecutive independent single runs after the fixes (acp2/consumer
5×, probel-sw02p/provider 3×, full sweep 1×; previously 99.6% racy
on acp2/consumer).

### Speed budget

Measured job baselines on the last pre-slim main run (2026-08-19):
Test ubuntu 5.8 min (this job carried the union sweep) · windows
2.7 min · macos 2.3 min · rocky9 1.8 min · rhel9-ubi 1.5 min · lint
0.6 min · cross-compile 1.4 min. The new floor check is pure awk over
the existing whole-repo profile (seconds) — the union sweep it
replaces cost up to 135 extra package test runs, so the ubuntu job is
expected at ≈ 4 min. A job that doubles its baseline is a regression
to investigate, not a budget to raise silently.

### Considered and rejected

- **`paths-ignore` for docs-only commits** — rejected. Skipping the
  workflow on docs commits would leave merge commits on main without
  their own complete verification (owner rule), stall release-please
  bookkeeping until the next code commit, and leave required PR
  checks pending on docs-only PRs. The single-pass speed work makes
  full runs cheap enough that the exemption is not worth its edge
  cases.

  One narrow exception (revision 2026-08-19): the **pull_request**
  trigger ignores `CHANGELOG.md` + `.release-please-manifest.json` —
  the two bot-owned files that are the release-please PR's entire
  diff. That PR is force-pushed on every merge to main, and each
  force-push spawned a CI run that parked at `action_required`
  (bot-authored PR + workflow approval policy): permanent noise in
  the Actions list, never a real verification. The `push` trigger is
  untouched, so the release merge commit itself still gets its own
  complete run — the owner rule holds.
- **Keeping the union "just in case"** — rejected. With p the
  per-run miss probability, the union passes with ~1−p⁵ even when a
  line is missed 80% of runs — that is exactly the masking the owner
  flagged. Determinism is enforced at the test-design level instead.

## Consequences

- CI is one documented chain per event; a red run means a real
  problem in that commit, worth reading every time.
- The coverage floor keeps its no-regression meaning: a line no test
  can reach fails the floor immediately, on every run.
- New concurrency code must ship with deterministic test seams
  (§Determinism rule) or it will red the floor honestly.
- Issues #690/#691 layering (single-workflow release gating,
  concurrency groups) is retained; only the retry/union accretions
  are removed.

## Revisions

- 2026-08-19 — initial version (issue #694).
- 2026-08-19 — pull_request trigger ignores the two release-please
  bot files (CHANGELOG.md, .release-please-manifest.json) so the
  release PR stops spawning permanently-action_required runs on
  every merge; push-to-main verification unchanged.
