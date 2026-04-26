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

## Ground-truth install (verified 2026-04-26 from upstream install doc)

**Published Docker image:** `amwa/nmos-testing` (Docker Hub).

**Ports the container exposes:**
- `:5000` — main web service + non-interactive API (suite runner).
- `:5001` — Controller-testing façade (acts as Mock Registry / Mock Node
  for IS-04-04 / IS-05-03).

## Run via devcontainer + isolated bridge (the only blessed mode)

dhs runs the AMWA conformance suite **inside the existing
`.devcontainer/`**, against a private docker-compose bridge that does
not touch the host LAN. Three constraints govern this design:

| Constraint | How it's enforced |
|---|---|
| **Use devcontainer.** | All conformance commands run from the dev container shell. Reviewers and CI use the same image. Adds `ghcr.io/devcontainers/features/docker-outside-of-docker:1` so `docker compose` from inside the devcontainer talks to the host Docker daemon. |
| **No garbage.** | `make test-conformance-nmos-<suite>` registers a `trap docker-compose-down EXIT`. Bridge network, containers, anonymous volumes — all destroyed on success, failure, OR Ctrl-C. The compose project name encodes the suite + run timestamp so concurrent runs never collide. No persistent volumes; the AMWA tool's results are pulled via its JSON API and written to the host workspace, not via volume bind. `docker image prune` runs nightly in CI. |
| **Stay on local network.** | Custom user-defined bridge network (`dhs_nmos_test_<phase>`). Linux Docker bridge networks isolate broadcast + multicast traffic from the host LAN by default — mDNS announcements stay inside the bridge subnet. Belt-and-braces: `UserConfig.py` ships with `DNS_SD_MODE='unicast'` by default, so even the in-bridge multicast is silenced. No `network_mode: host`, ever. |

### Per-phase compose stack

`tests/integration/nmos/<phase>/docker-compose.yml`:

```yaml
name: dhs_nmos_${PHASE}_${RUN_ID}    # encoded so concurrent runs never collide

networks:
  isolated:                          # ephemeral; destroyed by `down`
    name: dhs_nmos_test_${PHASE}_${RUN_ID}
    driver: bridge
    internal: false                  # outbound DNS works; inbound from LAN does NOT
    driver_opts:
      com.docker.network.bridge.enable_icc: "true"
      com.docker.network.bridge.enable_ip_masquerade: "true"

services:
  dhs-under-test:
    image: dhs:dev                   # built by devcontainer post-create
    networks: [isolated]
    command: dhs producer nmos serve --no-mdns --bind 0.0.0.0:8080
    # NO ports: published. Reachable only from amwa-nmos-testing.

  amwa-nmos-testing:
    image: amwa/nmos-testing@sha256:<DIGEST>   # pinned per phase
    networks: [isolated]
    volumes:
      - ./UserConfig.py:/config/UserConfig.py:ro
    # NO ports: published. We talk to it via `docker exec` or
    # `docker compose run` from the devcontainer; nothing escapes
    # to the host LAN.
    depends_on:
      dhs-under-test:
        condition: service_started
```

### `UserConfig.py` (committed per phase, unicast by default)

```python
# tests/integration/nmos/<phase>/UserConfig.py — copy of UserConfig.example.py
ENABLE_DNS_SD       = True
DNS_SD_MODE         = 'unicast'      # NEVER 'multicast' in CI; it leaks
QUERY_API_HOST      = 'dhs-under-test'
QUERY_API_PORT      = 8000
MAX_TEST_ITERATIONS = 0
```

`DNS_SD_MODE='multicast'` is allowed only on isolated lab segments
where the user explicitly OK's broadcasting. CI never sets it.

### Make target (no garbage guarantee)

`Makefile` snippet that lands in Phase 1 step #1:

```make
PHASE  ?= 02-is09
RUN_ID := $(shell date +%s)-$$$$
COMPOSE := docker compose -f tests/integration/nmos/$(PHASE)/docker-compose.yml \
           --project-name dhs_nmos_$(PHASE)_$(RUN_ID)

.PHONY: test-conformance-nmos
test-conformance-nmos:
	@trap '$(COMPOSE) down -v --remove-orphans --timeout 5' EXIT INT TERM; \
	 $(COMPOSE) up -d --quiet-pull && \
	 scripts/nmos-run-suite.sh $(PHASE) $(RUN_ID)
```

`scripts/nmos-run-suite.sh` does the API dance against the AMWA tool
on `:5000` *inside the bridge* (via `docker compose exec` — never
exposed to the host), pulls the JSON report, writes it to
`tests/integration/nmos/<phase>/results/<RUN_ID>.json`, and exits
non-zero if any test reports `Fail` or `Could Not Test`.

