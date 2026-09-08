# CCM provider runbook — replay a device for Cerebrum

Operate `dhs producer ccm serve`: capture a real EVS BRIDGE / Neuron once, replay it as a CCM device a controller drives. The emulate-before-hardware path for Tree/DM device control, configuration and monitoring.

Scope reminder (owner decision, `spec-review-0v1.md`): CCM in dhs is the acp2 successor for device control — **not a router**. Matrix routing stays with Cerebrum and the router protocols; the emulator does not serve §17.

## 1. Capture the device model

On a host that reaches the real device (HTTPS, port 443):

```
dhs consumer ccm export 10.6.255.102
```

This writes `<out>/<Product@Ver>/api.yml` (the device's own OpenAPI 3.1) and the walked device model. With PR #984 merged the export also writes `dm-tree.json` — the full recursive model (resource path → resource JSON) — which is what the provider replays. Keep the export in git per ADR-0022 (versioned DM store); a later firmware exports beside it and `diff -u` shows the drift.

## 2. Replay it

Plain HTTP for a lab capture:

```
dhs producer ccm serve --dm-tree BRIDGE@7.0.2/dm-tree.json --api-spec BRIDGE@7.0.2/api.yml --bind :8080
```

Indistinguishable from a device (HTTPS, as a real one serves on :443):

```
dhs producer ccm serve --dm-tree BRIDGE@7.0.2/dm-tree.json --api-spec BRIDGE@7.0.2/api.yml \
  --bind :8443 --tls-cert dev.crt --tls-key dev.key --metrics-addr :9100
```

| Flag | Purpose |
|---|---|
| `--dm-tree PATH` | the model to replay (required) |
| `--api-spec PATH` | the device's `api.yml`, served at `/api/v1/docs/api.yml` |
| `--bind ADDR` | listen address (default `:8080`) |
| `--tls-cert` / `--tls-key` | serve HTTPS (given together); TLS 1.2 floor via the shared transport posture |
| `--metrics-addr ADDR` | Prometheus `/metrics` + `/snapshot.json`, like every dhs producer |
| `--readme PATH` | render another Markdown doc at `/x-dhs/readme` |

The startup log states what was loaded: `resources=N nodes=M spec=true|false`.

## 3. Point Cerebrum at it

Configure the Cerebrum CCM driver with the emulator's host and port instead of the device's. Cerebrum fetches `/api/v1/docs/api.yml` and walks `/api/v1` exactly as it would a BRIDGE; nothing on that namespace is dhs-specific.

## 4. Verify

From any host that reaches the emulator:

```
curl -s http://HOST:8080/api/v1              # root: ["io","misc","processing","self", ...]
curl -s http://HOST:8080/api/v1/self         # identity
curl -s http://HOST:8080/api/v1/docs/api.yml # the OpenAPI 3.1
```

Write, then confirm by read-back (§11.3 — the 202 is an acknowledgement, not the result):

```
curl -s -i -X PATCH http://HOST:8080/api/v1/io/ip/senders/video/<uuid> \
  -H 'Content-Type: application/json' -d '{"enable":true}'   # HTTP/1.1 202, empty body
curl -s http://HOST:8080/api/v1/io/ip/senders/video/<uuid>   # shows enable:true
```

Open `http://HOST:8080/x-dhs/` in a browser for the landing (identity, the API table read from the OpenAPI, links), `/x-dhs/readme` for the tech doc, `/x-dhs/capabilities` for the JSON summary. Everything under `/x-dhs` is dhs-only and never appears under `/api/v1`.

## 5. Expected responses (the contract)

| Request | Response |
|---|---|
| `GET` node path | `200` sorted array of child names |
| `GET` resource path | `200` captured body, verbatim |
| `GET` unknown path | `404 {"code":404,"message":...}` |
| `PUT` / `PATCH` resource | `202` empty (§11.1) — read back to confirm |
| `PUT` / `PATCH` with `uuid`/`id` | those keys ignored (§14.1 immutable) |
| write to `.../status` or `docs/api.yml` | `405` (GET-only, §11.1) |
| write to a collection array | `405` (not an individual resource) |
| body not a non-empty JSON object | `400` (§11.2 maps not arrays; minProperties 1) |

## 6. Troubleshooting

- **`--dm-tree` parse error** — the file must be a JSON object whose keys are resource paths and values the resource bodies. Every ancestor becomes a node automatically; record leaves only.
- **Cerebrum sees no spec** — start with `--api-spec`; without it `/api/v1/docs/api.yml` is a `404` and the landing says so.
- **Cerebrum refuses plain HTTP** — a real device is HTTPS on 443; run with `--tls-cert/--tls-key --bind :8443` (or `:443` with the right privileges).
- **A write "did nothing"** — the `202` is only the acknowledgement; `GET` the resource. Writes to `/status`, the spec, or a collection are refused with `405` by design.
- **No change notifications** — the `/ws` change stream (§13) is not served by this emulation yet; it is JWT-gated on the real device and a later unit.
- **Metrics show zero** — pass `--metrics-addr`; every response records size and handler latency, so `dhs metrics show --url http://HOST:9100/snapshot.json` reports real traffic.

## 7. Idempotency (ADR-0007)

`dhs producer ccm ensure --state present|absent` converges the serving instance the same way as every other producer (keyed on `--pidfile`); run-twice yields no change. The replay is deterministic: the same `dm-tree.json` serves the same bytes every time, so two exports of the same firmware diff to nothing.
