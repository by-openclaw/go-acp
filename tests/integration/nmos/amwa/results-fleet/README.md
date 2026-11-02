# AMWA NMOS Testing Tool — fleet sweep on the released binary

Every scope of `ansible/playbooks/amwa-validate.yml`, run from the
control node against **dhs v0.40.2** — the binary the fleet and the
plant run — on 2026-10-06. The tool is the fleet's pinned
`nmos-testing` on the tooling host. One JSON per catalogue entry,
exactly as the tool wrote it. This folder holds the latest release's
sweep; a new release replaces it.

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-validate.yml
```

## Score — 77 entries, 3 768 Pass, 0 Fail, coverage 90.1 %

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that ran.

```
applicable         4196     (4503 total, less 307 Not Applicable)
EXECUTED           3782     pass=3768 fail=0 warn=14
SKIPPED             414     disabled=353 couldnottest=15 notimpl=1 manual=45
COVERAGE           90.1%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`.

The number counts every window as the tool reported it, so a row a
plain window leaves disabled and its authorization twin scores is
counted once on each side. Of the 311 authorization rows the plain
windows leave disabled, 304 are scored in a twin of this sweep.

## Per entry

| Scope | Entry | Pass | Fail | Warning | Could Not Test |
|---|---|---:|---:|---:|---:|
| registry | IS-04-02-registry | 74 | 0 | 0 | 2 |
| registry, at v1.2 | IS-04-02-registry-v1.2 | 70 | 0 | 0 | 1 |
| registry, at v1.1 | IS-04-02-registry-v1.1 | 70 | 0 | 0 | 1 |
| registry, at v1.0 | IS-04-02-registry-v1.0 | 54 | 0 | 0 | 1 |
| registry, authorization | IS-04-02-auth | 90 | 0 | 0 | 2 |
| registry, at v1.2, authorization | IS-04-02-registry-v1.2-auth | 86 | 0 | 0 | 1 |
| registry, at v1.1, authorization | IS-04-02-registry-v1.1-auth | 86 | 0 | 0 | 1 |
| registry, at v1.0, authorization | IS-04-02-registry-v1.0-auth | 70 | 0 | 0 | 1 |
| mirror | IS-04-02-mirror | 74 | 0 | 0 | 2 |
| mirror, authorization | IS-04-02-mirror-auth | 90 | 0 | 0 | 2 |
| node | IS-04-01 | 60 | 0 | 0 | 0 |
| node, at v1.2 | IS-04-01-v1.2 | 58 | 0 | 0 | 0 |
| node, at v1.1 | IS-04-01-v1.1 | 57 | 0 | 0 | 0 |
| node, at v1.0 | IS-04-01-v1.0 | 52 | 0 | 2 | 0 |
| node, authorization | IS-04-01-auth | 67 | 0 | 0 | 0 |
| node, at v1.2, authorization | IS-04-01-v1.2-auth | 65 | 0 | 0 | 0 |
| node, at v1.1, authorization | IS-04-01-v1.1-auth | 64 | 0 | 0 | 0 |
| node, at v1.0, authorization | IS-04-01-v1.0-auth | 59 | 0 | 2 | 0 |
| node, unicast DNS-SD | IS-04-01-unicast | 60 | 0 | 0 | 0 |
| node, at v1.2, unicast DNS-SD | IS-04-01-unicast-v1.2 | 57 | 0 | 1 | 0 |
| node, unicast DNS-SD, authorization | IS-04-01-unicast-auth | 67 | 0 | 0 | 0 |
| node, at v1.2, unicast DNS-SD, authorization | IS-04-01-unicast-v1.2-auth | 64 | 0 | 1 | 0 |
| node | IS-04-03 | 17 | 0 | 0 | 0 |
| node, at v1.2 | IS-04-03-v1.2 | 16 | 0 | 0 | 0 |
| node, at v1.1 | IS-04-03-v1.1 | 16 | 0 | 0 | 0 |
| node, at v1.0 | IS-04-03-v1.0 | 16 | 0 | 0 | 0 |
| node, authorization | IS-04-03-auth | 24 | 0 | 0 | 0 |
| node, at v1.2, authorization | IS-04-03-v1.2-auth | 23 | 0 | 0 | 0 |
| node, at v1.1, authorization | IS-04-03-v1.1-auth | 23 | 0 | 0 | 0 |
| node, at v1.0, authorization | IS-04-03-v1.0-auth | 23 | 0 | 0 | 0 |
| node, TLS | BCP-003-01-tls | 8 | 0 | 0 | 0 |
| node | BCP-005-01-01 | 16 | 0 | 0 | 0 |
| node, authorization | BCP-005-01-01-auth | 23 | 0 | 0 | 0 |
| node | BCP-006-01-01 | 23 | 0 | 0 | 0 |
| node, authorization | BCP-006-01-01-auth | 30 | 0 | 0 | 0 |
| node | BCP-006-04 | 21 | 0 | 0 | 0 |
| node, authorization | BCP-006-04-auth | 28 | 0 | 0 | 0 |
| node | BCP-007-03-01 | 55 | 0 | 0 | 0 |
| node, authorization | BCP-007-03-01-auth | 69 | 0 | 0 | 0 |
| node | BCP-008-01-01 | 98 | 0 | 0 | 0 |
| node | BCP-008-02-01 | 98 | 0 | 0 | 0 |
| node | IS-05-01 | 62 | 0 | 0 | 0 |
| node, authorization | IS-05-01-auth | 69 | 0 | 0 | 0 |
| node | IS-05-02 | 56 | 0 | 0 | 0 |
| node, authorization | IS-05-02-auth | 70 | 0 | 0 | 0 |
| node | IS-07-01 | 19 | 0 | 0 | 0 |
| node, authorization | IS-07-01-auth | 26 | 0 | 0 | 0 |
| node | IS-07-02 | 53 | 0 | 0 | 0 |
| node, authorization | IS-07-02-auth | 74 | 0 | 0 | 0 |
| node | IS-08-01 | 36 | 0 | 0 | 0 |
| node, authorization | IS-08-01-auth | 43 | 0 | 0 | 0 |
| node | IS-08-02 | 43 | 0 | 0 | 0 |
| node, authorization | IS-08-02-auth | 57 | 0 | 0 | 0 |
| node | IS-09-01 | 5 | 0 | 0 | 0 |
| node | IS-09-02 | 4 | 0 | 0 | 0 |
| node, unicast DNS-SD | IS-09-02-unicast | 4 | 0 | 0 | 0 |
| node | IS-11-01 | 107 | 0 | 4 | 0 |
| node, authorization | IS-11-01-auth | 128 | 0 | 4 | 0 |
| node, one receiver per format | IS-11-01-single | 111 | 0 | 0 | 0 |
| node, authorization, one receiver per format | IS-11-01-single-auth | 132 | 0 | 0 | 0 |
| node | IS-12-01 | 147 | 0 | 0 | 1 |
| node | IS-14-01 | 167 | 0 | 0 | 0 |
| node, authorization | IS-14-01-auth | 174 | 0 | 0 | 0 |
| controller | BCP-006-01-02-conn-v1.0 | 5 | 0 | 0 | 0 |
| controller | BCP-006-01-02-conn-v1.1 | 5 | 0 | 0 | 0 |
| controller | BCP-006-01-02-conn-v1.2 | 5 | 0 | 0 | 0 |
| controller | BCP-007-03-02-conn-v1.0 | 4 | 0 | 0 | 0 |
| controller | BCP-007-03-02-conn-v1.1 | 4 | 0 | 0 | 0 |
| controller | BCP-007-03-02-conn-v1.2 | 4 | 0 | 0 | 0 |
| controller | IS-04-04-query-v1.0 | 4 | 0 | 0 | 0 |
| controller | IS-04-04-query-v1.1 | 4 | 0 | 0 | 0 |
| controller | IS-04-04-query-v1.2 | 4 | 0 | 0 | 0 |
| controller | IS-04-04-query-v1.3 | 4 | 0 | 0 | 0 |
| controller, unicast DNS-SD | IS-04-04-unicast | 5 | 0 | 0 | 0 |
| controller | IS-05-03-conn-v1.0 | 4 | 0 | 0 | 0 |
| controller | IS-05-03-conn-v1.1 | 4 | 0 | 0 | 0 |
| controller | IS-05-03-conn-v1.2 | 4 | 0 | 0 | 0 |

The sweep was run twice on v0.40.2. The second run, with the baselines
of this page committed, ended on "at or above baseline, 0 failures" for
every scope.

## What did not reach a verdict (414)

| Rows | State | Why | Whose |
|---:|---|---|---|
| 304 | Test Disabled | the authorization rounds of a plain window ("only performed when … 'ENABLE_AUTH' is True") | scored in that entry's `-auth` twin above |
| 7 | Test Disabled | the same rounds of IS-09-01 | the tool's — its armed instance does not sit this suite (#1312) |
| 30 | Test Disabled | the multicast rounds in the unicast windows and the unicast rounds in the multicast windows | scored in the other window |
| 12 | Test Disabled | "disabled for Nodes >= v1.3" (4) and "disabled for Nodes < v1.3" (8) | scored at the other minor: `test_12` at v1.0 – v1.2, `test_12_01` at v1.3 |
| 45 | Manual | fault injection, reboot persistence, visual checks | the manual-rows play and the reboot play, see [`integration-v0.40.2.md`](integration-v0.40.2.md) |
| 8 | Could Not Test | IS-04-02 `auto_registration_5/6` "No resources found" — the Registration API has no listing for the tool to pick an id from | the tool's (nmos-cpp scores the same) |
| 6 | Could Not Test | IS-04-02 `auto_registration_4`, the same cause, at v1.0 – v1.2 | the tool's |
| 1 | Could Not Test | IS-12-01 `auto_ms05_1.2.1` "Not Implemented" | the tool's |
| 1 | Not Implemented | BCP-003-01 `test_02` — recommended TLS 1.2 suites Go's `crypto/tls` does not carry (ECDSA CBC-SHA384, DHE); every suite it does carry is offered and handshake-tested | the standard library's |

## The fourteen Warnings

- **Eight**, IS-11-01 and IS-11-01-auth, `test_04_03_01/_02` and
  `test_04_04_01/_02`: the tool activates the first receiver of a
  format against its reference sender, then reports every other
  receiver of that format as "no compatible senders". The same node
  with one receiver per format (`IS-11-01-single`, and its twin) scores
  the four rounds: 111 and 132 Pass, no Warning.
- **Four**, IS-04-01 at v1.0 and its twin, `test_24` and `test_24_01`:
  the tool asks a video Source and Flow for `grain_rate`, an attribute
  IS-04 v1.0 does not define (it arrives in v1.1); the suite does not
  gate the two rounds on the minor.
- **Two**, IS-04-01 unicast at v1.2 and its twin, `test_12`: the tool
  looks on mDNS for the announce of a Node that runs unicast DNS-SD and
  so has none ("will not affect operation in registered mode").

## Against the earlier releases

- **v0.37.0** scored 52 entries, 2 272 Pass, 0 Fail, 90.6 %. Since then
  the catalogue gained the IS-04 suites at every earlier minor (Node,
  peer-to-peer Node and Registry at v1.0, v1.1 and v1.2, plain and with
  authorization), IS-04-01 with authorization, the mirror with
  authorization and IS-11 with one receiver per format: 25 entries.
- Scoring the earlier minors found defects that v1.3 alone never
  showed, all fixed before this sweep: the announce of a registered
  Node before v1.3 (#1380), its address record (#1384), a Source lost
  in the narrowing to v1.0 (#1386), and the last one — a replaced TXT
  record left readable in mDNS caches, and direct questions for an
  instance unanswered (#1413) — which kept `test_12` failing at
  v1.0 – v1.2 until v0.40.2.
- **v0.35.0, v0.36.0 and v0.36.2** scored as v0.37.0, entry for entry.

The tool does not exercise what the pairings found in those releases;
that evidence is in [`integration-v0.40.2.md`](integration-v0.40.2.md).
