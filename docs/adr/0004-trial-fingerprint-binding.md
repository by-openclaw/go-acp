# ADR-0004 — Trial license fingerprint binding

Status: accepted

## Context

Trial licenses must not be transferable between machines. A customer
issued a 30-day demo for evaluation on one server cannot copy it to a
production cluster.

## Decision

Trial licenses carry `demo: true` and a `fingerprint` claim bound to
the first machine that activates the license.

### Fingerprint computation

```
fingerprint = sha256(
    canonical(MAC of primary network interface) || "\n" ||
    canonical(machine-id from /etc/machine-id or registry equivalent) || "\n" ||
    canonical(hostname FQDN)
)
```

### Activation flow (first run on a machine)

1. Customer drops the trial `license.lic` file.
2. Connector starts. License signature verifies. `demo: true` is
   detected.
3. Connector computes the local fingerprint.
4. If license has no `fingerprint` claim yet (issued without binding),
   POST to `refresh_url` with the local fingerprint to receive a
   re-signed JWT carrying that fingerprint.
5. If license already has a `fingerprint` claim, it must match the
   local fingerprint exactly. Mismatch → connector exits non-zero,
   logs `LICENSE: trial fingerprint mismatch — license bound to a
   different machine`.

### Behaviour

| State | Effect |
|---|---|
| `demo: true` + valid fingerprint match | startup banner at WARN: `LICENSE: demo, expires <date>, NOT for production use`; periodic reminder every 24 h |
| `demo: true` + fingerprint mismatch | refuse to start |
| `demo: true` + expiry passed | refuse to start |
| `demo: false` | fingerprint claim optional; if present and mismatched, refuse to start (production licenses can also be node-locked) |

### Online refresh exclusion

Trial licenses have `online_refresh: false`. They cannot renew.
Customer must purchase a non-demo license to extend.

## Consequences

- Trials cannot be cloned across machines.
- Customer evaluation friction is one extra HTTP call on first
  activation (to bind the fingerprint, if not pre-bound).
- Hardware changes (NIC swap, machine-id rotation, hostname change)
  invalidate trials by design.
- Production licenses may opt-in to fingerprint binding for
  node-locked deployments.

## Forbidden

- Issuing trials without an `expires_at` shorter than the standard
  trial term (default 30 days).
- Allowing `online_refresh: true` on `demo: true` licenses.
- Implementing a "transfer trial" feature.
