# NMOS — conformance testing (AMWA NMOS Testing tool)

Every NMOS PR that lands an implementation chunk MUST pass the
relevant AMWA NMOS Testing suite as a CI gate. No phase merges with
"Could Not Test" outcomes against the suites it claims to satisfy.

## What it is

The AMWA NMOS Testing tool is the canonical conformance harness for
NMOS implementations. Hosted at <https://specs.amwa.tv/nmos-testing/>,
sources at <https://github.com/AMWA-TV/nmos-testing>.

A single Python web service that operates in three modes:

- **Mock Registry** — Node-under-test registers against it; tool
  validates the registration payloads + heartbeat behaviour.
- **Mock Node** — Controller-under-test discovers and probes it; tool
  validates the Controller's response.
- **Probe-only** — tool acts as a client, hitting endpoints on the
  implementation-under-test directly.

Hybrid suites combine the three to validate interactions (e.g. IS-04
+ IS-05 staging round-trip).

Public CI Dashboard: <https://specs.amwa.tv/nmos-dashboard/dashboard.html>

## Suite catalogue (current as of 2026-02-16)

| Suite ID | Name | Tests our role | Required for dhs phase |
|---|---|---|---|
| **IS-04-01** | IS-04 Node API | Node | Step #3 (Node provider) |
| **IS-04-02** | IS-04 Registry APIs | Registry | Step #4 (Registry middleware) |
| **IS-04-03** | IS-04 Node API (P2P) | Node | Step #3 follow-up — P2P advertisement |
| **IS-04-04** | IS-04 Controller | Controller | Step #5 (Controller consumer) |
| **IS-05-01** | IS-05 Connection Management | Node | Step #7 (IS-05 provider) |
| **IS-05-03** | IS-05 Controller | Controller | Step #8 (IS-05 consumer) |
| **IS-07-01** | IS-07 Event & Tally | Node | Steps #10 / #11 (IS-07 prov + cons) |
| **IS-08-01** | IS-08 Audio Channel Mapping | Node | Step #9 (IS-08 both sides) |
| **IS-09-02** | IS-09 Discovery | Node | Step #2 (IS-09) |
| **IS-12-01** | IS-12 Control Protocol *(invasive)* | Node | Step #14 (IS-12 wire) |
| **IS-14-01** | IS-14 Device Configuration *(invasive)* | Node | OUT OF SCOPE v1 |
| **IS-10-01** | IS-10 Authorization | Auth Server | OUT OF SCOPE v1 |
| **BCP-003-02** | TLS / Auth | Node + Registry | OUT OF SCOPE v1 |
| **BCP-008-01** | Receiver Status Monitoring | Node | Step #16 |
| **BCP-008-02** | Sender Status Monitoring | Node | Step #17 |

"Invasive" = the suite triggers state changes on the device (writes,
activations, reboots), so dhs runs them only against an isolated
instance, never against a production-loaded Registry.

## How dhs uses it (CI gate, per phase)

Each implementation PR ships:

1. **Docker compose target** under
   `tests/integration/nmos/<phase>/docker-compose.yml`:
   ```yaml
   services:
     dhs-under-test:
       build: ../../..
       command: dhs producer nmos serve --no-mdns ...
     amwa-nmos-testing:
       image: amwa/nmos-testing:latest      # pinned by digest
       depends_on: [dhs-under-test]
   ```
2. **Make target** `make test-conformance-nmos-<suite>` that:
   - boots the compose stack,
   - drives the AMWA tool's QuestionAPI / AnswerAPI in non-interactive
     mode pointing at the dhs endpoint,
   - persists the JSON / JUnit-XML report to
     `tests/integration/nmos/<phase>/results/<date>-<suite>.json`.
3. **CI step** runs `make test-conformance-nmos-<suite>` and fails the
   PR if any test reports `Fail` or `Could Not Test` for required
   coverage. `Optional` and `Test Disabled` are allowed.
4. **Result archive** — every passing report is committed under
   `tests/integration/nmos/results/` so the conformance trajectory is
   a first-class repo artefact (mirrors the per-protocol fixture
   policy in `feedback_fixture_dogfood.md`).

## Verdict semantics

