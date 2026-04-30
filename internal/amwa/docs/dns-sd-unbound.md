# NMOS — Mode B unicast DNS-SD with pfSense Unbound

Production recipe for IS-04 §3.1.2 + RFC 6763 §10 unicast DNS-SD against
the lab pfSense at 10.100.0.1 / `by-systems.arpa.` zone. Live-verified
2026-04-30 against EVS Cerebrum's hosted Registry (10.100.0.5:8080) +
the workstation `dhs registry nmos serve` (10.6.239.113:8235); both
instances visible from one `dhs consumer nmos discover` call.

> **Why Mode B over Mode A across subnets.** Mode A (mDNS multicast) is
> link-local — TTL=1, won't traverse a router. Reflectors like Avahi can
> bridge the multicast across VLANs, but in a DMZ topology that leaks
> *every* `_*._tcp/udp` chatter (Spotify, AirPlay, Chromecast, printers,
> file shares) across the security boundary, has crash-on-malformed-mDNS
> CVE history (CVE-2017-6519, CVE-2021-3468), and grants no NMOS-specific
> benefit our peers don't already get from Mode B. Mode B keeps DMZ sealed
> at L3 and only publishes the records you intend.

---

## Per-instance pattern

Each NMOS Registry / System service contributes **one PTR + one SRV +
one TXT** record per face (Registration / Query). The four IS-04
§3.1.1.1 TXT keys are required:

| Key | Spec | Example |
|---|---|---|
| `api_proto` | IS-04 §3.1.1.1 | `http` (or `https` once IS-10 lands) |
| `api_ver` | IS-04 §3.1.1.1 | comma-list — `v1.3` or `v1.1,v1.2,v1.3` |
| `api_auth` | IS-04 §3.1.1.1 | `false` (until IS-10 deployed) |
| `pri` | IS-04 §3.1.1.1 | `0`–`99` production, `100`+ dev/staging |

Multiple instances of the same service type all PTR back to the bare
service name (RFC 6763 §4.3); the browser receives every PTR and
resolves each instance independently via SRV + TXT (chase-the-PTR,
RFC 6763 §10 — implemented in
[`internal/amwa/session/dnssd/unicast.go`](../session/dnssd/unicast.go)).

---

## pfSense — Services → DNS Resolver → General Settings → Custom Options

