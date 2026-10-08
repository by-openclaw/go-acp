# CCM provider

`dhs producer ccm serve` replays a captured device model as a CCM
device, so a controller drives dhs as it would a Neuron.

This page says where the provider stands. What it serves is in
[`../provider/README.md`](../provider/README.md) (also served rendered at
`/x-dhs/readme`); how to capture, replay, verify and troubleshoot is in
[`runbook.md`](runbook.md).

## In two commands

```
dhs consumer ccm export 10.6.255.102                # capture the device once
dhs producer ccm serve --dm-tree <export>/dm-tree.json --api-spec <export>/api.yml \
    --bind :8443 --tls-cert dev.crt --tls-key dev.key
```

## What it is, today

| | State |
|---|---|
| the tree, the OpenAPI document, the `{code,message}` errors | served, as captured |
| writes | only those the device's document declares (`PUT`; no `PATCH`) |
| matrix `main` / `backup` | read and written; `current` stays as captured |
| the device shape | the one under `/api/v1` (BRIDGE / CONVERT) |
| the event channel (`/ws`, spec §13) | **not served** — #1214 |
| the SHUFFLE shape (`/api`) | **not served** — #1214 |
| metrics | `--metrics-addr`, like every producer |

## Proof

- Unit tests in `../provider/`.
- **No controller we did not write has driven it yet.** Our own consumer
  reading it back is a smoke test, not an oracle (ADR-0034). The proof
  is Cerebrum's CCM driver pointed at it, and waits for Cerebrum.
