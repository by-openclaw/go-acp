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
| `main` / `backup` | no equivalent | two independent **routing levels**: each destination has a main source and a backup source (the Neuron front end has main and backup inputs and outputs; the backup level routes a source to the output's backup side). Named, writable, strings. **Not** ST 2022-7 — that is the IP stream `legs` concept, unrelated to the matrix. |
| `current` | tally / read-back | the effective route per destination, read-only; how it is derived from main/backup (failover rule) is **not in the spec** — open question for EVS |
| State | crosspoint tally per level | `MatrixState`: a flat **dst → src** map per level endpoint. **The ids are not uuids**: they are each group's `template` rendered with `{idx}` (child index, 3 digits) and `{subIdsIdx}` (channel, 2 digits), e.g. `IP000-05`, `EM003-12`, `DB255-01` |
| Info | matrix size / labels | `MatrixInfo{description, version, sources[], destinations[]}` where each entry is a **group** `{template, type, path, children[]{id, subIds}}`. **This is the link**: a state id resolves through its group → `children[idx].id` (the object uuid — or an integer id for internal resources) → the object at `path/{id}`, channel `subIdsIdx` |
| Write | set crosspoint | `PUT`/`PATCH` a level's map → **202**, confirm via `current` |

**Worked samples, confirmed live on the BRIDGE 7.0.2 (2026-09-09)** — the link that could not be made before, made explicit. Two real routes from `/matrix/audio/current`:

```
route  EM000-00  <-  IP000-00        (embedder channel fed by an IP receiver channel)
  destination EM000-00 -> group type=Embedder    template=EM{idx}-{subIdsIdx}  idx=0 channel=00
                          children[0] = {id: 619811ac-96bc-473f-a90b-cc7c31470a00, subIds: 16}
                          GET /api/v1/processing/video/channels/619811ac-…   -> uuid 619811ac-…  (video channel; no name field)
  source      IP000-00 -> group type=IP          template=IP{idx}-{subIdsIdx}  idx=0 channel=00
                          children[0] = {id: cd17dc08-f637-4b0b-a221-4e6a9d0c9530, subIds: 16}
                          GET /api/v1/io/ip/receivers/audio/cd17dc08-…        -> uuid cd17dc08-…  name "Input Audio Stream 1"

route  IP000-05  <-  DM000-05        (IP sender channel 5 fed by de-embedder channel 5)
  destination IP000-05 -> group type=IP          template=IP{idx}-{subIdsIdx}  idx=0 channel=05
                          children[0] = {id: cc0e77f7-966a-41a1-a054-8c02d480b829, subIds: 16}
                          GET /api/v1/io/ip/senders/audio/cc0e77f7-…          -> uuid cc0e77f7-…  name "Output Audio Stream 1"  channels=16
  source      DM000-05 -> group type=De-embedder template=DM{idx}-{subIdsIdx}  idx=0 channel=05
                          children[0] = {id: 619811ac-96bc-473f-a90b-cc7c31470a00, subIds: 16}
                          GET /api/v1/processing/video/channels/619811ac-…   -> the SAME video channel object as the embedder above
```

Reading it: the state never carries a uuid. `info` tells you, per group, the `template`, the `path` and the ordered `children`; render the template with the child index and channel to get the state id, or parse a state id back (`\d+` for each placeholder) to reach `children[idx].id` and `GET path/{id}`. One video-channel object appears in two groups — as a **De-embedder source** and an **Embedder destination** — and processing objects carry no `name` (only io streams do). The identity at `/self` is nested: `app.productName` / `app.productVersion` / `app.modelVersion`.

Not every matrix is multi-level: `data/output` and `video/output` answer `404` for `main`/`backup` (single level — `current` only); `audio`, `data/path`, `video/path` have all three. The `{idx}` zero-pad width is per matrix (`IP000`, `MA003` on audio; `IP00`, `PATH00` on data/path) — parse with `\d+`; the rendering width rule is unconfirmed and is a question for EVS. These captures live under `internal/ccm/codec/testdata/live/BRIDGE@7.0.2/` as the real oracle. The audio matrix has four source groups (IP receivers ×144, De-embedder ×32, Delay Bank ×256, MADI inputs ×4) and four destination groups (IP senders, Embedder, Delay Bank, MADI outputs); routes cross groups (`EM000-00 ← IP000-00`). The same rendered key can name a receiver (as a value) and a sender (as a key) — the side disambiguates. Firmware deviations from the paper/schema, recorded as compliance events not worked around: `info` carries **no `levels`** array (levels exist only as the `current`/`main`/`backup` endpoints), and Delay-Bank `children[].id` is the **integer `0`** where the schema says string.

So: canonical `matrix` / `usage` / `replace` map (spec review §17 row: state map = routes, usage = the inverted map). The essence matrices (`audio`, `video`, `data/output`, `data/path`, `video/output`, `video/path`) are separate canonical **matrices** (`matrix_id` as a string identifier), `main`/`backup` are canonical **levels** (string names), and `current` is the read-only **tally** level. What the canonical entity must grow: a destination/source id that is a **resolved tuple** — `(group type, object uuid or int id, channel)` — with the object's `name` fetched from `path/{id}`; the template-rendered key is the wire form, the tuple is the model. The emulator validates every written key/value by rendering the group templates over `info.children` (a key that renders to no child is a `400`), and the consumer view shows names, not `IP000-05`.

**Emulator today:** a `PUT` to `main`/`backup` is stored like any resource write, but **`current` is not recomputed** — routing semantics are the next matrix unit (see the TODO list), not yet implemented. Until then treat matrix on the emulator as read-back-what-you-wrote.