The AMWA tool emits one of these outcomes per individual test case:

| Outcome | dhs gating |
|---|---|
| `Pass` | ✅ required for everything we claim |
| `Fail` | ❌ blocks the PR — fix or document a compliance event + scope-out |
| `Warning` | ⚠️ allowed but logged; converted to a compliance event in our impl |
| `Optional` | ✅ acceptable to skip if we don't claim that feature |
| `Could Not Test` | ❌ blocks the PR — means the test setup is broken; fix the harness |
| `Test Disabled` | ✅ acceptable when AMWA explicitly disables a flaky test |

## Configuration

The tool needs to know our endpoint URLs. Per phase:

| Phase | We expose | We point AMWA at |
|---|---|---|
| Step #2 (IS-09) | System API on `:8010` | `--config http://dhs-under-test:8010/x-nmos/system/v1.0/global` |
| Step #3 (Node) | Node API on `:8080` | `--node-url http://dhs-under-test:8080/x-nmos/node/v1.3/` |
| Step #4 (Registry) | Reg + Query on `:8000` + `:8001` | `--registry-url ...`, `--query-url ...` |
| Step #5 (Controller) | nothing — AMWA acts as Mock Node + Mock Registry, dhs Controller probes | `dhs consumer nmos walk http://amwa-nmos-testing:5001/...` |
| Step #7 / #8 (IS-05) | Connection API on `:8080` | reuse Node URL — IS-05 runs on the same Node host |
| Steps #10 / #11 (IS-07) | WS publisher on `:8090` | `--events-ws-url ws://dhs-under-test:8090/events` |
| Step #14 (IS-12) | NCP WS on `:8090/ncp` | `--ncp-ws-url ws://dhs-under-test:8090/ncp` |

All ports are configurable; the values above are the dhs defaults
documented in `agents.md` once the implementations land.

## Scope-outs (acceptable Fail / skip)

The phases below explicitly do NOT claim these AMWA suites; a `Fail`
on them is acceptable provided the corresponding dhs compliance event
fires AND the scope-out is named in the PR description:

| AMWA suite | Reason scoped out |
|---|---|
| IS-10-01 | Auth out of scope for v1 (top-level). |
| BCP-003-02 | Auth out of scope for v1. |
| IS-14-01 | Device Configuration out of scope for v1. |
| Any TLS-only assertion | dhs v1 ships `http://` and `ws://` only. |
| BCP-007-01 NDI assertions | NDI codec profile WIP at AMWA, deferred. |
| BCP-006-02 / BCP-006-03 (H.264 / H.265) | WIP at AMWA, deferred. |

A future PR may flip any scope-out by claiming the suite + landing the
implementation.

## Versioning + pinning

The AMWA tool master branch moves. We pin per-phase:

```
tests/integration/nmos/<phase>/.amwa-nmos-testing-pin
```

Each pin is a Docker image digest (`amwa/nmos-testing@sha256:<digest>`)
captured the day the PR is approved. Bumping the pin is its own PR
(category `chore(nmos)`) and re-runs every prior phase's suite under
the new tool version — analogous to the tsl/osc cross-impl byte-oracle
pattern in
[`feedback_fixture_dogfood.md`](../../../C:/Users/BY-SYSTEMSSRLBoujraf/.claude/projects/c--Users-BY-SYSTEMSSRLBoujraf-Downloads-acp/memory/feedback_fixture_dogfood.md).

## Cross-reference

- AMWA tool: <https://github.com/AMWA-TV/nmos-testing>
- AMWA dashboard: <https://specs.amwa.tv/nmos-dashboard/dashboard.html>
- Strict-deps doc: [`dependencies.md`](dependencies.md) — conformance
  CI runs against the plugin layer; tool talks to dhs over HTTP / WS
  exactly like a real peer would.
- Matrix-compliance: [`matrix-compliance.md`](matrix-compliance.md) —
  some real vendors (Lawo VSM) deviate from AMWA-tested behaviour;
  passing the AMWA suite is necessary but not sufficient. We run BOTH
  the AMWA tool AND vendor-specific integration tests where available.
- Sequenced plan: [`sequenced-tasks.md`](sequenced-tasks.md) — each
  phase names its required suite ID.
