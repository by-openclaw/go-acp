# AMWA NMOS Testing Tool — fleet sweep on the released binary

Every scope of `ansible/playbooks/amwa-validate.yml`, run from the
control node against **dhs v0.34.0** — the binary the fleet and the
plant run — on 2026-10-04. The tool is the fleet's pinned
`nmos-testing` on the tooling host. One JSON per catalogue entry,
exactly as the tool wrote it. This folder holds the latest release's
sweep; a new release replaces it.

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-validate.yml
```

## Score — 86.8 % coverage, 0 Fail

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that ran.

```
applicable         1763     (1901 total, less 138 Not Applicable)
EXECUTED           1530     pass=1524 fail=0 warn=6
SKIPPED             233     disabled=207 couldnottest=7 notimpl=1 manual=18
COVERAGE           86.8%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`
(leave `IS-04-01.json` out — see "The retried entry").

## Per entry

| Scope | Entry | Pass | Fail | Warning | Could Not Test |
|---|---|---:|---:|---:|---:|
| registry | IS-04-02 (`IS-04-02-registry.json`) | 74 | 0 | 0 | 2 |
| registry, authorization | IS-04-02-auth | 88 | 0 | 2 | 2 |
| mirror | IS-04-02 through the mirror (`IS-04-02-mirror.json`) | 74 | 0 | 0 | 2 |
| node | IS-04-01 (`IS-04-01-retry.json`) | 60 | 0 | 0 | 0 |
| node, unicast DNS-SD | IS-04-01-unicast | 60 | 0 | 0 | 0 |
| node | IS-04-03 | 17 | 0 | 0 | 0 |
| node | IS-05-01 | 62 | 0 | 0 | 0 |
| node, authorization | IS-05-01-auth | 69 | 0 | 0 | 0 |
| node | IS-05-02 | 56 | 0 | 0 | 0 |
| node | IS-07-01 | 19 | 0 | 0 | 0 |
| node | IS-07-02 | 53 | 0 | 0 | 0 |
| node | IS-08-01 | 36 | 0 | 0 | 0 |
| node | IS-08-02 | 43 | 0 | 0 | 0 |
| node | IS-09-01 | 5 | 0 | 0 | 0 |
| node | IS-09-02 | 4 | 0 | 0 | 0 |
| node, unicast DNS-SD | IS-09-02-unicast | 4 | 0 | 0 | 0 |
| node | IS-11-01 | 107 | 0 | 4 | 0 |
| node | IS-12-01 | 147 | 0 | 0 | 1 |
| node | IS-14-01 | 167 | 0 | 0 | 0 |
| node | BCP-005-01-01 | 16 | 0 | 0 | 0 |
| node | BCP-006-01-01 | 23 | 0 | 0 | 0 |
| node | BCP-006-04 | 21 | 0 | 0 | 0 |
| node | BCP-007-03-01 | 55 | 0 | 0 | 0 |
| node | BCP-008-01-01 | 98 | 0 | 0 | 0 |
| node | BCP-008-02-01 | 98 | 0 | 0 | 0 |
| node, TLS | BCP-003-01-tls | 8 | 0 | 0 | 0 |
| controller | IS-04-04 at Query v1.0 / v1.1 / v1.2 / v1.3 | 4 each | 0 | 0 | 0 |
| controller, unicast DNS-SD | IS-04-04-unicast | 5 | 0 | 0 | 0 |
| controller | IS-05-03 at Connection v1.0 / v1.1 / v1.2 | 4 each | 0 | 0 | 0 |
| controller | BCP-006-01-02 at Connection v1.0 / v1.1 / v1.2 | 5 each | 0 | 0 | 0 |
| controller | BCP-007-03-02 at Connection v1.0 / v1.1 / v1.2 | 4 each | 0 | 0 | 0 |

## What did not reach a verdict (233)

| Rows | State | Why | Whose |
|---:|---|---|---|
| 193 | Test Disabled | "only performed when … 'ENABLE_AUTH' is True" — the authorization rounds of every suite sat in a plain window | **ours** — only IS-04-02 and IS-05-01 have an authorization twin today (#1312) |
| 12 | Test Disabled | the multicast rounds in the unicast windows and the unicast rounds in the multicast windows | scored in the other window |
| 2 | Test Disabled | "disabled for Nodes >= v1.3" | the tool's |
| 18 | Manual | fault injection, reboot persistence, visual checks | the fault-injection verify play and the operator |
| 6 | Could Not Test | IS-04-02 `auto_registration_5/6` "No resources found" — the Registration API has no listing for the tool to pick an id from | the tool's |
| 1 | Could Not Test | IS-12-01 `auto_ms05_1.2.1` "Not Implemented" | the tool's |
| 1 | Not Implemented | BCP-003-01 `test_02` — recommended TLS 1.2 suites Go's `crypto/tls` does not carry (ECDSA CBC-SHA256/384, DHE) | the standard library's |

## The six Warnings

- IS-11-01 `test_04_03_01/_02`, `test_04_04_01/_02`: the tool activates
  the first receiver of a format against its reference sender, then
  reports every other receiver of that format as "no compatible
  senders". The fixture keeps its second receivers on purpose.
- IS-04-02-auth `test_01`, `test_02`: the authorization twin announces
  its registry at a development priority; the plain window announces at
  99 and scores a Pass. Ours to fix in the role (#1312).

## The retried entry

`IS-04-01.json` is the first attempt: 59 Pass and one Warning,
`test_16_01` "Node never made contact with registry 5 advertised on
port 5106". The catalogue allows this entry one retry in a fresh
window, on the ground that the lab's real devices register with the
tool's mock registries on the shared link; the retry
(`IS-04-01-retry.json`) scored 60 and is the one counted above. The
same first-attempt Warning was drawn on v0.33.0 — whether it is the
third party or the node is not proven yet (#1312).
