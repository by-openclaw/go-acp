# CLAUDE.md — EVS Neuron REST connector

Atomic context for the `neuron` connector (`internal/neuron/`). Read
with the top-level [`CLAUDE.md`](../../CLAUDE.md).

## What it is

The EVS Neuron's **REST control surface** — distinct from acp2
(AN2/binary, numeric object ids). This is HTTPS/JSON at
`https://<neuron>/api/v1/`, and every stream resource carries a stable
**UUID**, so the connector addresses by UUID, not oid — and that UUID
is the join key to the same box's NMOS registry resource and (via each
leg's stream id) its acp2 object. Issue #975.

## Authoritative DM

The device serves its own **OpenAPI 3.1 spec** at
`/api/v1/docs/api.yml` (Swagger/Redoc UI at `/api/v1/docs/`). It is
committed verbatim at
[`codec/testdata/neuron-api-1.0.0.yml`](codec/testdata/neuron-api-1.0.0.yml)
as the authoritative DM oracle — 74 paths, **73 GET + 35 PUT**
(read AND control: route receivers, set matrix crosspoints), 214
component schemas. `spec_test.go` pins our hand-written model to it so
a firmware reshape is caught in unit test, not the field.

There is also a full walked capture
([`neuron-bridge-6.7.4.dm.json`](codec/testdata/neuron-bridge-6.7.4.dm.json),
BRIDGE 6.7.4) as a runtime decoder oracle — same committed-real-device
precedent as the acp2 CONVERT Hybrid DM.

## Tree

```
/api/v1/
  self                              productName / productVersion / modelVersion
  misc/{pattern-gen,ddr,ftp,rtp-payload-types,nmos,macs,user-label,reference}
  io/{ip,madi,sdi}
    ip/{senders,receivers}/{video,audio,data}   ← UUID-keyed streams, 2022-7 legs, SDP
  processing/{audio,data,video}
  matrix/{audio,data,video}                      ← crosspoints (PUT to route)
```

`misc/nmos` reports the Neuron's NMOS registry pointing — the cross-
surface correlation (#826): same box on REST + NMOS + acp2.

## Quirks

- **No documented WebSocket** in this firmware's spec (probed: `/ws`,
  `/api/v1/events`, `/subscribe` all 404). If a push channel exists it
  is undocumented / on another port — do not assume one.
- Self-signed TLS, unauthenticated on the lab device. Client skips
  verification by default (`--verify-tls` opts in) — a closed
  media-plane VLAN.
- A leg's `mac` field is a **stream UUID, not a MAC address**.
- Stream arrays are positional in the REST path, but each element
  carries its own `uuid` — always key by that, never by index.

## Status / next units

Unit 1 (PR #980): stdlib UUID-keyed codec + `dhs consumer neuron walk`
— live-proven 416 streams off 10.6.255.102. NEXT (await user
direction): get/export/audit by UUID; the NMOS-UUID cross-oracle
(#826); control verbs (route/matrix) from the 35 PUT ops; Ansible
verify play. Not the slot-based `consumer.Protocol` interface — REST
doesn't fit it; standalone verb like `cerebrum-nb`.
