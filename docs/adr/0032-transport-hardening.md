# ADR-0032 — Transport hardening: fuzzed decoders, gateway as protective proxy

Status: proposed

This ADR is a living document. Add new facts in the Revisions trailer.

## Context

Every wire decoder runs on the first bytes of every datagram or frame
from anybody, before any community, USM or session check. A panic, hang
or out-of-bounds read there is a remote denial of service — the first
thing a scanner probes.

The devices themselves cannot be hardened. A Snell rack, an IRD, a
matrix frame is built for a trusted control LAN; it will not survive a
fuzzer or a malformed-packet flood, and pointing one at live hardware can
brick it. So robustness is our job, on both sides: our code must be proof
against hostile input, and the fragile device must never meet the
attacker at all.

## Decision

### 1. Every wire decoder is fuzzed

Each protocol codec ships Go fuzz targets over its `[]byte` decode
entry points. The decoder must never panic, hang or read out of bounds
on any input; a rejected frame is the expected outcome, not a bug.
Without `-fuzz` the targets run a seed corpus as ordinary regression
tests, so CI exercises them; `go test -fuzz` drives the search. A
crasher is a finding, fixed and added to the corpus.

### 2. Never fuzz live hardware

Fuzzing targets our code and vendor **emulators** only, never a
production device. The point of fuzzing is to prove *we* survive so the
device never has to.

### 3. Gateway as protective proxy

The universal gateway is not only translation and abstraction; it is a
**protocol-aware firewall in front of devices that have no defences of
their own**. Fragile devices are never directly exposed. The gateway is
the only hardened surface, and for every request it validates and
sanitises the input, bounds the response size and the work one request
can cost, rate-limits, and forwards only well-formed, authorised
requests. The device sees only our clean, bounded traffic — exactly the
shape the fuzzing proves we handle. This makes input validation, rate
limiting and the source allowlist gateway *requirements*, not options.

### 4. Silent, non-amplifying responders

A UDP responder answers nothing on malformed or unauthorised input, so a
scanner learns nothing from the difference between a wrong password and a
closed port (RFC 1157 §4.1 for SNMP; the same rule for every protocol).
It caps the work and the response size one request can cause, so it is
never a reflection or amplification vector (e.g. the SNMP GETBULK
repetition cap).

### 5. Resource limits and source allowlist

Every producer bounds frame size, in-flight connections, and read
deadlines, and can restrict the sources it answers to an allowlist bound
to the trusted segment.

## Consequences

- A decoder crash becomes a caught regression, not a field outage.
- The gateway is the security boundary; devices stay on a trusted segment
  and are reachable only through it (today: the DMZ VLAN).
- New surface to own: per-codec fuzz targets, a reflection/amplification
  audit per responder, and a lab suite that runs real tools (onesixtyone,
  nmap NSE, a GETBULK flood) against our agent and emulators — never a
  live device.

## Revisions

- 2026-09-19 — Initial proposal. Captures the fuzz-every-decoder rule,
  the never-fuzz-hardware safety rule, and the gateway-as-protective-proxy
  principle, after fuzzing the SNMP decoders (zero crashes) and the
  observation that a Snell rack survives only because it never meets the
  attacker — the gateway does.
