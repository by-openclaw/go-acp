# AMWA NMOS conformance harness

The AMWA NMOS Testing Tool is the external arbiter for our node, our
registry and our controller. Our own `profile` verb is not a substitute
for it: it asserts what we believe the spec says, which is exactly the
thing under test.

## Two ways to run it

**Fleet (permanent).** `ansible/playbooks/amwa-tools.yml` keeps the tool
and the nmos-cpp reference implementations running on `dhs-tools`
(`.104`). See `ansible/roles/dhs_amwa/`. That stack is also the
spec-correct PEER our consumer is measured against — a registry and a
node that track the spec, so "the peer is wrong" can be told apart from
"we are".

**This directory (local).** A `docker compose` harness that puts dhs
and the tool on ONE bridge, which is the only shape where the IS-04
mDNS discovery rounds can pass: multicast flows between containers on a
shared bridge, and a node reached across a routed boundary is only
discoverable in the spec's unicast mode.

```
DHS_API_VER=v1.3 docker compose up -d --build
curl -s -X POST http://127.0.0.1:5000/api \
  -H 'Content-Type: application/json' \
  -d '{"suite":"IS-04-01","host":["dhs-node"],"port":[18080],"version":["v1.3"],"output":"json"}' \
  > results/is04-01-v1.3.json
```

`Dockerfile.dhs` expects the cross-compiled binary at `./dhs`
(gitignored — build it, don't commit it):

```
GOOS=linux GOARCH=amd64 go build -o tests/integration/nmos/amwa/dhs ./cmd/dhs
```

## Why avahi is in the image

dhs delegates DNS-SD to a system daemon where one exists (#194). Without
it the stdlib fallback's 500 ms read-deadline jitter fails the cascade
-timing rounds (test_05/15/16) — the tests that measure whether
discovery is handled by a real responder or by polling. nmos-cpp does
the same via Bonjour. The stdlib path stays the floor and is never
removed; it just scores worse, and that difference is the point.

## results/

Committed, not scratch. A conformance number is only meaningful against
the run that produced it, so the JSON stays next to the code it scored.
`NOTES.md` carries the per-test posture: which rows are real gaps, which
are environmental, and which the tool disables on purpose.

Current baseline is 2026-05-02 and covers IS-04-01 only. Nothing has
ever been run against our REGISTRY (IS-04-02) or our CONTROLLER
(IS-04-04), nor any of IS-05/07/08/09/12 or the BCPs. That gap is the
work, and the tool on `.104` exposes 26 suites to close it against.