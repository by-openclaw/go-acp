# Cerebrum NB provider — N/A by design (consumer-only)

> **Status: not applicable — by design, not deferred.**
>
> The Cerebrum NB connector is **consumer-only**. There is no provider
> plugin, and there will not be one. This is a deliberate scope decision
> grounded in what the protocol *is*, not a "ship now, finish later" gap.

---

## Why there is no provider role

The EVS **Cerebrum Northbound API 0v16** (a.k.a. Neuron Bridge) is a
**northbound API exposed by the Cerebrum broadcast-control system** so that
*external* systems can drive and observe it. The roles are fixed by the
protocol:

| Role | Who plays it | What dhs does |
|---|---|---|
| **Northbound API server** | the EVS Cerebrum application itself | nothing — this is EVS's product |
| **Northbound API client** | an external controller / monitor | **our consumer** (`dhs consumer cerebrum-nb`) |

For most dhs connectors the provider role means "serve the protocol so a
real controller can drive *us*" (ACP1 serves an AxonNet device tree; Probel
SW-P-02 serves a matrix). That makes sense for **device / matrix**
protocols where dhs can stand in for the hardware.

Cerebrum is different: it is not a device, it is a **control system**. To
"serve Cerebrum" would mean re-implementing the Cerebrum routing engine,
device tree, salvo/category model, data stores, and licensing — i.e.
building a competitor to EVS's product, not a wire endpoint. That is out of
scope for dhs and contributes nothing to the consumer use-cases (drive +
monitor a real Cerebrum). Hence the provider deliverable is **N/A**.

Contrast with the loopback-capable connectors: ACP1 and Probel-SW02P can
run our consumer against our own provider for loopback regression
(ADR-0025 Tier 4). Cerebrum has **no provider and therefore no loopback
tier** — see [verbs.md](verbs.md) §11 and
[`../../../ansible/playbooks/cerebrum-nb-integration.yml`](../../../ansible/playbooks/cerebrum-nb-integration.yml),
which mirrors acp1's **external-oracle** pattern (gated on a live host),
not acp2's loopback pattern.

## How the ADR-0025 "provider" deliverable is satisfied

[ADR-0025](../../../docs/adr/0025-per-connector-definition-of-done.md)
lists a provider deliverable for the general connector. For a consumer-only
protocol that deliverable is satisfied by **explicitly documenting that no
provider exists and why** (this file), exactly as the definition of done
intends: the connector is complete when its role on the wire is fully and
honestly covered. There is no hidden TODO here.

## If you came looking for a server to test against

There is no in-tree emulator either. Validate the consumer against a
**real, licensed Cerebrum** (the external oracle). Until the NB northbound
licence is enabled on the lab Cerebrum, integration is exercised only by:

- the **codec-generated fixtures** ([../testdata/fixtures/](../testdata/fixtures/README.md)) —
  real 0v16 wire frames, drift-guarded; and
- the **unit tests** in [../codec/](../codec/) and [../consumer/](../consumer/).

See [runbook.md](runbook.md) for the operator workflow and
[consumer.md](consumer.md) for the CLI.

## See also

- [README.md](README.md) · [consumer.md](consumer.md) · [verbs.md](verbs.md) · [runbook.md](runbook.md) · [keys.md](keys.md)
- [../CLAUDE.md](../CLAUDE.md) — wire layer, mtid, quirks
- [../../../docs/adr/0025-per-connector-definition-of-done.md](../../../docs/adr/0025-per-connector-definition-of-done.md)
