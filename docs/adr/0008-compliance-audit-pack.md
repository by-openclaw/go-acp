# ADR-0008 — Per-connector COMPLIANCE.md template

Status: accepted

## Context

Customer audits — internal and regulatory — require deterministic
answers about every connector: what ports, what protocols, what data,
what retention, how it maps to NIS2 / ISO 27001 / GDPR / CRA. Today
those answers live in scattered docs or in operators' heads. A
standardised audit pack per connector makes audit prep deterministic
and lets the customer's auditor self-serve.

## Decision

Every connector ships a `internal/<proto>/COMPLIANCE.md` file with the
sections below in this exact order. Missing sections are a CI-blocking
defect.

### Required sections

| # | Heading | Content |
|---|---|---|
| 1 | Ports | inbound + outbound, default + configurable, layer (TCP/UDP/WS/WSS) |
| 2 | Protocols | wire spec citations with section / page / paragraph (e.g. "AMWA IS-04 v1.3.3 §6.3 paging", "Probel SW-P-08 Issue 30 §3.2.3 cmd 04") |
| 3 | Firewall rules | "permit src=any dst=`<host>` dport=`<port>/<proto>`" lines ready to paste into customer config |
| 4 | Network direction | inbound (controller→us), outbound (us→device), or both |
| 5 | Encryption — in transit | TLS support? mandatory? cipher suites? supported TLS versions? |
| 6 | Encryption — at rest | per-state-file: what's encrypted, what isn't, rationale |
| 7 | Data classification | what data the connector handles (configuration, telemetry, PII?) |
| 8 | PII / GDPR | per EU 2016/679: what personal data is processed, retention, right-to-erasure handling |
| 9 | Logs | what's logged; retention; scrubbing rules (e.g. no transport_file SDP, no auth tokens, no full credentials) |
| 10 | NIS2 obligations | per EU 2022/2555: risk-management measures; supply-chain (deps from ADR-0005); incident reporting hooks |
| 11 | ISO 27001 controls | mapping table to relevant controls (A.5 Information security policies, A.8 Asset management, A.12 Operations security, A.14 System acquisition, etc.) |
| 12 | Cyber Resilience Act | per EU 2024/2847 (effective 2027): security-by-design statements, vulnerability handling, secure update mechanism |
| 13 | Audit log | which events the connector emits (license install, license expire, plugin register, plugin drain, config change) |
| 14 | Recovery | crash behaviour, persistent state location, restart idempotency |
| 15 | Multi-OS dep matrix | per ADR-0016: per-OS native lib + version, fallback path, install commands |

### Per-OS dep matrix template (section 15)

| OS | Native lib | Source / version | Pure Go? | CGo? | Stdlib fallback | Install command |
|---|---|---|---|---|---|---|
| Linux | (e.g.) Avahi via DBus | `org.freedesktop.Avahi.Server`, `godbus/dbus/v5` | yes | no | stdlib `net.UDPConn` | `apt install avahi-daemon` |
| macOS | (e.g.) Bonjour | `libSystem dns_sd.h` | (per ADR-0016 conflict) | (depends) | stdlib floor | built-in |
| Windows | (e.g.) Bonjour | `dnssd.dll` | (per ADR-0016 conflict) | (depends) | stdlib floor | install Bonjour Print Services |

### Tooling

- `tools/check-compliance.sh` — verify every connector has a
  `COMPLIANCE.md` with all 15 sections present (regex-checked); CI
  fails on missing section.

## Consequences

- Customer audit prep: one `COMPLIANCE.md` per connector → done.
- Regulatory audits (NIS2, GDPR, ISO 27001) trace to specific
  sections.
- New connectors fill the template; missing fields fail CI.
- Operations team can paste firewall rules straight from the doc.

## Forbidden

- Shipping a connector without a `COMPLIANCE.md`.
- Sections out of order (auditor muscle memory).
- "TBD" entries that ship to main without a tracker issue and ETA.