```
# === Upstream resolution =====================================================
forward-zone:
    name: "."
    forward-ssl-upstream: yes
    forward-addr: 1.1.1.1@853                # Cloudflare DoT IPv4
    forward-addr: 1.0.0.1@853                # Cloudflare DoT IPv4 (secondary)
    forward-addr: 2606:4700:4700::1111@853   # Cloudflare DoT IPv6
    forward-addr: 2606:4700:4700::1001@853   # Cloudflare DoT IPv6 (secondary)

# === pfBlockerNG block lists =================================================
server:include: /var/unbound/pfb_dnsbl.*conf

# === AMWA NMOS / IS-04 v1.3 Registry catalogue ===============================
# IS-04 §3.1 — Nodes/Controllers discover Registries via DNS-SD by browsing
# `_nmos-register._tcp` (registration face) and `_nmos-query._tcp` (query
# face). Each instance contributes one PTR + one SRV + one TXT (+ one A for
# the SRV target), with the four IS-04 §3.1.1.1 TXT keys (api_proto, api_ver,
# api_auth, pri). Multiple instances of the same service type all PTR back
# to the bare service name; the browser chases each one.
server:local-zone: "by-systems.arpa." transparent

# --- dhs Registry on by-desk-03 (10.6.239.113:8235) --------------------------
# `dhs registry nmos serve --bind :8235` running on the workstation.
# Phase 1 step #1 ships the discovery face only; Registration / Query HTTP
# REST surface lands in Phase 1 step #4.
server:local-data: "_nmos-register._tcp.by-systems.arpa. 60 IN PTR dhs._nmos-register._tcp.by-systems.arpa."
server:local-data: "dhs._nmos-register._tcp.by-systems.arpa. 60 IN SRV 0 0 8235 by-desk-03.by-systems.arpa."
server:local-data: 'dhs._nmos-register._tcp.by-systems.arpa. 60 IN TXT "api_proto=http" "api_ver=v1.3" "api_auth=false" "pri=0"'
server:local-data: "_nmos-query._tcp.by-systems.arpa.    60 IN PTR dhs._nmos-query._tcp.by-systems.arpa."
server:local-data: "dhs._nmos-query._tcp.by-systems.arpa. 60 IN SRV 0 0 8235 by-desk-03.by-systems.arpa."
server:local-data: 'dhs._nmos-query._tcp.by-systems.arpa. 60 IN TXT "api_proto=http" "api_ver=v1.3" "api_auth=false" "pri=0"'
server:local-data: "by-desk-03.by-systems.arpa. 60 IN A 10.6.239.113"

# --- dhs IS-09 System API on by-desk-03 (10.6.239.113:10641) -----------------
# `dhs producer nmos serve --role system --config global.json` running on the
# workstation. IS-09 v1.0 has its own DNS-SD service type (`_nmos-system._tcp`)
# and predates IS-10, so the TXT record advertises only api_proto / api_ver /
# pri — `api_auth` is intentionally absent per the v1.0 spec.
server:local-data: "_nmos-system._tcp.by-systems.arpa.    60 IN PTR dhs._nmos-system._tcp.by-systems.arpa."
server:local-data: "dhs._nmos-system._tcp.by-systems.arpa. 60 IN SRV 0 0 10641 by-desk-03.by-systems.arpa."
server:local-data: 'dhs._nmos-system._tcp.by-systems.arpa. 60 IN TXT "api_proto=http" "api_ver=v1.0" "pri=0"'

# --- EVS Cerebrum hosted Registry (10.100.0.5:8080) --------------------------
# Cerebrum's "Network Media Server" device with Hosted Registry mode enabled;
# Cerebrum runs both Registration + Query faces on the same HTTP listener.
# IS-04 catalogue versions confirmed live 2026-04-30: v1.1, v1.2, v1.3.
# Note: per `cerebrum-interop.md` §5, Cerebrum's Query face returns 404 on
# `nodes/`/`devices/`/etc. — Controllers must walk Nodes directly. Only
# `subscriptions/` (WS subscription POST endpoint) is usable.
server:local-data: "_nmos-register._tcp.by-systems.arpa. 60 IN PTR cerebrum._nmos-register._tcp.by-systems.arpa."
server:local-data: "cerebrum._nmos-register._tcp.by-systems.arpa. 60 IN SRV 0 0 8080 cerebrum-nmos.by-systems.arpa."
server:local-data: 'cerebrum._nmos-register._tcp.by-systems.arpa. 60 IN TXT "api_proto=http" "api_ver=v1.1,v1.2,v1.3" "api_auth=false" "pri=0"'
server:local-data: "_nmos-query._tcp.by-systems.arpa.    60 IN PTR cerebrum._nmos-query._tcp.by-systems.arpa."
server:local-data: "cerebrum._nmos-query._tcp.by-systems.arpa. 60 IN SRV 0 0 8080 cerebrum-nmos.by-systems.arpa."
server:local-data: 'cerebrum._nmos-query._tcp.by-systems.arpa. 60 IN TXT "api_proto=http" "api_ver=v1.1,v1.2,v1.3" "api_auth=false" "pri=0"'
server:local-data: "cerebrum-nmos.by-systems.arpa. 60 IN A 10.100.0.5"
```

Quoting rules:

| Outer quote | When |
|---|---|
| Double `"…"` | PTR / SRV / A — no inner double-quotes |
| Single `'…'` | TXT — inner `"key=value"` segments stay literal so Unbound emits each as its own length-prefixed RFC 1035 §3.3.14 string |

Avoid escaping inner `"` with `\"`; pfSense's WebUI textarea round-trips
strip backslashes inconsistently.

Apply: GUI **Save → Apply Changes** triggers an Unbound reload.

---

## Verification

### 1. DNS ground truth — `nslookup` against the resolver

