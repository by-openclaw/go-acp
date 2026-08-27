# AMWA NMOS Testing Tool — Registry conformance evidence

`dhs registry nmos serve` scored by the AMWA NMOS Testing Tool, suite
**IS-04-02 "IS-04 Registry APIs"**, once per IS-04 minor.

The suite drives both faces of the Registry: the Registration API that
Nodes POST into, and the Query API + WebSocket subscriptions that
Controllers read from.

## Score — 236 Pass / 0 Fail

| Minor | Pass | Fail |
|---|---:|---:|
| v1.0 | 47 | 0 |
| v1.1 | 62 | 0 |
| v1.2 | 62 | 0 |
| v1.3 | 65 | 0 |

Remaining states are the tool's own: 64 Test Disabled (HTTPS/auth
rounds, off in this config), 37 Could Not Test, 31 Not Applicable,
4 Manual.

## Run it

The registry must be a **container on the tool's own docker bridge**:

```bash
docker run -d --name dhs-registry --hostname dhs-registry \
  --network dhs-amwa dhs-node:conformance \
  registry nmos serve --bind :8235 --mdns --advertise-host dhs-registry:8235
```

then POST to the tool:

```json
{"suite":"IS-04-02","host":["dhs-registry","dhs-registry"],
 "port":[8235,8235],"version":["v1.3","v1.3"],
 "selector":[null,null],"urlpath":[null,null],"output":"json"}
```

## Two things that made earlier runs lie

Worth recording, because both produced failures that pointed away from
the cause:

**A stray registry won the mDNS name.** A second `dhs registry` was
running on 10.100.0.103 announcing the same instance name,
`dhs-nmos-registry`. The tool resolved the name to *that* host, so it
was scoring a different process than the one under test — reported as
four WebSocket failures (test_22_2, test_23_1, test_24_1, test_31,
"Expected at least one message via WebSocket subscription"). Nothing
was wrong with the subscriptions; the tool was talking to another
registry. All four passed once the stray was stopped.

**mDNS does not reach into a container from the LAN.** Running the
registry on the host (10.100.0.101) left test_01/test_02 failing with
"No matching mDNS announcement found" — the tool runs inside a
container, and multicast reaches it only from its own bridge. This is
the same constraint the Node rounds already document, and the fix is
the same: put the thing under test on that bridge.

## Not covered here: the Controller

**IS-04-04 "IS-04 Controller"** and **IS-05-03** are NOT in this
evidence set, and cannot be run as things stand.

Their first endpoint slot is `testing-facade / testquestion`: the tool
does not drive a controller directly. It POSTs questions to a **Testing
Facade** — a service that receives each question, makes the controller
under test perform the action, and answers back. The tool's second slot
for IS-04-04 has `disable_fields: ["host", "port"]`, because the tool
supplies its own mock registry for the controller to discover.

We have no facade, so these suites are untested rather than passing.
Building one means implementing the AMWA Testing Facade question/answer
protocol and wiring it to `dhs consumer nmos`.
