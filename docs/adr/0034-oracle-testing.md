# ADR-0034 — Oracles: what an independent implementation must prove

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

Unit tests assert that our code does what we think the spec says. They
cannot tell us that what we think the spec says is what the field
implements — the assertion and the implementation were written by the
same person, from the same reading, on the same afternoon.

The gap is not theoretical. The SNMP connector shipped an agent that
answered every v3 failure with `usmStatsUnknownEngineIDs`. Every unit
test passed, because every unit test agreed with the code. RFC 3414
§3.2 requires the counter that matches the failure, and §4's time
synchronisation is built on one of them, so the bug meant that no
manager in the plant could ever recover from our agent rebooting, and
none could tell that from a wrong password. It was found in the first
minute of pointing `snmpget` at us, and the symptom was a message
net-snmp prints and our own manager could not have produced.

ADR-0025 already requires a "vendor emulator + real device — never our
own provider" for integration. What it does not say is what counts as
an oracle, what an oracle must be pointed at, or what happens when the
oracle and the spec disagree. This ADR fills that in, and mostly
codifies what the mature connectors already do: Probel against Commie
and VSM, Ember+ against TinyEmber+, NMOS against AMWA's own suite,
SNMP against net-snmp and the IRDs.

## Decision

### 1. An oracle is an independent implementation, run as a process

Never linked into our build. A test that imports somebody's Go library
compares two Go structs; a test that runs their binary compares
datagrams, which is the thing a device will send us. Running it as a
process also keeps it out of `go.mod` entirely — an oracle is a test
fixture on a host, not a dependency, and ADR-0005's build-graph rule is
untouched by it.

This is a hard rule for the same reason ADR-0006 exists: the moment a
third-party implementation is in the build, it is in the product.

### 2. An oracle is evidence, not authority

When the oracle and the spec disagree, **the spec wins** and the
difference is a compliance event, exactly as a device's deviation is
(root CLAUDE.md, "Spec-strict, no-workaround posture"). Without this
rule the oracle becomes the spec by default and we quietly reimplement
somebody else's bugs.

The exception is the one already written down: where every shipping
implementation contradicts the spec, the documented criteria for that
exception apply, and the compliance event names it.

### 3. Both roles, or it proves half of what you think

Every connector is measured in both directions:

- our **consumer** against their **provider**;
- our **provider** against their **consumer**.

Only the second direction found the SNMP counter bug, because the bug
was in what we *emit*, and nothing that only polls can see that.

Where a connector has no producer — because it is parked, or the
protocol has no inbound role — the direction that does not exist is
recorded as such in the connector's `CLAUDE.md`, not silently skipped.

### 4. Negative paths are mandatory

The oracle must be pointed at the failures, not only the happy path:
a wrong password, an expired or wrong-hostname certificate, an
unacknowledged notification, a refused SET, a version the peer does
not speak. Silent failure lives there, and a happy-path oracle test is
precisely blind to it.

The assertion is on what the oracle *says*, not just that it failed:
"snmpget: Authentication failure (incorrect password, community or
key)" proves our Report carried the right counter. "It returned
non-zero" proves nothing.

### 5. Pin the oracle and record its version

A passing run states what it passed against. "Works with net-snmp" is
not a result; "net-snmp 5.9.3" is. The version goes in the play's
output and in the connector's docs, so a later disagreement can be
attributed to a change on either side.

### 6. Install and drive oracles with Ansible, on the lab control node

Never on the devices. The install is idempotent and confined to the
control node or a CI runner; a device is polled and captured, never
made into a test harness.

### 7. Loopback oracle tests run in CI

This is the payoff. Most oracles are ordinary packages — net-snmp,
curl, openssl, websocat — and most of our protocols can be exercised
end to end on 127.0.0.1 between our own binary and theirs. Those tests
do not need the plant, so they belong in CI as a gate on every PR,
alongside the unit floors, not in a play somebody remembers to run.

Oracle tests that need a real device or a vendor emulator stay
Ansible-driven and outside CI, as ADR-0025 already has them.

### 8. Where no independent implementation exists, say so

Our own DHS-MIB, our canonical tree, a vendor protocol nobody else
implements: there is no oracle, and inventing ceremony does not create
one. The honest substitute is the spec plus committed captures
(ADR-0025 deliverable 6), and the connector's `CLAUDE.md` records that
this is the case and why.

## The oracle per transport

The list a new connector starts from. It is a default, not a closed
set — a protocol's own reference implementation beats a generic tool
whenever one exists.

| Transport / concern | Oracle | What it is for |
|---|---|---|
| Any datagram or stream | `tshark` | the dissector every plugin already ships, used to VERIFY rather than merely exist |
| SNMP v1/v2c/v3 | net-snmp (`snmpget`, `snmpwalk`, `snmpinform`, `snmptrapd`, `snmpd`); `snmpsim` for scale | what every NMS in the field is built on |
| HTTP / HTTPS | `curl`, plus `openssl s_client` for the handshake | status codes are the easy half; certificate and TLS-version failures are the half that hides |
| WebSocket / WSS | **Autobahn\|Testsuite**, `websocat` for ad-hoc | Autobahn is the industry conformance suite for WS — several hundred cases covering framing, fragmentation, UTF-8 and close codes |
| TLS posture | `testssl.sh` | cipher, version and expiry regressions |
| mDNS / DNS-SD | `avahi-browse`, `dns-sd` | discovery is exactly where two implementations quietly disagree |
| AMWA NMOS | AMWA `nmos-testing` | the model this ADR generalises |
| Ember+ | TinyEmber+, EmberPlusView | already in `internal/emberplus/assets/` |
| Probel SW-P-08/02 | Commie, Lawo VSM | already the tier-3 oracles |

## Consequences

- ADR-0025 deliverable 3 gains a definition: an integration test that
  only drives our own provider does not satisfy it, and a connector
  with an oracle available in only one direction says so.
- Some oracle tests move INTO CI, which raises the bar on every PR for
  the protocols that can run on loopback.
- A new connector's scope document names its oracle(s) up front, or
  records that none exists.
- Installing oracles is a fleet change, which means it is an Ansible
  change, reviewed like any other.

## What this ADR does NOT change

- ADR-0005 — nothing here enters the build graph; an oracle is a
  process on a host.
- ADR-0006 — codecs stay stdlib-only.
- ADR-0025 — the tiers and the six deliverables stand; this ADR says
  what counts as the oracle inside them.
- The spec-strict posture — an oracle never overrules the spec.

## Revisions

| Date | Change |
|---|---|
| 2026-09-24 | Written, from the SNMP v3 work: net-snmp as oracle found a `usmStats` counter bug that every unit test agreed with. |