```
nslookup -type=PTR _nmos-register._tcp.by-systems.arpa 10.100.0.1
nslookup -type=PTR _nmos-query._tcp.by-systems.arpa    10.100.0.1
nslookup -type=SRV cerebrum._nmos-register._tcp.by-systems.arpa 10.100.0.1
nslookup -type=TXT cerebrum._nmos-register._tcp.by-systems.arpa 10.100.0.1
nslookup -type=A   cerebrum-nmos.by-systems.arpa 10.100.0.1
```

Each `_nmos-register._tcp` + `_nmos-query._tcp` PTR query should return
two answers (`dhs.…`, `cerebrum.…`).

### 2. dhs end-to-end

```
dhs.exe consumer nmos discover --no-mdns --unicast --resolver 10.100.0.1 \
        --service _nmos-register._tcp.by-systems.arpa --timeout 5s

dhs.exe consumer nmos discover --no-mdns --unicast --resolver 10.100.0.1 \
        --service _nmos-query._tcp.by-systems.arpa    --timeout 5s
```

Expected output (one block per instance):

```
Discovered 2 instance(s) of _nmos-register._tcp.by-systems.arpa:
  cerebrum._nmos-register._tcp.by-systems.arpa
    host = cerebrum-nmos.by-systems.arpa:8080
    pri  = 0
    proto= http
    ver  = v1.1,v1.2,v1.3
    auth = false
    ipv4 = 10.100.0.5
  dhs._nmos-register._tcp.by-systems.arpa
    host = by-desk-03.by-systems.arpa:8235
    pri  = 0
    proto= http
    ver  = v1.3
    auth = false
    ipv4 = 10.6.239.113
```

Order is unspecified per RFC 6763 §4.3; both instances must appear.

### 3. HTTP face sanity (separate from discovery)

```
curl http://10.100.0.5:8080/x-nmos/registration/        # ["v1.1/","v1.2/","v1.3/"]
curl http://10.100.0.5:8080/x-nmos/registration/v1.3/   # ["resource/","health/"]
curl http://10.100.0.5:8080/x-nmos/query/v1.3/          # ["nodes/","devices/",…]
```

---

## Adding a new Registry / System / Node instance

Pattern per peer:

1. Pick a unique label inside the zone — e.g. `cerebrum-stg`, `node-cam-04`.
2. Add **two** PTR rows (one per face) under the bare service type:
   `_nmos-register._tcp.<zone>.` and `_nmos-query._tcp.<zone>.` (omit
   query if the peer is Node-only).
3. Add **one** SRV per face pointing at a target hostname inside the
   zone.
4. Add **one** TXT per face with the four IS-04 keys.
5. Add **one** A record for the target hostname.

For an IS-09 System server, use `_nmos-system._tcp` instead — and
**omit `api_auth`** from its TXT record (IS-09 v1.0 predates IS-10).
For Node P2P (Mode D), use `_nmos-node._tcp`.

Compliance events fired by `dhs consumer nmos discover` when a peer's
records deviate (priority collisions, missing keys, malformed `pri`)
are catalogued in
[`../codec/dnssd/types.go`](../codec/dnssd/types.go) and surface via
the `compliance.Event` channel — see
[`internal/protocol/compliance/`](../../protocol/compliance/).

---

## Cross-references

- [`cerebrum-interop.md`](cerebrum-interop.md) — Cerebrum-specific
  Registry quirks + Mode 1/2/3 mapping.
- [`firewall-recipes.md`](firewall-recipes.md) — host firewall rules
  (Windows / Linux / macOS) for the ports advertised by these records.
- [`matrix-compliance.md`](matrix-compliance.md) — per-vendor compliance
  tracker; Cerebrum row references this recipe.
- [`sequenced-tasks.md`](sequenced-tasks.md) §Phase 1 #1 — DNS-SD
  client + server + unicast fallback (PR #149).
- [`internal/amwa/CLAUDE.md`](../CLAUDE.md) Quirks #1 — four deployment
  modes + when to choose unicast over mDNS.
