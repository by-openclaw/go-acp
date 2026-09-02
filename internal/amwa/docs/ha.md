# NMOS — High availability + multi-Registry rules

What the IS-04 spec actually mandates around connecting to multiple
Registries, and what HA topologies dhs supports as a result.

## TL;DR

| Side | How many Registries / Query APIs at once? | Failover? |
|---|---|---|
| **Node** (device) | **One Registration API at a time.** | Yes — on 5xx or missed heartbeat, pick next from discovered list (lower `pri` first). Must re-register within GC interval (12 s default) or get purged. |
| **Controller** | **One Query API at a time.** | Yes — same pattern. |
| **Registry** | N/A — it doesn't connect to other Registries. | Active/passive via `pri` ranking (out-of-the-box). Active/active = shared-store, out of scope for v1. |

This means **HA is client-driven**, not Registry-side: Nodes and
Controllers do the failover work; Registries are independent.

---

## What the spec says (IS-04 v1.3.3, verbatim)

### Selection rules (Discovery — Registered Operation)

> "Values 0 to 99 correspond to an active NMOS Registration API (zero
> being the highest priority). Values 100+ are reserved for
> development work to avoid colliding with a live system."

> "the Node orders these based on their advertised priority (TXT
> `pri`), filtering out any APIs which do not support the desired API
> version, protocol and authorization mode"

> "The Node selects a Registration API to use based on the priority,
> and a random selection if multiple Registration APIs of the same api
> version with the same priority are identified."

> "The client selects a Query API to use based on the priority, and a
> random selection if multiple Query APIs of the same version with the
> same priority are identified."

### Failover rules (Discovery — Registered Operation + Behaviour — Registration)

> "If the chosen Registration API does not respond correctly at any
> time, another Registration API SHOULD be selected from the
> discovered list."

> "As this issue could affect just one Registration API in a cluster,
> it is advised that clients identify another Registration API to use
> from their discovered list."

> "The first interaction with a new Registration API in this case
> SHOULD be a heartbeat to confirm whether the Node is still present
> in the registry."

> "If a `5xx` error is encountered when interacting with all
> discoverable Registration APIs, clients SHOULD implement an
> exponential backoff algorithm."

### Timing

> "Nodes SHOULD perform a heartbeat every 5 seconds by default"

> "Registration APIs SHOULD use a garbage collection interval of 12
> seconds by default (triggered just after two failed heartbeats at
> the default 5 second interval)"

> "It is RECOMMENDED that heartbeat and garbage collection intervals
> are user-configurable to non-default values"

Both knobs exist in dhs: the registry's `--heartbeat-timeout` /
`--gc-interval` and the node's `--heartbeat` (#855; an IS-09 System
API's `heartbeat_interval` outranks the flag — the System API is the
plant-wide operator config).

**Spec gap — the threshold is not discoverable.** IS-04 defines the
defaults and no way for a Registry to advertise the values it actually
uses: DNS-SD TXT carries `api_proto` / `api_ver` / `api_auth` / `pri`,
nothing about timing; `POST /health/nodes/{id}` answers the last-seen
time, never the deadline; the Query API exposes no registry
configuration. A node configured off the registry's real timeout finds
out only when a heartbeat 404s. A controller watching the Query API
cannot tell eviction from clean deregistration either — a `removed`
grain is `pre`-only in both cases — so "node disappeared" findings
must always be phrased as two possibilities (clean DELETE vs GC).
Nothing to fix in our code; recorded so nobody re-derives it.

### Multi-network exception (closest to "HA" in the spec)

> "Registered discovery MAY use either a single registry, or an
> independent registry for each network"

> Node MAY register with Registration APIs (and host its Node API)
> "via multiple independent network interfaces"

This is the ST 2022-7 dual-network pattern: one Registry per
red/blue network, Nodes register via both interfaces.

### P2P is explicitly NOT a resilience strategy

> "it is RECOMMENDED that for most use cases, in particular fixed
> installations and those requiring high levels of resilience, the
> registered discovery mechanism is used."

So P2P fallback exists but cannot be sold as HA.

---

## HA topologies dhs will support

### Topology 1 — single Registry (default)

```
   ┌────────┐                    ┌────────────┐
   │ [N]    │   POST /resource   │ [R]        │
   │ Node   │ ──────────────────>│ Registry   │
   └────────┘                    │ pri=0      │
                                  └────────────┘
                                         ▲
                                         │  GET / WS
                                         │
                                  ┌────────────┐
                                  │ [C]        │
                                  │ Controller │
                                  └────────────┘
```

`dhs registry nmos serve` (default `--priority 0`).

### Topology 2 — active/passive priority pair (in-spec HA)

Two Registries with different `pri` values. Clients prefer lower `pri`,
failover to higher `pri` on 5xx. State is **not** shared between them —
when failover fires, every Node re-registers from scratch on the
secondary, then heartbeats hold registration alive there.

```
                       ┌────────────────┐
                       │ [R1] Registry  │
                       │ pri=0  PRIMARY │
                       └────────────────┘
                              ▲
                              │  preferred
                              │
   ┌────────┐                 │
   │ [N]    │  ───────────────┤
   │ Node   │                 │
   └────────┘                 │  on 5xx → failover
                              ▼
                       ┌────────────────┐
                       │ [R2] Registry  │
                       │ pri=1 SECONDARY│
                       └────────────────┘
```

CLI:
```
dhs registry nmos serve --priority 0 --advertise-host 10.0.0.1:8000   # on host A
dhs registry nmos serve --priority 1 --advertise-host 10.0.0.2:8000   # on host B
```

