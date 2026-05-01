# IS-04-01 — known caveats for this harness

Snapshot of the per-test posture for the AMWA NMOS Testing tool against
dhs running in this docker-compose. Update whenever the cause of a row
changes; the table is the source of truth for what to expect.

## Tests that need human attention

| Test | State | Note |
|---|---|---|
| `test_22` Node resource IDs persist over a reboot | Manual | Spec-mandated manual check. **In this harness `dhs` is rebuilt + the container recreated every iteration**, so IDs are intentionally ephemeral; the AMWA testing tool can never auto-verify this against an ephemeral container. To verify for real: deploy `dhs` outside Docker (or with a writable persistent volume that holds the bundle JSON), reboot the host, and confirm via the Node API that every UUID is identical pre-/post-reboot. |
| `test_16` Failover heartbeat-only | Fail (timing) | Docker Desktop Windows: the AMWA cascade kill loop disables mocks every `HEARTBEAT_INTERVAL+1=6 s`; under that schedule our 5 s heartbeat occasionally hits a freshly-disabled mock and the cascade re-pick triggers a stray POST /resource. Same code on Linux Docker (true host networking) clears it. Track the platform-portability gap, do not patch by lengthening heartbeats — that breaks `test_05`. |
| `test_16_01` Timeout-mock heartbeat | Warning | Same root cause as `test_16` — cascade runs out of clock before reaching registry 5 (5106). |

## Tests that look "Not Implemented" but are intentional

| Test | State | Note |
|---|---|---|
| `auto_node_17..23` (7 rows) | Test Disabled | We advertise `api_auth=false`; IS-10 / TLS is **out of scope for v1**, the tool correctly skips. |
| `test_02`, `test_02_01` | Test Disabled | Tool's `DNS_SD_MODE=multicast`; these are unicast variants. |
| `test_12` | Test Disabled | v1.0/v1.1/v1.2 row, we serve v1.3 only. |

## Steady-state result

`56 Pass / 1 Fail / 1 Warning / 1 Manual` against IS-04-01 v1.3.3, on
amwa/nmos-testing image as of 2026-05-01. Re-run via:

```
docker compose down && docker compose up -d --build
# wait ~10 s for both containers to be healthy
curl -s -X POST http://127.0.0.1:5000/api \
  -H 'Content-Type: application/json' \
  -d '{"suite":"IS-04-01","host":["172.19.0.3"],"port":[18080],"version":["v1.3"],"output":"json"}'
```

## Web UI

`http://127.0.0.1:5000` → drop-down `IS-04-01` → Host `172.19.0.3`,
Port `18080`, Version `v1.3` → Test.
