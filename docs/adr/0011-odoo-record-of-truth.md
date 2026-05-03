# ADR-0011 — Odoo as customer + license + asset record-of-truth

Status: accepted

## Context

License management requires tracking customers, contracts, billing,
issued licenses, deployed assets (which dhs binaries run where,
which version, which features, which heartbeat). Building a separate
licensing system would duplicate Odoo (the existing ERP/CRM) and
fork the source of truth.

## Decision

Odoo is the **single source of truth** for customer + license + asset
records. dhs license issuance integrates into Odoo workflows. Vault
holds the signing key (per ADR-0003 / ADR-0010); Odoo holds the
business data.

### Odoo data model (custom module)

| Odoo model | Purpose | Key fields |
|---|---|---|
| `res.partner` (existing) | customer/company record | name, email, country, contact tree |
| `sale.subscription` (existing — Subscriptions module) | recurring license term | partner_id, plan, recurring_amount, start_date, next_invoice |
| `dhs.license` (new) | one signed JWT per issuance | partner_id, subscription_id, features (m2m to dhs.feature), demo (bool), issued_at, expires_at, revoked_at, fingerprint, jwt_blob (text), online_refresh (bool), notes |
| `dhs.feature` (new) | feature catalogue | key (e.g. `emberplus.provider`), display_name, description |
| `dhs.asset` (new) | runtime asset tracking | partner_id, license_id, hostname, fingerprint, dhs_version, last_heartbeat, connector_type, connector_version |
| `dhs.asset.event` (new) | heartbeat / activation log | asset_id, kind (start/heartbeat/stop), at, details (JSON) |

### Issuance flow (signer external to Odoo)

```
Sales rep creates Sale Order in Odoo
   ↓
Odoo confirms order → creates dhs.license record (jwt_blob blank)
   ↓ webhook to signer
Signer service (Go binary, hardened host)
   ├─ pulls dhs.license fields from Odoo via JSON-RPC
   ├─ signs JWT-EdDSA via Vault Transit (ADR-0003 + ADR-0010)
   └─ writes jwt_blob back to dhs.license via Odoo API
Odoo emails JWT to customer via existing mail templates
   ↓
Customer drops license.lic on dhs host, restarts connector
```

### Online refresh flow

```
dhs-emberplus at customer site
   ↓ HTTPS GET /dhs/license/refresh?id=<license_id>&fingerprint=<sha256>
Odoo HTTP route (custom module)
   ├─ looks up dhs.license by id
   ├─ checks revoked_at IS NULL and expires_at > now
   ├─ verifies fingerprint matches
   ├─ logs dhs.asset.event (heartbeat)
   └─ returns current jwt_blob (possibly newer if renewed since last fetch)
dhs-emberplus replaces local cached license
```

### Reports for free in Odoo

| Report | How |
|---|---|
| Customer X's licenses + features + expiry | filter `dhs.license` by partner_id |
| Licenses expiring in next 30 days | filter `dhs.license` by expires_at |
| Customer X's deployed assets (hostnames, versions) | filter `dhs.asset` by partner_id |
| Connectors offline >7 days | filter `dhs.asset` by last_heartbeat |
| Revenue per active license | join `dhs.license` ↔ `sale.subscription` |
| Feature adoption across all customers | group by `dhs.feature` |

## Consequences

- Customer / license / asset / billing data unified in one system.
- Sales workflows trigger license issuance natively (Sale Order
  confirmed → license minted).
- Asset tracking (heartbeats from deployed connectors) feeds
  customer support ("customer X has 5 nodes deployed, all on
  v0.7.0, last seen 2h ago").
- Customer self-service portal via Odoo's existing portal module.

## Forbidden

- Building a parallel licensing database outside Odoo.
- Storing license-signing keys in Odoo (those live in Vault per
  ADR-0010).
- Hand-issuing licenses outside the Odoo workflow (every issuance
  must trace back to a `sale.order` or explicit `dhs.license`
  record).
