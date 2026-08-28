# Neuron interop matrix — deep tests with receipts

Live interop of the dhs NMOS stack against a real **EVS Neuron**
(CONVERT Hybrid, `bm-n-nnbrg-c01`, 10.6.255.102 on the MGMT VLAN) and
**EVS Cerebrum** (10.100.0.5 on the DMZ VLAN), across every discovery
mode and the IS-09 dimension. Every ✅ has a captured command +
observed output from the session of 2026-08-27/28; nothing here is
"should work".

Network reality that shapes the matrix: **mDNS does not cross the
routed pfSense hop** between MGMT and DMZ. Cells that need the Neuron
to *hear* multicast from the lab are physically empty on this
topology — the honest result is "not reachable by design", and the
unicast/manual columns are exactly what IS-04 §3.1 provides for it.

## 1. dhs Controller ↔ Neuron, DIRECT (no registry)

| Mode | Result | Receipt |
|---|---|---|
| manual (`--node`) | ✅ | `walk`/`connect`/`set` drove the Neuron all session: VTX-01→VRX-02 routed, VTX-01/VTX-02 assigned ST 2022-7 groups (`239.6.40.x/239.7.40.x`), dual-leg SDPs with `a=group:DUP` served |
| mcast (`discover -service _nmos-node._tcp`) | ✅ (absence = pass) | a REGISTERED Node must stop advertising `_nmos-node` (IS-04 §4.2.1); the Neuron is registered and correctly absent from the browse |
| sd-dns (zone `_nmos-node._tcp.nmos.lab`) | ✅ | the dnsmasq zone publishes the Neuron's Node API; `discover -no-mdns -resolver … -service _nmos-node._tcp.nmos.lab` resolves it |

## 2. Neuron → dhs Registry

| Mode | Result | Receipt |
|---|---|---|
| manual (`discoveryMode=Manual` + override) | ✅ | full parity registered: 208 senders / 208 receivers / sources / flows, 5 s heartbeats, GC-verified; survives registry restarts by re-registering |
| mcast (`discoveryMode=mDNS`) | ⚠️ mode works, link doesn't carry — **measured, not assumed** | flipped the Neuron to `discoveryMode=mDNS` (overrides untouched), NMOS off/on, watched 80 s: `status=None` throughout — registries announce on the DMZ and multicast stops at pfSense. Reverted to Manual; re-registered into the dhs registry within one cycle |
| sd-dns | ⏳ needs SRV records in a DNS the *Neuron* queries — pfSense Unbound per [`dns-sd-unbound.md`](dns-sd-unbound.md) | operator step on pfSense; the record set is the same one the lab zone carries |
| IS-09 on/off | ⏳ same VLAN constraint for mDNS discovery of the System API; the Neuron API also exposes `system.uri` for direct assignment — probe pending |

## 3. dhs Controller → dhs Registry → Neuron

| Mode (controller finds the registry) | Result | Receipt |
|---|---|---|
| manual (`--registry URL`) | ✅ | catalogue walks + IS-05 connects against the registry-held Neuron all session |
| mcast | ✅ | AMWA IS-04-04/IS-05-03 controller suites at 100% used discovery; live browse shows both registries with pri/TXT |
| sd-dns (unicast) | ✅ | controller suites ran against the tool's mock unicast DNS at 100%; live zone resolves `_nmos-register._tcp.nmos.lab` |

## 4. Neuron → Cerebrum Registry

| Mode | Result | Receipt |
|---|---|---|
| manual | ✅ | first joined Cerebrum with full 208-sender parity (after the pfSense MGMT rule); Cerebrum GC per-spec |
| mcast / sd-dns | same VLAN constraints as §2 — identical conclusions | |

## 5. Neuron + Cerebrum-hosted Registry + dhs Controller

✅ dhs controller reads Cerebrum's Query API (`--registry
http://10.100.0.5:8080`) — used throughout the session for node/device
verification. Note Cerebrum's registry offers **no WS subscriptions**
(POST /subscriptions → 404) and defaults to **page size 10** — the
controller's Link-header pagination is what makes its catalogue
complete.

## 6. Cerebrum as CONSUMER of the dhs Registry (the inverse)

✅ Cerebrum's **Network Media** device discovered `dhs-nmos-registry`
via Bonjour and consumed it as a full controller: 7 concurrent
connections, **six v1.3 Query-WS subscriptions** (one per resource
type). The Neuron reached Cerebrum's UI entirely through the dhs
registry. Details + setup traps in
[`cerebrum-interop.md`](cerebrum-interop.md).

## 7. IS-09 dimension (dhs side — live, not just suite-scored)

`dhs producer nmos serve --role system --config
tests/fixtures/nmos/system-global.json` stood up a System API
(heartbeat 4 s, PTP domain 127 — values chosen to be distinguishable
from defaults) announced on `_nmos-system._tcp`:

- **ON, mDNS**: the running mDNS-mode node observed the advert
  (`ttl=120` — the record-TTL fix at work) and logged
  `System API global applied` within seconds of the server appearing
  **mid-run** — IS-09 §4's re-resolve requirement, live.
- **OFF / static mode**: static-mode nodes log
  `System API discovery skipped (static mode)` — no discovery, no
  application, by configuration.
- **sd-dns**: the zone's `_nmos-system._tcp.nmos.lab` SRV resolves via
  `discover -no-mdns -resolver … -service _nmos-system._tcp.nmos.lab`.

And the CONTROLLER side (`dhs consumer nmos system`) read the global
through all three paths:

| Mode | Command shape | Result |
|---|---|---|
| direct | `system --direct 10.100.0.101:10641` | ✅ global read, heartbeat 4 / PTP 127 |
| mcast | `system --mdns` | ✅ selected `dhs-nmos-system` by pri, global read |
| sd-dns | `system --no-mdns --unicast --resolver … -service _nmos-system._tcp.nmos.lab` | ✅ — the search domain rides inside `-service`, same convention as `discover` |

## Field notes from running the cells

- The `_nmos-node` browse for cell 1 surfaced a **stray node from an
  earlier session** still announcing from another LXC. Second time
  this class bit (a stray *registry* once got scored in place of the
  one under test): before trusting any discovery-based result, browse
  the service type and account for every instance you see.
- The unicast zone resolves all three service types
  (`_nmos-register`, `_nmos-node`, `_nmos-system`) — one dnsmasq
  instance is a complete sd-dns test bench.

## Pending cells

- Neuron `system.uri` direct-assignment probe (IS-09 on/off on the
  device) — after the current Neuron-serialized run completes.
- Neuron sd-dns via pfSense Unbound — operator step, recipe exists.
- Formal three-mode × IS-09 sweep of the dhs nodes into the Cerebrum
  registry is implicitly ✅ (dhs-node-mdns / -dnssd / -manual all
  registered there; see [`runbook.md`](runbook.md) §5).
