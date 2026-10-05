# AMWA NMOS Testing Tool — fleet sweep on the released binary

Every scope of `ansible/playbooks/amwa-validate.yml`, run from the
control node against **dhs v0.37.0** — the binary the fleet and the
plant run — on 2026-10-05. The tool is the fleet's pinned
`nmos-testing` on the tooling host. One JSON per catalogue entry,
exactly as the tool wrote it. This folder holds the latest release's
sweep; a new release replaces it.

```bash
cd ansible && ansible-playbook -i inventory/hosts.ini playbooks/amwa-validate.yml
```

## Score — 90.6 % coverage, 0 Fail

**Read the coverage number first.** The tool's `Fail` tally counts only
tests that ran.

```
applicable         2517     (2691 total, less 174 Not Applicable)
EXECUTED           2280     pass=2272 fail=0 warn=8
SKIPPED             237     disabled=207 couldnottest=7 notimpl=1 manual=22
COVERAGE           90.6%
```

Regenerate with `python tests/integration/nmos/amwa/coverage.py <dir>`.

The number counts every window as the tool reported it, so a row a
plain window leaves disabled and its authorization twin scores is
counted once on each side. Of the 193 authorization rows the plain
windows leave disabled, 156 are scored in a twin of this sweep; the 37
that are not are listed below.

## Per entry

| Scope | Entry | Pass | Fail | Warning | Could Not Test |
|---|---|---:|---:|---:|---:|
| registry | IS-04-02 (`IS-04-02-registry.json`) | 74 | 0 | 0 | 2 |
| registry, authorization | IS-04-02-auth | 90 | 0 | 0 | 2 |
| mirror | IS-04-02 through the mirror (`IS-04-02-mirror.json`) | 74 | 0 | 0 | 2 |
| node | IS-04-01 | 60 | 0 | 0 | 0 |
| node, unicast DNS-SD | IS-04-01-unicast | 60 | 0 | 0 | 0 |
| node | IS-04-03 | 17 | 0 | 0 | 0 |
| node, authorization | IS-04-03-auth | 24 | 0 | 0 | 0 |
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
| node | IS-12-01 | 147 | 0 | 0 | 1 |
| node | IS-14-01 | 167 | 0 | 0 | 0 |
| node, authorization | IS-14-01-auth | 174 | 0 | 0 | 0 |
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
| node, TLS | BCP-003-01-tls | 8 | 0 | 0 | 0 |
| controller | IS-04-04 at Query v1.0 / v1.1 / v1.2 / v1.3 | 4 each | 0 | 0 | 0 |
| controller, unicast DNS-SD | IS-04-04-unicast | 5 | 0 | 0 | 0 |
| controller | IS-05-03 at Connection v1.0 / v1.1 / v1.2 | 4 each | 0 | 0 | 0 |
| controller | BCP-006-01-02 at Connection v1.0 / v1.1 / v1.2 | 5 each | 0 | 0 | 0 |
| controller | BCP-007-03-02 at Connection v1.0 / v1.1 / v1.2 | 4 each | 0 | 0 | 0 |

## What did not reach a verdict (237)

| Rows | State | Why | Whose |
|---:|---|---|---|
| 156 | Test Disabled | the authorization rounds of a plain window ("only performed when … 'ENABLE_AUTH' is True") | scored in that suite's `-auth` entry above |
| 37 | Test Disabled | the same rounds of IS-04-01 (7), IS-04-01-unicast (7), IS-09-01 (7) and IS-04-02 through the mirror (16) | **ours** — these four have no authorization twin yet (#1312) |
| 12 | Test Disabled | the multicast rounds in the unicast windows and the unicast rounds in the multicast windows | scored in the other window |
| 2 | Test Disabled | "disabled for Nodes >= v1.3" | the tool's |
| 22 | Manual | fault injection, reboot persistence, visual checks | the fault-injection verify play and the operator |
| 6 | Could Not Test | IS-04-02 `auto_registration_5/6` "No resources found" — the Registration API has no listing for the tool to pick an id from | the tool's |
| 1 | Could Not Test | IS-12-01 `auto_ms05_1.2.1` "Not Implemented" | the tool's |
| 1 | Not Implemented | BCP-003-01 `test_02` — recommended TLS 1.2 suites Go's `crypto/tls` does not carry (ECDSA CBC-SHA256/384, DHE) | the standard library's |

## The eight Warnings

IS-11-01 and IS-11-01-auth, `test_04_03_01/_02` and `test_04_04_01/_02`
each: the tool activates the first receiver of a format against its
reference sender, then reports every other receiver of that format as
"no compatible senders". The fixture keeps its second receivers on
purpose.

## Against the earlier releases

- **v0.35.0, v0.36.0 and v0.36.2** scored the same, entry for entry: 52 entries,
  2 272 Pass, 0 Fail, the same eight Warnings. What changed underneath
  between v0.35.0 and v0.37.0 is the registry — it now serves the document
  a Node registered instead of a re-encoding of it (#1338), and shows a
  resource registered at a later minor on its earlier endpoints,
  translated (#1337) — and the mirror (#1336, #1340, #1351, #1346). IS-04-02
  scores the registry, plain, with authorization and through the
  mirror, exactly as before: 74 / 90 / 74.
- **v0.34.0** scored 40 entries, 1 524 Pass, 86.8 %. v0.35.0 added the
  twelve authorization twins, brought IS-04-02-auth from 88 Pass and 2
  Warnings to 90 Pass (the twin announces its registry at priority 99),
  and scored IS-04-01 at 60 on its first attempt, where a Warning on
  `test_16_01` had needed a retry: the node gave a registry that did
  not answer the operating system's whole connect time-out, and now
  gives it one heartbeat period.

The tool does not exercise what the pairing with nmos-cpp found in
those releases; that evidence is in
[`integration-v0.37.0.md`](integration-v0.37.0.md).