The `trap` ensures cleanup even on Ctrl-C, segfault, or runner OOM.

### Why no `network_mode: host`

`network_mode: host` would publish AMWA mDNS (and any test-suite
multicast probes) onto the user's actual LAN — visible to any other
NMOS-aware device on the network. That fails the "stay on local
network" rule and would risk false discovery in shared lab
environments. Custom bridge keeps everything self-contained.

### Devcontainer feature requirement (lands in Phase 1 step #1)

`.devcontainer/devcontainer.json` will gain:

```jsonc
"features": {
    "ghcr.io/devcontainers/features/docker-outside-of-docker:1": {
        "version": "latest",
        "moveDockerSocket": true
    },
    // ... existing features ...
}
```

Existing devcontainer is a single Go-1.22-bookworm image with no
docker-cli. Adding the docker-outside-of-docker feature gives the
devcontainer access to the host Docker daemon (via socket mount) so
`docker compose` works inside. No Docker-in-Docker — the daemon stays
on the host, the devcontainer just talks to it.

## How dhs uses it (CI gate, per phase)

Each implementation PR ships:

1. **Docker compose target** under
   `tests/integration/nmos/<phase>/docker-compose.yml`:
   ```yaml
   services:
     dhs-under-test:
       build: ../../..
       command: dhs producer nmos serve --no-mdns ...
       network_mode: host        # Linux CI runners only
     amwa-nmos-testing:
       image: amwa/nmos-testing@sha256:<DIGEST>   # pinned per phase
       network_mode: host
       volumes:
         - ./UserConfig.py:/config/UserConfig.py
       depends_on: [dhs-under-test]
       # ports 5000 (UI + API), 5001 (Mock services)
   ```
2. **NTP / chrony on the runner.** Three test groups need clock sync
   between dhs and the AMWA tool:
   - IS-04 Registry Query API pagination tests
   - IS-05 tests #29 + #30 (absolute scheduled activation)
   - IS-08 test #4 (absolute scheduled activation)
   CI provisioning enables `chronyd` against `pool.ntp.org` before the
   compose stack boots.
3. **Make target** `make test-conformance-nmos-<suite>` that:
   - boots the compose stack,
   - drives the AMWA tool's API at `:5000` in non-interactive mode
     pointing at the dhs endpoint,
   - persists the JSON / JUnit-XML report to
     `tests/integration/nmos/<phase>/results/<date>-<suite>.json`.
4. **CI step** runs `make test-conformance-nmos-<suite>` and fails the
   PR if any test reports `Fail` or `Could Not Test` for required
   coverage. `Optional` and `Test Disabled` are allowed.
5. **Result archive** — every passing report is committed under
   `tests/integration/nmos/results/` so the conformance trajectory is
   a first-class repo artefact (mirrors the per-protocol fixture
   policy in `feedback_fixture_dogfood.md`).

## Tool limitations (verified from upstream README)

| Limitation | Impact on dhs CI |
|---|---|
| **Only one Node tested per run.** | Multi-Node testbeds need separate compose stacks. Per-phase test runs use exactly one dhs Node. |
| **mDNS announcements broadcast on the network** unless `ENABLE_DNS_SD=False` or `DNS_SD_MODE='unicast'`. | CI uses `unicast` mode by default; multi-host LAN-integration tests use `multicast` only on isolated lab segments. |
| **No published SemVer release** (only "JT-NM Tested" snapshot tags). | Pin by Docker image digest, not version tag. Bumping the pin is its own `chore(nmos)` PR. |
| **Time sync required** for IS-04 pagination + IS-05 #29-30 + IS-08 #4. | CI runner provisions NTP before the suite. Local-dev `make test-conformance` warns if clock drift > 1 s. |
| **Web UI primary; non-interactive mode docs split across pages.** | dhs CI hits the JSON API on `:5000` directly, not the UI. Each `make test-conformance-nmos-<suite>` target wraps the API call in shell glue. |

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

Each pin is a Docker image digest captured the day the PR is approved:

```
# tests/integration/nmos/02-is09/.amwa-nmos-testing-pin
amwa/nmos-testing@sha256:abc123...
```

Bumping the pin is its own `chore(nmos)` PR that re-runs every prior
phase's suite under the new tool version — analogous to the tsl / osc
cross-impl byte-oracle pattern in
[`feedback_fixture_dogfood.md`](../../../C:/Users/BY-SYSTEMSSRLBoujraf/.claude/projects/c--Users-BY-SYSTEMSSRLBoujraf-Downloads-acp/memory/feedback_fixture_dogfood.md).

The repo has no SemVer releases; "JT-NM Tested August 2022 v1.1" was
the most recent tagged release at time of writing. We track `master`
by digest, not by tag.

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