Recovery time = first-failed-heartbeat → next-Registry detection +
re-register handshake. With defaults: ≤ 5 s detection + ≤ 1 s
re-register = under the 12 s GC window.

### Topology 3 — ST 2022-7 dual-network (in-spec HA)

Separate Registry per redundant network. Each Node registers on both
networks via independent interfaces. Each Controller picks the
Query API on the network it's connected to.

```
   ┌────────────────┐                    ┌────────────────┐
   │ [R-RED] pri=0  │                    │ [R-BLU] pri=0  │
   │ on RED network │                    │ on BLU network │
   └────────────────┘                    └────────────────┘
           ▲                                      ▲
           │ POST /resource                       │ POST /resource
           │ via RED iface                        │ via BLU iface
           │                                      │
           └──────────────┬───────────────────────┘
                          │
                   ┌────────────┐
                   │  [N] Node   │
                   │  has RED+   │
                   │  BLU NICs   │
                   └────────────┘
```

CLI:
```
# RED Registry on red network
dhs registry nmos serve --bind 10.0.0.1:8000  --advertise-host 10.0.0.1:8000

# BLU Registry on blue network
dhs registry nmos serve --bind 10.0.1.1:8000  --advertise-host 10.0.1.1:8000

# Node side — `--bind` takes ONE address, so run one dhs producer per
# network (one process per NIC)
dhs producer nmos serve --bind 10.0.0.50:2080 --advertise-host 10.0.0.50:2080   # RED
dhs producer nmos serve --bind 10.0.1.50:2080 --advertise-host 10.0.1.50:2080   # BLU
```

The single-process HA mechanism dhs ships out of the box is the
active/passive `pri` pair (Topology 2); ST 2022-7 dual-network today
means one process per network as above.

### Topology 4 — active/active shared-store (OUT OF SPEC + OUT OF SCOPE FOR v1)

Two Registries with the same `pri` value sharing an in-memory store
(Redis / etcd / gossip). Clients use either, see consistent state.

**Why it's out of scope:**
- IS-04 doesn't define this; clients will pick *one* by random tiebreaker
  and stay there until 5xx anyway, so the only benefit is "either
  Registry survives without forcing client failover".
- dhs project policy (top-level `CLAUDE.md` "Storage"): *"No database.
  No Redis. Files only."*
- Belongs to the cross-protocol HA epic [#127](https://github.com/by-openclaw/go-acp/issues/127).
  Re-evaluate after that epic settles a shared-store strategy.

---

## Failover state machine (Node side)

```
                ┌──────────────────────────────────┐
                │  DISCOVER                        │
                │  ───────────                     │
                │  mDNS browse _nmos-register._tcp │◄──┐
                │  + unicast SRV/TXT               │   │
                │  → ranked list by pri, random    │   │
                │    tiebreaker                    │   │
                └────────────────┬─────────────────┘   │
                                 │ list non-empty      │ TTLs expired,
                                 ▼                     │ all targets failed
                ┌──────────────────────────────────┐   │
                │  REGISTERING                     │   │
                │  ────────────                    │   │
                │  POST /resource (Node)           │   │
                │  POST /resource (Devices ...)    │   │
                │  -> 200/201                      │   │
                └────────────────┬─────────────────┘   │
                                 │                     │
                                 ▼                     │
                ┌──────────────────────────────────┐   │
                │  REGISTERED                      │   │
                │  ────────────                    │   │
                │  POST /health/nodes/{id} every   │   │
                │  5 s (or configured interval)    │   │
                └────┬─────────────────────────┬───┘   │
                     │ 5xx OR network drop     │       │
                     │                         │       │
                     ▼                         │       │
                ┌──────────────────────────┐   │       │
                │  FAILOVER                │   │       │
                │  ─────────                │   │       │
                │  pick next entry from    │   │       │
                │  discovered list (next   │   │       │
                │  pri, then random)       │   │       │
                │  send heartbeat as first │   │       │
                │  call to confirm Node    │   │       │
                │  presence; if 200→ stay  │   │       │
                │  REGISTERED. If 404→     │   │       │
                │  re-POST /resource and   │   │       │
                │  go back to REGISTERING. │   │       │
                └────┬─────────────────────┘   │       │
                     │ no entries left         │       │
                     ▼                         │       │
                ┌──────────────────────────┐   │       │
                │  BACKOFF                  │   │       │
                │  ─────────                │   │       │
                │  exponential, max 30 s.  │   │       │
                │  re-discover when TTLs    │───┘       │
                │  expire OR backoff hits  │           │
                │  cap.                    │───────────┘
                └──────────────────────────┘
```

The same shape applies to Controller side, swapping
"Registration API" for "Query API" and dropping the heartbeat /
re-register branch.

---

## Failover observability

The Node-side failover machinery is implemented in
`internal/amwa/provider/registration_client.go`. It fires no dedicated
compliance events; the observable surface is the structured log:

```
provider/node: better registry available — switching   a higher-pri Registry appeared; client re-targets it
provider/node: heartbeat 404 — re-registering          new Registry does not know us; full re-register
provider/node: heartbeat failed                        current Registry disqualified; next cycle tries next-best
provider/node: re-register attempt failed              registration retry against the picked Registry failed
```

On failover the client follows IS-04 v1.3.3 §6.1: heartbeat-first
against the new Registry, full `POST /resource` only on a 404 (AMWA
test_16 fails a Node that re-POSTs on every failover).

---

## Cross-reference

- Cross-protocol HA epic: [#127](https://github.com/by-openclaw/go-acp/issues/127)
- Matrix-vendor compliance (per-vendor failover quirks):
  [`matrix-compliance.md`](matrix-compliance.md)
