# AMWA NMOS Testing Tool — Node conformance evidence

Scored by the AMWA NMOS Testing Tool from the Linux control node:

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-conformance.yml
```

8 suites × every applicable IS-04 minor. 31 runs, one JSON + one node
log each.

## Current score — 72.1% coverage, 98.0% of executed passed

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that RAN; a skipped test contributes zero to it. Reporting
"998 pass / 12 fail" without the coverage figure overstates conformance,
because nearly 400 applicable tests never reached a verdict at all.

```
applicable         1412     (1550 total, less 138 Not Applicable)
EXECUTED           1018     pass=998 fail=12 warn=8
SKIPPED             394     disabled=271 couldnottest=78 notimpl=25 manual=20
COVERAGE           72.1%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`.

### The 394 that did not run

| Count | Why | Real gap? |
|---:|---|---|
| 252 | `ENABLE_AUTH` is False | **yes** — BCP-003-02 Authorization is unimplemented |
| 36 | "Replaced by 'auto' test" | no — superseded by the auto_ variants |
| 30 | IS-07 Events API returned no number/string/enum sources | **yes, ours** — the test bundle has no such sources |
| 26 | "No resources found to perform this test" | **yes, ours** — fixture too thin |
| 25 | No BCP-004-01 `constraint_sets` in Receiver caps | **yes, ours** — BCP-004-01 is implemented but not exercised |
| 15 | `DNS_SD_MODE` is not 'unicast' | **yes** — unicast DNS-SD is NOT covered by these runs |
| 20 | Manual | needs a human |

Two worth calling out: **unicast DNS-SD is untested here**, and
**BCP-004-01 caps are implemented but never exercised** because the
test node's Receivers carry no constraint_sets.

## Failure detail — 12 Fail

Every failure is in one suite, **IS-09-02**, 3 on each of the four
minors. Every other suite is clean at every minor:

| Suite | v1.0 | v1.1 | v1.2 | v1.3 |
|---|---|---|---|---|
| IS-04-01 Node API | 45 | 48 | 49 | 59 |
| IS-04-03 Node API (peer-to-peer) | 16 | 16 | 16 | 17 |
| IS-05-01 Connection Management | 60 | 61 | 62 | 61 |
| IS-05-02 Interop (IS-04) | — | — | 55 | 56 |
| IS-07-01 Event & Tally | 5 | 5 | 5 | 19 |
| IS-07-02 Interop (IS-05) | — | — | 43 | 52 |
| IS-08-01 Channel Mapping | 31 | 31 | 31 | 31 |
| IS-08-02 Interop (IS-04) | — | 40 | 40 | 41 |
| **IS-09-02 System API Discovery** | **1 / 3 FAIL** | **1 / 3 FAIL** | **1 / 3 FAIL** | **1 / 3 FAIL** |

## The IS-09-02 failure is harness contention, not Node logic

All three failures on each minor say the same thing: *"Node did not
attempt to contact the advertised System API."*

The Node did contact one. Its log for the run shows it discovering,
reading and applying a System API two seconds after start:

```
provider/node: System API global applied
  url=http://10.100.0.104:8110/x-nmos/system/v1.0/global
```

`10.100.0.104:8110` is the **nmos-cpp reference registry** the
`dhs_amwa` role runs, and it serves an IS-09 System API of its own:

```
$ avahi-browse -aprt | grep _nmos-system
=;br-…;IPv4;nmos-cpp_system_10-100-0-104_8110;_nmos-system._tcp;local;
   nmos-registry.local;10.100.0.104;8110;"pri=100" "api_ver=v1.0" "api_proto=http"
```

So two System APIs are advertised on the bridge during IS-09-02: the
suite's own mock, which the test expects the Node to contact, and the
reference registry's, which the Node found first. IS-09 §3.1 selection
is by advertised priority, and a competing advertisement on the same
link makes the suite's outcome depend on which one wins.

Corroborating this reading: `test_01_01` — *"Node does not attempt to
contact an unsuitable System API"* — **passes** on every minor. The
Node's filtering and selection are working; what it selected was a real
System API that the suite did not put there.

The fix belongs in the harness (keep the reference registry off the
bridge for IS-09-02, or give the suite's mock a priority that wins),
not in the Node. Tracked rather than silently accepted.

## What changed to get here

- **998 vs 980** at the previous run: v1.1 stripped `channels` from
  audio Sources, which `source_audio.json` requires from v1.1 onward.
  Every audio Source failed to register; IS-04-01 v1.1 reported it as
  test_09/10/11/26 "not found in the registry" and IS-08-02 v1.1 as
  "not all Output source IDs were advertised". Nine failures, none
  naming a schema. `schemas.RequiredLeaves` plus each minor's
  `drop_test.go` now catch that class at unit-test speed.
- IS-09-02 previously failed because the Node **refused** a `/global`
  that AMWA's own mock serves without `label`/`description`. It now
  absorbs and applies it, reporting `nmos_is09_global_deviation`. That
  fix is real and visible in the log above — it is simply not enough
  while a second System API is on the link.
