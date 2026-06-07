# ADR-0003 — License JWT-EdDSA signed by Vault Transit

Status: accepted

## Context

dhs is commercial software. Each connector must verify entitlement at
startup (which features are enabled, expiry, demo flag, fingerprint).
The license must work offline (customer site without internet) and
support online refresh when renewed.

The signing key must never sit on disk where it could be exfiltrated.

## Decision

### Format

JWT (RFC 7519) signed with **EdDSA** (RFC 8037, Ed25519). RSA is
rejected (key sizes, performance, no operational benefit over Ed25519).

Wire shape: `eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9.<base64-payload>.<Ed25519-sig>`

### Claims

```json
{
  "iss": "by-systems.be",
  "sub": "<customer legal entity>",
  "iat": 1730000000,
  "exp": 1761536000,
  "license_id": "uid-<customer>-<year>-<seq>",
  "customer_id": "<short slug>",
  "product": "dhs",
  "features": {
    "<connector>.<feature>": true,
    "<connector>.<feature>": false
  },
  "demo": false,
  "online_refresh": true,
  "refresh_url": "https://license.by-systems.be/v1/refresh",
  "fingerprint": "sha256:..."
}
```

`features` is an object (not an array) so per-feature gating reads as
boolean lookups. `demo` and `fingerprint` are governed by ADR-0004.

### Signing key

The Ed25519 private key lives in **Vault Transit engine**. The
private key never leaves Vault. The signer service authenticates to
Vault via AppRole, calls `transit/sign/dhs-license`, receives the
signature, assembles the JWT.

### Verification

Connectors verify the JWT offline using a public key embedded at
build time. One global public key is embedded in every connector
binary (one signing keypair signs all customer licenses). Customer
isolation lives in claims (`customer_id`, `fingerprint`,
per-customer `features`), not in different keypairs.

### Online refresh

When `online_refresh: true`, the connector periodically POSTs
to `refresh_url` with `license_id` + current fingerprint. The server
returns a possibly-newer JWT (e.g. expiry extended after renewal).
The connector writes it back to its license cache and verifies it
offline with the embedded public key.

### Expiry behaviour

If `exp < now()` the connector **fails to start** with non-zero exit
and a clear error. There is no grace period, no degraded mode.

### Key rotation

Annual rotation by default; immediate rotation on incident. Per
ADR-0010 (Vault internal-only) the rotation is performed via Vault
Transit's `rotate` operation. Connector binaries embed the current public key
plus one previous version (grace period). The JWT header `kid`
identifies which key version signed it.

## Consequences

- One signing key, simple infrastructure.
- Customer licenses are portable text files (a single `.lic` JWT).
- No internet required for normal connector startup.
- Online refresh enables silent renewal after billing events.
- Compromise of the signer service impacts only the active signing
  window; rotate the Transit key + revoke the AppRole, no key
  exfiltration.

## Forbidden

- Storing the private signing key on disk anywhere outside Vault.
- Embedding a customer-specific public key in customer-specific
  binaries.
- Auto-downgrade or grace-period extension on expiry.
- Skipping signature verification under any flag.

## Revisions

- 2026-06-07 — errata: fixed the key-rotation citation (was
  "Per ADR-0009 / ADR-0014" — neither covers rotation) to ADR-0010
  (Vault internal-only). Per ADR-0015 Amendment policy; resolves
  coherence-review C4. — by-rune
