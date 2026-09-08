# dhs CCM device (provider)

This process replays a captured **CCM device model** — the REST device model of an EVS BRIDGE / Neuron — so a controller such as Cerebrum drives dhs as if it were the real device. It is the emulate-before-hardware path: capture a device once, replay it anywhere.

CCM is the acp2 successor for **Tree / DM device control, configuration and monitoring**. It is not a router protocol for dhs: routing stays with the controller (the Route Master) and the router protocols, so the matrix section of the spec is out of scope here.

## Two namespaces

| Prefix | What lives there |
|---|---|
| `/api/v1` | The CCM protocol, **100% to the spec**. The self-describing tree, the OpenAPI document, PUT/PATCH, the `{code,message}` error envelope. A CCM controller sees exactly a CCM device. |
| `/x-dhs` | dhs additions only — this page, the landing, a capabilities document. Mounted through one entry point that prepends the prefix, so nothing here can ever change what a controller observes under `/api/v1`. |

## What it serves

- **The tree**: `GET` a node path returns the sorted array of its child names; `GET` a resource path returns the captured body verbatim; anything else is a `404`. This is the self-describing walk contract a controller recurses on.
- **The OpenAPI document** (3.1) at `/api/v1/docs/api.yml` — the path a real device uses — when started with `--api-spec`.
- **Writes** (`PUT` all mutable fields, `PATCH` one or more) answer an **empty `202`**: accepted as well-formed and applied. Confirm by reading the resource or its `/status` back. `uuid` and `id` are immutable and never overwritten. `/status` views and the spec are read-only (`405` on write); collections are not individual resources (`405`).
- **Errors** carry the CCM `{code, message}` envelope.

## Running it

```
dhs producer ccm serve --dm-tree BRIDGE@7.0.2/dm-tree.json --api-spec BRIDGE@7.0.2/api.yml
dhs producer ccm serve --dm-tree dm-tree.json --bind :8443 --tls-cert dev.crt --tls-key dev.key
```

- `--dm-tree` — the model to replay, as written by `dhs consumer ccm export` (required).
- `--api-spec` — the device's own `api.yml`, served at the well-known path.
- `--bind` — listen address; a real device serves HTTPS on `:443`.
- `--tls-cert` / `--tls-key` — serve HTTPS with the shared TLS 1.2 floor.
- `--metrics-addr` — Prometheus `/metrics` and `/snapshot.json`, like every dhs producer.
- `--readme` — serve another Markdown document at `/x-dhs/readme` instead of this one.

## Not served by this emulation

- The `/ws` change-stream WebSocket (spec §13) — a later unit; it is JWT-gated on the real device.
- Matrix routing (spec §17) — out of scope by decision; read-only consumption at most.
