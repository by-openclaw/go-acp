# AMWA NMOS Testing Tool — Registry conformance evidence

`dhs registry nmos serve` scored by the AMWA NMOS Testing Tool, suite
**IS-04-02 "IS-04 Registry APIs"**, once per IS-04 minor.

The suite drives both faces of the Registry: the Registration API that
Nodes POST into, and the Query API + WebSocket subscriptions that
Controllers read from.

## Score — 69.2% coverage, and 0 failures within it

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that RAN; anything skipped contributes zero to it, so a suite
that executes two thirds of itself still reports "0 Fail". Quoting that
alone overstates conformance.

```
applicable          341     (372 total, less 31 Not Applicable)
EXECUTED            236     pass=236 fail=0 warn=0
SKIPPED             105     disabled=64 couldnottest=37 manual=4
COVERAGE           69.2%
```

Run `python tests/integration/nmos/amwa/coverage.py <results-dir>` to
regenerate this rather than eyeballing pass counts.

Per minor, of what executed:

| Minor | Pass | Fail |
|---|---:|---:|
| v1.0 | 47 | 0 |
| v1.1 | 62 | 0 |
| v1.2 | 62 | 0 |
| v1.3 | 65 | 0 |

### The 105 that did not run

| Count | Why | Real gap? |
|---:|---|---|
| 64 | `ENABLE_AUTH` is False | **yes** — BCP-003-02 Authorization is unimplemented, so this is untested *and* unwritten |
| 37 | "No resources found to perform this test" | **yes, ours** — the registry was near-empty; the run needs a fuller resource set registered first |
| 4 | Manual | needs a human |

So: nothing that ran failed, and roughly a third of the suite did not
run. Both halves are true and only the pair is honest.

## Run it — on the LAN, with real discovery

The registry runs on the LAN (10.100.0.101) and is found by **mDNS**,
not by being co-located with the tool:

```bash
dhs registry nmos serve --bind 0.0.0.0:8235 --mdns \
  --advertise-host 10.100.0.101:8235
```

The tool must be on the **host network** so it participates in LAN
multicast:

```bash
docker run -d --name nmos-testing-lan --network host \
  -v <config-volume>:/config amwa/nmos-testing:latest
```

then POST the run:

```json
{"suite":"IS-04-02","host":["10.100.0.101","10.100.0.101"],
 "port":[8235,8235],"version":["v1.3","v1.3"],
 "selector":[null,null],"urlpath":[null,null],"output":"json"}
```

> An earlier version of this file described running the registry as a
> container on the tool's docker bridge. That was a workaround for the
> tool being walled off from LAN multicast, and it tested the wrong
> thing — the registry must be discoverable over the LAN by mDNS,
> unicast DNS-SD and manual configuration alike. Our announcement is
> visible from other LAN hosts:
>
> ```
> eth0;IPv4;dhs-nmos-registry;_nmos-register._tcp;local;
>   dhs-debian.local;10.100.0.101;8235;"api_ver=v1.0,v1.1,v1.2,v1.3" "pri=0"
> ```
>
> The fix belonged on the tool's side, not ours.

## What the LAN run found that the bridged run did not

**`DELETE /subscriptions/{id}` was not implemented.** Only GET was
registered on the by-id prefix, so a Controller had no way to release a
subscription. IS-04's Query API defines it.

It surfaced several steps from the cause. Our
`Access-Control-Allow-Methods` is generated from the route table, so an
unregistered verb shows up as a CORS complaint:

```
auto_query_19: 'DELETE' not in 'Access-Control-Allow-Methods' CORS header
```

It had gone unnoticed because a non-persistent subscription is *also*
reaped when its WebSocket closes — the common path cleans up without
anyone calling DELETE, so nothing looked broken.

Fixed in `registry/subscriptions.go` (`HandleDeleteByID`, 204 on
success, 404 on unknown id, closes the WebSocket) and registered in
`registry/query.go`. Pinned by `subscription_delete_test.go`, including
a test that asserts the CORS header advertises DELETE — because that is
the form the failure actually took.

## A stray registry can make this whole file lie

While setting this up, a second `dhs registry` was running on
10.100.0.103 announcing the same mDNS instance name,
`dhs-nmos-registry`. The tool resolved the name to *that* host and
scored a different process than the one under test — reported as four
WebSocket failures claiming no subscription messages arrived. The
subscriptions were fine. Check `avahi-browse -prt _nmos-register._tcp`
resolves to the host you think it does before trusting a run.

## Not covered here: the Controller

**IS-04-04 "IS-04 Controller"** and **IS-05-03** are NOT in this
evidence set.

Their first endpoint slot is `testing-facade / testquestion`: the tool
does not drive a controller directly. It POSTs questions to a **Testing
Facade** — a service that receives each question, makes the controller
under test perform the action, and answers back on `answer_uri`. The
tool's second slot for IS-04-04 has `disable_fields: ["host", "port"]`,
because the tool supplies its own mock registry for the controller to
discover.

The suites are real and runnable — 5 tests in IS-04-04 (discover the
registry by unicast DNS-SD, reach the Query API, enumerate Senders and
Receivers across pagination, notice a Sender going offline) and 4 in
IS-05-03 (identify IS-05-controllable Receivers, activate a connection,
disconnect, reflect state from the Query API). Every one of them maps
onto `dhs consumer nmos walk` and `connect`.

`internal/amwa/facade/` implements the facade protocol. It is not yet
wired into a scored run, so these suites are **untested, not passing**.
