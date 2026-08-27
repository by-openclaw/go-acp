# AMWA NMOS Testing Tool — Controller conformance evidence

`dhs consumer nmos` scored as an NCuT (NMOS Controller under Test) by
the AMWA NMOS Testing Tool: suite **IS-04-04 "IS-04 Controller"** at
every Query API minor, and **IS-05-03 "IS-05 Controller"** at every
Connection API minor.

## Score — 100.0% coverage, 100% of executed passed

```
applicable           28     (46 total, less 18 Not Applicable)
EXECUTED             28     pass=28 fail=0 warn=0
SKIPPED               0
COVERAGE          100.0%
```

| Suite | Minor | Pass | Fail |
|---|---|---:|---:|
| IS-04-04 | v1.0 | 5 | 0 |
| IS-04-04 | v1.1 | 5 | 0 |
| IS-04-04 | v1.2 | 5 | 0 |
| IS-04-04 | v1.3 | 5 | 0 |
| IS-05-03 | conn v1.0 | 4 | 0 |
| IS-05-03 | conn v1.1 | 4 | 0 |

What those tests actually prove: the controller locates the registry by
**unicast DNS-SD** through the tool's mock DNS (test_01 checks the DNS
server's query log, not our word for it), reads the Query API **with
pagination** at a forced page limit of 2, enumerates senders and
receivers correctly, notices a sender going offline and coming back
within the tool's 30-second answer window, identifies IS-05-controllable
receivers, performs and removes immediate activations, and tracks
connection state changes made *behind its back* from Query API state.

## How a controller is scored: the Testing Facade

The tool cannot drive a controller the way it drives a Node — the
controller is the side that does the calling. It POSTs questions
("select the Senders you can see", "perform an immediate activation
between these two") and waits for answers on a callback. A human
normally answers them in a UI.

`dhs consumer nmos facade` (internal/amwa/facade/) answers them by
actually driving `walk`/`connect` against whatever registry the tool
stood up, which makes the run reproducible instead of an operator
exercise. Matching is by **UUID only** — the offered answers and the
`metadata` block carry resource ids precisely so an automated facade
never parses prose. Phrasing is consulted only where the tool encodes
intent nowhere else (offline-selection, the "press Next as soon as you
detect it" monitors, connect-vs-disconnect on identical metadata).

## Run it

Tool on the host network with `DNS_SD_MODE='unicast'` in its
UserConfig (the mock DNS binds :53 on the tool host). Facade:

```bash
dhs consumer nmos facade --bind :5601 --resolver 10.100.0.104 --domain testsuite.nmos.tv
```

Then POST, e.g. IS-04-04 at v1.3 (slot 0 = facade, slot 1 = the tool's
own mock Query API, host/port disabled):

```json
{"suite":"IS-04-04","host":["10.100.0.101",null],"port":[5601,null],
 "version":["v1.0","v1.3"],"selector":[null,null],"urlpath":[null,null],
 "output":"json"}
```

IS-05-03 takes three slots (facade, query, connection), versions
`["v1.0","v1.3","<conn minor>"]`.

## Three bugs this suite forced out

**Controller discovery browsed the wrong service.** The consumer's
`DiscoverMDNS`/`DiscoverUnicast` aliases are the IS-09 helpers and
resolve `_nmos-system._tcp`; `pickQueryInstance` then filtered for
`_nmos-query._tcp` and always came up empty. Registry discovery by
mDNS or unicast DNS-SD could never have worked — only explicit
`--registry` URLs did, which is why the gap survived. IS-04-04 test_01
(discovery through the mock DNS) failed instantly against it. Fixed in
`consumer/controller.go`; the base URL now also prefers the A record
over the SRV hostname, since zone-internal names
(`*.testsuite.nmos.tv`, `.local`) don't resolve through the system
resolver.

**No pagination in the Query client.** `fetchListRaw` fetched one page.
The tool sets the page limit to 2 and checks every page was visited; a
plant-sized registry would have been silently truncated the same way.
`session/query` now follows RFC 8288 `Link: rel="next"` to exhaustion.

**Monitors treated metadata as an instruction.** IS-05-03 test_04's
"press Next as soon as the NCuT detects the connection is inactive"
carries `metadata.receiver` — naming what to *watch*. The facade's
first cut treated any metadata as "act on this" and disconnected the
receiver itself, then answered within the same second ("Connection
still active"). Monitor questions ("as soon as") are now classified
before the action branch, and the watch is scoped to the named
receiver so leftovers from earlier tests can't satisfy it.

## v1.0 needed a spec feature, not a workaround

At v1.0 the tool emits **no pagination headers** — its own mock adds
them "for v1.1 and up", which matches the spec: REST pagination arrived
in IS-04 v1.1. A v1.0 Query API with more resources than its page size
has no REST way to serve the rest. The enumeration mechanism v1.0
actually specifies is the **WebSocket subscription**: POST
/subscriptions, connect to `ws_href`, and the SYNC grains carry the
current state of every matching resource.

`session/query/ws.go` implements that client
(`ListViaSubscription`), and the facade uses it for senders/receivers
when the negotiated minor is v1.0. IS-04-04 test_03/test_04 pass at
v1.0 through `subscription_websockets > 0` — the tool's own criterion
for a v1.0 controller.
