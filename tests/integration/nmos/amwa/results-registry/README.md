# AMWA NMOS Testing Tool — Registry conformance evidence

`dhs registry nmos serve` scored by the AMWA NMOS Testing Tool, suite
**IS-04-02 "IS-04 Registry APIs"**, once per IS-04 minor.

The suite drives both faces of the Registry: the Registration API that
Nodes POST into, and the Query API + WebSocket subscriptions that
Controllers read from.

## Score — 78.6% coverage, 100% of executed passed, 0 Fail

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that RAN; anything skipped contributes zero to it. Quoting a pass
count alone overstates conformance.

```
applicable          341     (372 total, less 31 Not Applicable)
EXECUTED            268     pass=268 fail=0 warn=0
SKIPPED              73     disabled=64 couldnottest=5 manual=4
COVERAGE           78.6%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`.

Per minor, of what executed:

| Minor | Pass | Fail |
|---|---:|---:|
| v1.0 | 54 | 0 |
| v1.1 | 70 | 0 |
| v1.2 | 70 | 0 |
| v1.3 | 74 | 0 |

### The 73 that did not run

| Count | Why | Real gap? |
|---:|---|---|
| 64 | `ENABLE_AUTH` is False | **yes** — BCP-003-02 Authorization is unimplemented, so this is untested *and* unwritten |
| 5 | `auto_registration_4/5/6`: "No resources found" | **no — tool-side ceiling.** The tool harvests `saved_entities` for these via collection GETs on the Registration API, an endpoint IS-04 does not define (Registration API is POST/DELETE by id). Nothing a conformant registry can serve makes them run. |
| 4 | Manual | needs a human |

## How the run is made hermetic (it was not, twice)

- **LAN mode, not docker bridge.** The tool runs `--network host` so it
  participates in real multicast; the registry's own mDNS announcement
  is what test_01/02 verify. Bridged runs failed exactly those two.
- **A persistent subscription is pre-created per minor** (POST
  `/subscriptions` with `persist:true`) so the by-id GET / DELETE auto
  tests have an entity — otherwise they record Could Not Test.
- **No live devices in the store during scoring.** The EVS Neuron
  auto-registers into this registry (it is pointed here in the lab),
  and a suite run with a real device's 208 senders churning inside the
  store is not reproducible: v1.3 `test_31` flapped 201-vs-200 on
  resource updates in exactly that state. The evidence run parks the
  Neuron on the Cerebrum registry, restarts the store fresh, scores,
  then points the Neuron back.

## What the LAN + WS grains found (both fixed)

**`DELETE /subscriptions/{id}` was missing** — only GET was routed, so
a Controller could never release a subscription. Surfaced as a CORS
complaint (`auto_query_19`: DELETE absent from
`Access-Control-Allow-Methods`, which is generated from the route
table). Fixed in `registry/subscriptions.go` + pinned by
`subscription_delete_test.go`, including the CORS-header form of the
failure.

**PTP clocks lost required fields in Query-WS grains** — the store
re-marshals resources into grain `pre`/`post`, and `omitempty` on the
two boolean fields of `NodeClock` dropped a registered
`"locked": false` / `"traceable": false`, both REQUIRED by
`clock_ptp.json`. The tool reported it as
`test_31: "'ptp' is not one of ['internal']"` — the anyOf validator
matching neither clock branch. `NodeClock.MarshalJSON` now encodes per
branch (all six fields for ptp, exactly `{name, ref_type}` for
internal); pinned by `TestNodeClockMarshalPTPKeepsRequiredFalse`, which
registers the interesting values — the false ones.

## A stray registry can make this whole file lie

While setting this up, a second `dhs registry` was running on another
host announcing the same mDNS instance name. The tool resolved the
name to *that* host and scored a different process than the one under
test. Check `avahi-browse -prt _nmos-register._tcp` resolves to the
host you think it does before trusting a run.

## Interop beyond the tool

The same registry build accepts a real EVS Neuron (firmware
CONVERT Hybrid): full resource parity — 208 senders, 208 receivers,
208 sources/flows — registered via the Registration API v1.3 with
5 s heartbeats, GC-verified. See `internal/amwa/docs/` for the lab
topology (Neuron → pfSense → registry ← Cerebrum).

## Not covered here: the Controller

**IS-04-04 / IS-05-03** evidence lives in `results-controller/` — the
controller is driven through the Testing Facade
(`internal/amwa/facade/`), scored at 100% of its applicable tests.
