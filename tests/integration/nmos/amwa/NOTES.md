# IS-04-01 — known caveats for this harness

Snapshot of the per-test posture for the AMWA NMOS Testing tool against
dhs running in this docker-compose. Update whenever the cause of a row
changes; the table is the source of truth for what to expect.

## Multi-version conformance status (2026-05-02)

`feat/nmos-is04-amwa-conformance` runs all four AMWA-published IS-04
minors per the strict-all-versions rule
(`internal/amwa/CLAUDE.md` Versioning, memory
`feedback_amwa_strict_all_versions`). Each `.env` row of the harness
sets `DHS_API_VER` and the suite's `version` parameter; result JSONs
land in `results/is04-01-v<X>.json`.

| API ver | Pass | Fail | Warning | Manual | Not Impl. | Test Disabled | N/A | Could Not Test | Notes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---|
| **v1.3** | **56** | 1 | 1 | 1 | 1 | 10 | 3 | 1 | round 25 baseline; only Fail is `test_16` cascade-timing under Docker Desktop Windows |
| v1.2 | 34 | 13 | 2 | 1 | 1 | 10 | 4 | 8 | known gaps: v12 codec missing field-gating; watcher api_ver match issues |
| v1.1 | 32 | 14 | 2 | 1 | 1 | 10 | 5 | 8 | same root causes as v1.2 |
| v1.0 | 29 | 14 | 2 | 1 | 1 | 9 | 9 | 8 | v10 codec lands in commit 4bb7f6f; remaining gaps shared with v1.1/v1.2 |

How to reproduce any single round:

```
cd tests/integration/nmos/amwa
DHS_API_VER=v1.3 docker compose up -d --build
# wait ~10 s for both containers to be healthy
curl -s -X POST http://127.0.0.1:5000/api \
  -H 'Content-Type: application/json' \
  -d '{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["v1.3"],"output":"json"}' \
  > results/is04-01-v1.3.json
docker compose stop dhs
DHS_API_VER=v1.2 docker compose up -d dhs
# repeat the curl with version=v1.2, etc.
```

## Known gaps blocking v1.0 / v1.1 / v1.2 from matching v1.3 baseline

These are *missing implementations*, not deferred work — every minor in
the Versioning table must reach v1.3-equivalent conformance. Each gap is
a tracked sub-issue closed by its own commit on this protocol branch.

| Gap | Affects | Sub-issue | Cause |
|---|---|---|---|
| **v12 codec field-gating** | v1.2 round | TODO | `internal/amwa/codec/is04/v12/codec.go` claims "v1.2 = v1.3" but v1.3 added `interfaces[].attached_network_device` (Node), `caps.constraint_sets` + `caps.version` (Receiver). v12 should strip these on encode and reject on decode. |
| **v11 codec field-gating completeness** | v1.1 round | TODO | `nodeV12PlusFields = ["interfaces"]` only; `clocks` was wrong key earlier — needs full audit against v1.1.3 schema, especially Source/Flow per-format fields. |
| **Registry watcher api_ver match logic** | v1.0/v1.1/v1.2 rounds | TODO | When running with `--api-ver=v1.X` (X<3), the watcher logs no Registry-found activity even though AMWA mocks advertise the matching version. Need to verbose-log `pickAPIVer` decisions to confirm whether mocks' `api_ver` TXT is read correctly. |
| **Producer URL routing for non-default api_ver** | v1.0/v1.1/v1.2 Node API responses | TODO | `auto_node_11/12` failures are "Response schema validation error" — JSON shape returned at `/x-nmos/node/v1.X/` is canonical-shape (v1.3) when it should be downcast to the requested minor. Provider needs to look up the codec by URL path version and use that codec's Encode methods, not the canonical struct's. |
| **Node fixture `api.versions` array** | all rounds | resolved 2026-05-02 | `amwa-test-node.json` had `["v1.3"]` hardcoded; updated to `["v1.0","v1.1","v1.2","v1.3"]`. Follow-up: provider should auto-derive from `is04.SupportedVersions()` instead of trusting the fixture. |

## Tests that need human attention

| Test | State | Note |
|---|---|---|
| `test_22` Node resource IDs persist over a reboot | Manual | Spec-mandated manual check. **In this harness `dhs` is rebuilt + the container recreated every iteration**, so IDs are intentionally ephemeral; the AMWA testing tool can never auto-verify this against an ephemeral container. To verify for real: deploy `dhs` outside Docker (or with a writable persistent volume that holds the bundle JSON), reboot the host, and confirm via the Node API that every UUID is identical pre-/post-reboot. |
| `test_16` Failover heartbeat-only (v1.3) | Fail (timing) | Docker Desktop Windows: the AMWA cascade kill loop disables mocks every `HEARTBEAT_INTERVAL+1=6 s`; under that schedule our 5 s heartbeat occasionally hits a freshly-disabled mock and the cascade re-pick triggers a stray POST /resource. Same code on Linux Docker (true host networking) clears it. Track the platform-portability gap, do not patch by lengthening heartbeats — that breaks `test_05`. |
| `test_16_01` Timeout-mock heartbeat (v1.3) | Warning | Same root cause as `test_16` — cascade runs out of clock before reaching registry 5 (5106). |

## Tests that look "Not Implemented" but are intentional

| Test | State | Note |
|---|---|---|
| `auto_node_17..23` (7 rows) | Test Disabled | We advertise `api_auth=false`; IS-10 / TLS is **in scope per the strict-all-versions rule** but tracked under separate Track A composes (BCP-003-01 TLS + BCP-003-02 Auth). Tool correctly skips when ENABLE_AUTH=false. |
| `test_02`, `test_02_01` | Test Disabled | Tool's `DNS_SD_MODE=multicast`; these are unicast variants — covered by the planned `prod-dnssd` deployment shape in the SOW. |
| `test_12` (v1.3 round only) | Test Disabled | Tool says "disabled for Nodes >= v1.3"; the v1.0/v1.1/v1.2 sibling `test_12_01` covers the v1.3 case. |

## Web UI

`http://127.0.0.1:5000` → drop-down `IS-04-01` → Host `dhs-node`,
Port `18080`, Version `v1.0` / `v1.1` / `v1.2` / `v1.3` → Test.
