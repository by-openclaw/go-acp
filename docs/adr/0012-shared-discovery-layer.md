# ADR-0012 — Shared discovery layer (mDNS / unicast / peer-list)

Status: accepted

## Context

Many connectors need to discover peers. Today only the AMWA NMOS
plugin has a DNS-SD layer (Avahi DBus + stdlib floor). Re-implementing
discovery per protocol would duplicate code and force divergent UX
across connectors.

## Decision

Discovery is implemented once in `internal/transport/discover/`. Every
connector's `discover` verb (per ADR-0002) uses this shared package.
Per-protocol code provides only the **service name**.

### Three modes

| Mode | Use when | Mechanism |
|---|---|---|
| `mdns` | LAN, Avahi/Bonjour-friendly | multicast DNS-SD per RFC 6762/6763 |
| `unicast` (sd-dns) | enterprise / segmented network with DNS server | unicast DNS-SD via configured resolver + domain |
| `peer-list` | direct, no discovery infra | static `peers.csv` / `peers.yaml` of host:port |

### Per-protocol service names (canonical)

| Protocol | mDNS service name |
|---|---|
| ACP1 | `_dhs-acp1._udp` |
| ACP2 | `_dhs-acp2._tcp` |
| Ember+ | `_dhs-emberplus._tcp` |
| Probel SW-P-08 | `_dhs-probel-sw08p._tcp` |
| Probel SW-P-02 | `_dhs-probel-sw02p._tcp` |
| OSC | `_osc._udp` (or `_dhs-osc._udp`) |
| TSL UMD | `_dhs-tsl._udp` |
| HyperDeck | `_dhs-hyperdeck._tcp` |
| AMWA NMOS | `_nmos-register._tcp`, `_nmos-query._tcp`, `_nmos-node._tcp` (per spec) |

### Backend selection (multi-OS per ADR-0016)

| Host | Backend |
|---|---|
| Linux + avahi-daemon on system DBus | Avahi via `org.freedesktop.Avahi.Server` (godbus/dbus/v5) |
| macOS | Bonjour (path TBD per ADR-0016 CGo conflict) |
| Windows | Bonjour (path TBD per ADR-0016 CGo conflict) |
| anything else | stdlib `net.UDPConn` floor — universal fallback, never removed |

### Package shape

```
internal/transport/discover/
├── browser.go        # Browser interface
├── responder.go      # Responder interface (for protocols advertising on the wire)
├── browse_avahi.go   //go:build linux
├── browse_stdlib.go  // pure Go floor — all OS
├── browse_bonjour.go //go:build (darwin || windows) && cgo  (subject to ADR-0016 resolution)
└── doc.go
```

## Consequences

- Each protocol implements `discover` as: pick service name, pass to
  shared package, get peers back.
- New protocol = one constant (the service name), no new discovery
  code.
- Multi-OS backend choice is made once, applies everywhere.
- mDNS and unicast DNS-SD bugs fixed once benefit every connector.

## Forbidden

- Per-protocol re-implementation of mDNS / unicast DNS-SD.
- Hard-coded backends without OS-detection at startup.
- Removing the stdlib floor (it must always work as the universal
  fallback).
