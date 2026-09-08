# dhs CCM device (provider)

This process replays a captured **CCM device model** — the REST device model of an EVS BRIDGE / Neuron — so a controller such as Cerebrum drives dhs as if it were the real device. It is the emulate-before-hardware path: capture a device once, replay it anywhere.

CCM is the acp2 successor for **Tree / DM device control, configuration and monitoring**, matrix included. The contract is the device's own OpenAPI document (`api.yml`, OpenAPI 3.1): the emulator serves the captured tree and accepts exactly the writes that document declares, nothing more. Two sources exist for CCM — the EVS "Protocol Description 0v1" PDF, a design proposal, and the `api.yml` the device serves. Where they differ, the `api.yml` wins here, because it is what a real controller is written against.

## Two namespaces

| Prefix | What lives there |
|---|---|
| `/api/v1` | The CCM protocol, **100% to the device's API document**. The self-describing tree, the document itself, the writes it declares, the `{code,message}` error envelope. A CCM controller sees exactly a CCM device. |
| `/x-dhs` | dhs additions only — this page, the landing, a capabilities document. Mounted through one entry point that prepends the prefix, so nothing here can ever change what a controller observes under `/api/v1`. |

## What it serves

- **The tree**: `GET` a node path returns the sorted array of its child names; `GET` a resource path returns the captured body verbatim; anything else is a `404`. This is the self-describing walk contract a controller recurses on.
- **The OpenAPI document** (3.1) at `/api/v1/docs/api.yml` — the path a real device uses — when started with `--api-spec`.
- **Writes**, only where the `api.yml` declares them. The shipped document declares `PUT` on 35 resources and on the matrix `main`/`backup` levels, no `PATCH`; each answers the status the document promises (**`200` with the resource as it now reads**). `uuid` and `id` are never overwritten. A verb the document does not declare on a path is `405` (so `PATCH`, `/status` views, `info`, `current`, the spec, collections); an unknown path is `404`; a body that is not a JSON object, or a `MatrixState` with a non-string value, is `400`. Without `--api-spec` there is no contract, so the replay is `GET`-only.
- **Matrix**: served exactly as captured and declared — `info` and `current` are `GET`; `main`/`backup` are `GET`+`PUT` with a `MatrixState` (a flat destination → source map of string ids) on the matrices that have them (`audio`, `data/path`, `video/path`); `data/output` and `video/output` have no `main`/`backup` and answer `404` there. A `PUT` stores the map and returns it. The document says nothing about how `current` follows `main`/`backup`, so the emulator does not invent it: `current` stays as captured.
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
- Anything the `api.yml` does not declare: `PATCH`, request-body field validation beyond "a JSON object" (the per-resource `*Put` schemas are not enforced yet), and any `current`-from-`main`/`backup` rule — the document does not define one.
