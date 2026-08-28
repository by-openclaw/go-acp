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
| mcast (`discoveryMode=mDNS`) | ✅ **with the pfSense Avahi reflector** (without it: measured 80 s of `status=None` — multicast stops at the routed hop) | with reflection MGMT↔DMZ enabled plus a MGMT pass rule for `udp/5353 → 224.0.0.251`, the same flip registered the Neuron within ~40 s: `status=mDNS url=http://10.100.0.5:8080/…`. Both registries advertised at `pri=0`; **the Neuron's tie-break picked Cerebrum**. Bonus receipt: while unregistered in mDNS mode it advertised `_nmos-node._tcp` itself (v1.0–v1.3, port 3000) and withdrew it on re-registering — the §4.2.1 pair, both directions observed. Trade-off also observed live: the reflector leaked the MGMT NAS's FTP/SMB records onto the DMZ, exactly as [`dns-sd-unbound.md`](dns-sd-unbound.md) warns |
| sd-dns | ✅ infrastructure / ⏳ device client | pfSense Unbound now serves the full record set under `by-systems.arpa` (recipe refreshed + applied 2026-08-28): `discover -no-mdns -resolver 10.100.0.1` returns both registries, and the IS-09 System API resolves AND fetches `/global` through the same resolver. Every VLAN the pfSense resolver serves — the Neuron's included — can discover us. What remains is whatever DNS-SD client behaviour the Neuron itself implements (its registry Mode dropdown offers Manual/mDNS; observed no unicast option) |
| IS-09 on/off | ⚠️ **blocked by the device's mDNS-only IS-09 discovery across a routed hop** — measured in three layers. (1) REST: a PUT setting `system.uri` + `applyApiIs09=true` returns 200 and is silently discarded — the `system.*` block is read-only. (2) UI: with the firewall pass MGMT→`10.100.0.101:10641` in place and an NMOS off/on cycle, the API still reads `apply=false / discovered=NA` and zero packets reached our System API. (3) Transport: the Neuron's discovery choices are Manual/mDNS only, `system.uri` is not assignable, and no `_nmos-system` multicast exists on its segment — so even a working toggle has nothing to discover without an mDNS reflector on pfSense (which [`dns-sd-unbound.md`](dns-sd-unbound.md) advises against) or EVS adding unicast DNS-SD for IS-09. The unicast records ARE live and correct (§sd-dns); the gap is the device's client. Our System API side is fully proven (§7) |

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
