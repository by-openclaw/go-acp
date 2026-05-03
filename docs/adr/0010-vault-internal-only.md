# ADR-0010 — Vault internal-only — never public

Status: accepted

## Context

dhs uses HashiCorp Vault for two purposes:

1. **Transit engine** holds the Ed25519 license-signing private key
   (per ADR-0003). The signer service calls Vault to sign JWTs; the
   private key never leaves Vault.
2. **KV v2 engine** holds runtime secrets (e.g. customer device
   credentials accessed by connectors that integrate with the
   customer's own Vault deployment).

A Vault server is the single highest-value target in our infrastructure:
compromise yields the ability to forge licenses indefinitely and
exfiltrate customer secrets.

## Decision

Vault servers operated by BY-SYSTEMS for license signing MUST NEVER be
exposed to the public internet. Customer-facing license operations
(refresh, install, demo activation) go through public-facing
intermediaries (Odoo per ADR-0011) which talk to internal Vault over
firewalled internal networks.

### Topology

```
Public internet
    │
    ▼
Odoo  (HTTPS, behind WAF)
    │   • customer portal
    │   • license refresh route
    │   • subscription / billing
    │
    │  internal-only links (Proxmox VLAN, firewalled)
    ▼
Signer service ─────▶ Vault (Transit + KV)   ◀─── only signer + named ops admins
                                │
                                └─ Ed25519 license signing key (Transit, never leaves Vault)
                                └─ ops secrets (KV)
```

### Network segmentation

| Zone | Hosts |
|---|---|
| DMZ (public) | Odoo, mail, license-refresh public endpoint |
| App tier (internal) | Signer service, build pipeline |
| Secrets tier (internal, tightest firewall) | Vault — reachable only from signer + named ops admin IPs |

### Auth from signer to Vault

AppRole. RoleID + SecretID delivered out-of-band (or wrapped via
Vault's response-wrapping). Token policy `dhs-signer` minimum
privileges:

```hcl
path "transit/sign/dhs-license"  { capabilities = ["update"] }
path "transit/keys/dhs-license"  { capabilities = ["read"] }
```

No other Vault paths reachable from the signer's token.

### Rotation

Annual rotation by default (per ADR-0003); immediate rotation on
incident.

## Consequences

- Customers never reach Vault directly — they reach Odoo's
  license-refresh route, which talks internally to Vault.
- Vault compromise requires perimeter breach + Odoo or signer breach
  first — defence in depth.
- Audit log is centralised in Vault; every sign call is recorded
  with caller identity.

## Forbidden

- Exposing Vault to the public internet, ever.
- Granting public CIDR ranges access in Vault listener config.
- Embedding Vault tokens in customer-facing artefacts.
- Having connector binaries call BY-SYSTEMS Vault directly (they
  call Odoo's refresh route, which proxies internally).
