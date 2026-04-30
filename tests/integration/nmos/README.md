# NMOS — AMWA Conformance Testing harness

Integration tests that drive `dhs` against the **AMWA NMOS Testing**
canonical conformance suite (Docker, Python). Authoritative design
rules live in [`internal/amwa/docs/conformance.md`](../../../internal/amwa/docs/conformance.md);
this README describes the per-suite layout + invocation surface.

## Prerequisites

- Docker daemon reachable from the dhs devcontainer
  (`docker-outside-of-docker` feature on the devcontainer image).
- `dhs` binary built into `bin/dhs` (the devcontainer's
  `postCreateCommand` builds it).
- Network: each suite runs against a private docker-compose bridge.
  Outbound DNS works; inbound from the host LAN does NOT.

## Suites shipped today (one folder each)

| Suite ID | Tests our role | Notes |
|---|---|---|
| `is-04-01` | Node API | IS-04 v1.3 Node provider. |
| `is-04-02` | Registry APIs | IS-04 v1.1/1.2/1.3 Registration + Query. |
| `is-05-01` | Connection Management | IS-05 v1.1 staging + activation. |
| `is-07-01` | Event & Tally | IS-07 v1.0 WS publisher. |
| `is-09-02` | Discovery | IS-09 v1.0 System API. |

Future suites (BCP-008, IS-12, IS-08, IS-04-04 Controller)
land per-phase as the corresponding plugin layer ships. Skipped /
out-of-scope suites (IS-10 Auth, IS-14 Device Config) listed in
`internal/amwa/docs/conformance.md` §"Suite catalogue".

## Per-suite layout

Each suite directory contains:

```
is-04-01/
  docker-compose.yml      isolated bridge, dhs + amwa-nmos-testing
  UserConfig.py           AMWA tool config (DNS-SD mode, target hosts)
  expected.json           list of test IDs that MUST pass for this phase
```

## Invocation

Use the wrapper script — it injects RUN_ID, brings up the bridge,
runs the suite, scrapes JSON results, tears the bridge down (even on
^C / failure):

```
tests/integration/nmos/scripts/run-suite.sh is-04-01
```

Output:

- `tests/integration/nmos/results/<suite>-<RUN_ID>.json` — full AMWA
  Testing JSON dump (every test ID + Pass / Fail / Could-Not-Test).
- Exit code 0 if every entry in `expected.json` was Pass; non-zero
  otherwise (including any new Could-Not-Test that the local
  environment introduced).

## CI gating

The harness ships **disabled-by-default** in CI — the workflow
`.github/workflows/nmos-conformance.yml` runs only when:

- A PR labelled `conformance` is opened, OR
- The workflow is manually dispatched via `gh workflow run`, OR
- A merge to `main` carries the `conformance` label on its tip
  commit.

This keeps the standard PR pipeline fast (the AMWA tool image is
~600 MB; pulling on every PR would dominate CI runtime). Ship a
`conformance` label on any PR that touches NMOS spec coverage; the
result will block merge if any suite degrades.

## Image-digest pinning

`docker-compose.yml` files reference the AMWA tool by SHA256 digest
— never `:latest` — so a re-run of the same suite version always
produces the same Pass / Fail / Could-Not-Test partition. To bump
the digest:

```
docker pull amwa/nmos-testing
docker images --digests amwa/nmos-testing
# Update `image: amwa/nmos-testing@sha256:...` in each suite's
# docker-compose.yml.
# Run every suite once locally to re-baseline expected.json.
```

## Garbage-collection guarantee

Per `internal/amwa/docs/conformance.md`:

- Every `run-suite.sh` invocation registers `trap docker-compose-down
  EXIT` so the bridge + containers + anonymous volumes are destroyed
  on success, failure, OR Ctrl-C.
- Compose project name encodes suite + RUN_ID so concurrent runs
  never collide.
- No persistent volumes; results pulled via the AMWA tool's JSON
  API, never volume-mounted out.
- `docker image prune` runs nightly in CI.
