# Security Policy

## Scope

`dhs` connects to broadcast-control protocols designed for **local
broadcast-domain device control** in broadcast / media environments.
Most of these protocols (ACP1, ACP2, Probel SW-P-08 / SW-P-02, TSL UMD)
were not designed with authentication, integrity, or confidentiality
in mind.

Per-protocol security posture is documented in each
`internal/<proto>/COMPLIANCE.md` (per ADR-0008).

## Current posture (cross-cutting)

- Wire payloads on legacy device-control protocols are plaintext
  (no auth / no encryption / no integrity) — this is a property of the
  underlying protocol, not of `dhs`
- License and signing keys never appear in source, logs, or traces
  (per **ADR-0003** + **ADR-0010**)
- Property values cached on disk are treated as **stale until confirmed
  live** — never trusted as current state (see `CLAUDE.md`)
- No credentials are stored at the device-protocol layer

## Recommendations for deployment

- Isolate device-control traffic on a dedicated VLAN
- Use firewall rules to restrict access to per-protocol ports
- Do not expose device-control ports to the internet
- See [docs/deployment/](docs/deployment/) for cross-compile and firewall guidance

## Reporting vulnerabilities

Report security issues to: yboujraf@by-systems.be

Include:

- Description of the vulnerability
- Steps to reproduce
- Impact assessment

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
