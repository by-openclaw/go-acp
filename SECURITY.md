# Security Policy

## Scope

The ACP protocol family is designed for **local broadcast-domain device control**
in broadcast/media environments. It was not designed with security in mind.

## Current status (v1)

- No authentication or TLS -- out of scope for v1
- UDP broadcast on port 2071 -- no encryption, no integrity checks
- TCP direct on port 2071 -- plaintext
- AN2/TCP on port 2072 -- plaintext
- Property values are never persisted to disk
- No credentials are stored or transmitted

## Recommendations for deployment

- Isolate ACP traffic on a dedicated VLAN
- Use firewall rules to restrict access to ports 2071 and 2072
- Do not expose ACP ports to the internet
- See [docs/deployment/README.md](docs/deployment/README.md) for firewall rules

## Reporting vulnerabilities

Report security issues to: yboujraf@by-systems.be

Include:
- Description of the vulnerability
- Steps to reproduce
- Impact assessment

---

Copyright (c) 2026 BY-SYSTEMS SRL - www.by-systems.be
