# CCM consumer

`dhs consumer ccm <verb> <host> [flags]` — read, watch and write an EVS
Neuron over its REST API (CCM), the successor of acp2 on those devices.
The host is the positional argument; flags go on either side of it.

Everything on this page was run against real devices with the released
binary. The wire details and the deviations each device shows are in
[`../CLAUDE.md`](../CLAUDE.md); provisioning a Neuron end to end is in
[`provisioning.md`](provisioning.md).

## The two devices it is measured against

| | Neuron SHUFFLE 6.0.0 | Neuron CONVERT Hybrid 7.0.3 |
|---|---|---|
| address | `10.6.255.103` | `10.6.255.102` |
| API base | `/api` | `/api/v1` |
| how state arrives | pushed, over the event channel (`/api/ws`, spec §13) | polled — the device serves no event channel |
| size | 3 088 streams, 42 972 resources, 17 728 crosspoints x 3 maps | about 24 000 objects, 11 071 crosspoints |

Which of the two a device is, is asked of the device at connect time —
never assumed from its name or firmware. One connector serves both.

## Verbs

### info, health

```
$ dhs consumer ccm info 10.6.255.103
device       10.6.255.103:443
protocol     ccm v1
dtd_version  6.0.0-7e4a27e
slots        1

$ dhs consumer ccm health 10.6.255.103
host=10.6.255.103 proto=ccm
  reachable=true
  connected=true
  live=true  (last rx 0s ago, threshold 30s)
```

### walk

The streams, by UUID — eight seconds on the SHUFFLE:

```
$ dhs consumer ccm walk 10.6.255.103
Neuron SHUFFLE 6.0.0-7e4a27e (model 0) — 3088 stream(s)
UUID                                   KIND      ESSENCE ON     NAME
002220ab-3000-51b4-baba-a61f4bc56476   receiver  audio  yes    Input Audio Stream 1130
004897d3-f8eb-566a-8d75-b7b6fd0b6bf3   sender    audio  yes    Output Audio Stream 520
```

`--tree` walks the whole recursive device model instead — every node
and every resource (`--start p1,p2` seeds it from named nodes when the
API root does not list them). `--json` emits the device as one document.
The neutral `tree` verb prints the same model as objects: 357 638 lines
in about four minutes on the SHUFFLE.

### export

Stores what the device says about itself, keyed by product and
version, so a later firmware can be diffed against it:

```
$ dhs consumer ccm export 10.6.255.103
ccm export SHUFFLE@6.0.0-7e4a27e (10.6.255.103) -> ccm-export/SHUFFLE@6.0.0-7e4a27e
  3088 stream(s), 42972 DM resource(s) across 3364 node(s), api.yml stored (from /docs/openapi.yml)
  diff a later firmware with: diff -u ccm-export/SHUFFLE@6.0.0-7e4a27e/api.yml <newer>/api.yml
```

About 3 min 20 s on the SHUFFLE. The device model (DM) is always built
from the OpenAPI document the device serves, never written by hand.

With `--out-dir` and `--prefix` it also writes the routing as the
canonical matrix file-set (`-matrix.csv`, `-xpoint.csv`, `-dst.csv`,
`-src.csv`), one directory per matrix; `import --xpoint … --matrix …
[--check]` puts a matrix back, writing only the crosspoints that differ.

### get, set

One value by path, without a walk:

```
$ dhs consumer ccm get 10.6.255.103 --path reference.ptp.domain
value = 77
```

`set --path … --value …` writes one. A value the device's own document
forbids (out of range, wrong type) is refused before anything is sent.
This API has no PATCH: a write is a read of the resource, the change,
and a PUT of the whole resource, and several changes to one resource
are sent as one PUT.

### watch

```
$ dhs consumer ccm watch 10.6.255.103 --path processing.audio.delay.<bank>
```

On a device with the event channel nothing is polled: the device pushes
the resource once and then each change. On one without, the same verb
polls. A device is one or the other for the whole session.

One wildcard per subscription, never two: asked for every channel of
every sender in one subscription, the SHUFFLE stops answering its REST
API for about a minute. Members two levels deep are subscribed to
parent by parent instead.

### alarm

`alarm list | get | set | test | export | import | suggest` — the
neutral alarm rules, judged against the values `watch` sees.

## Flags

| Flag | Meaning |
|---|---|
| `--timeout D` | per-request timeout (default 8 s) |
| `--verify-tls` | verify the device certificate (default: skip — the lab devices are self-signed) |
| `--api-base` | the path the API hangs off; empty asks the device |
| `--api-spec` | the OpenAPI document relative to the base; empty tries both known names |
| `--json` | emit the whole device as JSON (`walk`) |

## What is tested, and where

`ansible/playbooks/ccm-integration.yml` runs the Go suite
`internal/ccm/integration` with the released CLI against both devices.
On the released v0.43.0 (2026-10-09), twice, the second run changing nothing: nine
tests pass —

- `info` names the device as it names itself; `get` reads one value
  without a walk; a value the document forbids never reaches the device;
- a delay, a gain and a crosspoint are each changed, seen by a `watch`
  without polling, and put back — the suite fails if a restore does;
- the device model is collected once and the next `watch` starts from it;
- `export` reads a device that has no API root;
- a device without the event channel is polled.

## Not covered

- Replay fixtures (ADR-0025 deliverable 6): none yet.
- Wireshark dissector: not shipped yet.

The six-deliverable state is in [`status.md`](status.md).
