# AMWA NMOS Testing Tool — Node conformance evidence

Scored by the AMWA NMOS Testing Tool from the Linux control node:

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-conformance.yml
```

8 suites × every applicable IS-04 minor. 31 runs, one JSON + one node
log each.

## Current score — 76.8% coverage, 99.0% of executed passed, 0 Fail

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that RAN; a skipped test contributes zero to it. Reporting a pass
count without the coverage figure overstates conformance, because
hundreds of applicable tests never reach a verdict at all.

```
applicable         1412     (1550 total, less 138 Not Applicable)
EXECUTED           1085     pass=1074 fail=0 warn=11
SKIPPED             327     disabled=271 couldnottest=36 notimpl=0 manual=20
COVERAGE           76.8%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`.

### Per-suite passes (fail=0 everywhere)

| Suite | v1.0 | v1.1 | v1.2 | v1.3 |
|---|---|---|---|---|
| IS-04-01 Node API | 53 | 56 | 57 | 60 |
| IS-04-03 Node API (peer-to-peer) | 16 | 16 | 16 | 17 |
| IS-05-01 Connection Management | 60 | 60 | 60 | 60 |
| IS-05-02 Interop (IS-04) | — | — | 55 | 56 |
| IS-07-01 Event & Tally | 19 | 19 | 19 | 19 |
| IS-07-02 Interop (IS-04/IS-05) | — | — | — | 52 |
| IS-08-01 Channel Mapping | 31 | 31 | 31 | 31 |
| IS-08-02 Interop (IS-04) | — | 40 | 40 | 41 |
| IS-09-02 System API Discovery | 4 | 4 | 4 | 4 |

IS-07-02 pairs with IS-04 **v1.3 only**, and AMWA's own artifacts force
that floor: `sender.json` at v1.0–v1.2 rejects
`urn:x-nmos:transport:websocket` (oneOf: the four RTP/DASH URNs, or NOT
`^urn:x-nmos:`), and Source `event_type` exists only from v1.3 — yet
the suite's `test_02` cross-references both against the Node API. A
v1.2 pairing can only "pass" by serving what v1.2's own schemas reject,
which IS-04-01 at v1.2 would then fail. The node projection
(`node_project.go`) is the spec-correct side; the earlier harness
pairing was the bug (see `ansible/roles/dhs_amwa_node/defaults/main.yml`).

### The 327 that did not run

| Count | Why | Real gap? |
|---:|---|---|
| 252 | `ENABLE_AUTH` is False | **yes** — BCP-003-02 Authorization is unimplemented |
| 15 | `DNS_SD_MODE` is not 'unicast' | **yes** — unicast DNS-SD is NOT covered by these runs |
| 4 | "Replaced by 'auto' test" or similar | no — superseded |
| 36 | Could Not Test — fixture too thin | **yes, ours** — see below |
| 20 | Manual | needs a human |

The 36 Could Not Test are all fixture gaps, not code gaps:

- IS-08-01/-02 `auto_channelmapping_14/15` — the fixture schedules no
  channel-mapping activations for the tool to observe.
- IS-08-01 `test_13/14/15` — no input forbids a route, prevents
  re-ordering, or constrains by block; the constraint-enforcement code
  paths exist but nothing in the bundle exercises them.
- IS-07-02 `test_06` — no matching resources in the bundle.

### The 11 Warnings

- IS-04-01 `test_12` ×3 (v1.0–v1.2): "no matching mDNS announcement" —
  an artifact of the docker-bridge run mode, where the tool cannot see
  the node's multicast. Not present in LAN-mode runs.
- IS-05-01 `test_27/28` ×8: activation entries set "later than
  expected" — scheduled-activation timing tolerance, worth a look but
  not a spec violation.

## IS-09-02: from 12 Fail to clean sweep

Every earlier run failed IS-09-02 test_01/03/04 on all four minors with
*"Node did not attempt to contact the advertised System API."* The
per-advertisement log line added to `SystemWatcher` turned out to be
the whole story: every observed advert logged `ttl=0`.

`DecodeInstances` never carried the PTR/SRV record TTL into the
`Instance`, so every advertisement the stdlib browser delivered was
indistinguishable from an RFC 6762 §10.1 goodbye — and the watcher
evicted each System API the moment it observed it. The Node literally
saw the suite's mock and instantly forgot it.

Fixed in `internal/amwa/codec/dnssd` (TTL decoded from the PTR record,
SRV as fallback) and `session/dnssd` (Avahi `ItemNew` emits
`DefaultAnnounceTTL`, since the daemon signals goodbyes as `ItemRemove`
— TTL==0 keeps one meaning on every backend). Pinned by
`TestEncodeAnnounceAndDecodeInstances` (TTL survives decode) and
`TestDecodeGoodbyeInstanceTTLZero` (only a goodbye decodes to zero).

## What changed to get here

- **This run's fixture carries the min/full archetypes** — including a
  boot-active ST 2022-7 sender whose dual-leg `a=group:DUP` SDP is what
  IS-05-01's SDP-vs-transport_params comparisons and SDPoker now score
  (a single-media-section SDP for a two-leg sender failed 21 tests
  before `sdpForSender` grew per-leg media sections).
- **dnssd record TTL** (above) — cleared all 12 IS-09-02 fails.
- **IS-07 events seed from the full bundle**, not the minor projection —
  IS-07-01 went from 5 passes below v1.3 to 19 at every minor.
- **IS-07-02 v1.2 pairing retired** per AMWA's own schema floor (above).
- Earlier: v1.1 stripped `channels` from audio Sources
  (`source_audio.json` requires it from v1.1); `schemas.RequiredLeaves`
  plus each minor's `drop_test.go` now catch that class at unit-test
  speed.

## Next gaps, in order

1. Fixture: min/full sender+receiver archetypes with real transport
   params (also fixes the 36 Could Not Test and exercises BCP-004 caps).
2. Unicast DNS-SD run mode for the node harness.
3. BCP-003-02 Authorization (252 disabled tests).
