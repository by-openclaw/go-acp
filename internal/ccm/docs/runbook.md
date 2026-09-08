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

Start under your service manager with a pidfile, then converge declaratively — the same generic verbs every dhs producer has:

```
dhs producer ccm serve --dm-tree dm-tree.json --bind :8080 --pidfile /run/dhs-ccm.pid --metrics-addr :9100
dhs producer ccm ensure --state present --pidfile /run/dhs-ccm.pid   # drift report; run-twice = 0 changes
dhs producer ccm ensure --state absent  --pidfile /run/dhs-ccm.pid   # graceful stop, idempotent
dhs producer ccm stop   --pidfile /run/dhs-ccm.pid
dhs producer ccm status --url http://HOST:9100/snapshot.json
```

`serve --pidfile` writes the PID on start and removes it on exit; `ensure`/`stop` key on that file. `ensure --state present` on a stopped instance reports the fact rather than starting a foreground service — starting is the service manager's job, which is what the Ansible play drives. The replay itself is deterministic: the same `dm-tree.json` serves the same bytes every time, so two exports of the same firmware diff to nothing.

## 8. Matrix (§17) — in scope (owner decision reversed 2026-09-09)

The real device exposes 16 `/v1/matrix/...` paths — audio, data, video, each with `info`, `current` (read-only), `main` and `backup` (`PUT`). The 2026-08-22 exclusion was made without knowing matrix was in the protocol; it is now **in scope** for dhs.

How the CCM matrix relates to the probel / Ember+ (Snell SW-P-08 family) matrix DM — the same *kind* of model, mappable onto the canonical matrix entity (ADR-0023), but not identical:

| Aspect | Probel SW-P-08 / Ember+ | CCM §17 |
|---|---|---|
| Addressing | numeric `(matrix, level, dst, src)` / OID + `(target, source)` | **UUIDs** for sources, destinations and per-channel slots |
| Planes | one matrix, numbered *levels* = signal planes (video, audio…) | one matrix **per essence** (`/matrix/audio`, `/matrix/data`, `/matrix/video`) |
| `main` / `backup` | no equivalent | ST 2022-7 **redundancy paths** for one route — named write levels, not signal planes |
| `current` | tally / read-back | the effective route, read-only |
| State | crosspoint tally per level | `MatrixState`: **dst-uuid → src-uuid** map (multi-level extended form) |
| Info | matrix size / labels | `MatrixInfo{description, version, sources[], destinations[]}`, entries as path refs or inline, `slot_type` for mixed essences |
| Write | set crosspoint | `PUT`/`PATCH` a level's map → **202**, confirm via `current` |

So: canonical `matrix` / `usage` / `replace` map (spec review §17 row: state map = routes, usage = the inverted map, level names as strings), with two things the canonical entity must grow: **UUID-keyed ids** (as NMOS already needs) and a **redundancy-leg** notion for `main`/`backup`, which today's `level_id` (a signal plane) does not express.

**Emulator today:** a `PUT` to `main`/`backup` is stored like any resource write, but **`current` is not recomputed** — routing semantics are the next matrix unit (see the TODO list), not yet implemented. Until then treat matrix on the emulator as read-back-what-you-wrote.
