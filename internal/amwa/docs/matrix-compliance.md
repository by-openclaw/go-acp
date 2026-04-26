# NMOS — matrix-vendor compliance tracker

Per the dhs spec-strict / no-workaround posture (top-level CLAUDE.md
"Spec-strict, no-workaround posture"), we implement every NMOS
specification exactly as written. When a real-world peer deviates from
the spec, we **absorb the deviation, keep running, fire a compliance
event naming what we saw**, and document the vendor here.

This file is the audit trail. Add a row whenever a new vendor is
verified live; never silently work around a deviation.

> Mirror of `internal/probel-sw08p/CLAUDE.md` "Known deviations from
> spec" + the salvo-emit pattern documented in
> `feedback_probel_salvo_connected.md`.

---

## Vendor matrix

### Lawo VSM — "NMOS Client (generic)" driver

Source: <https://docs.lawo.com/vsm-ip-broadcast-control-system/vsm-interface-driver-and-application-details/driver-supported-protocol-driver/driver-nmos-client-generic>
Verified: 2026-04-26 (documentation review — no live VSM-NMOS testbed yet).

| Topic | Lawo VSM posture | dhs response |
|---|---|---|
| **IS-04 Node API** | ✅ Supported v1.0 – v1.3 (auto-selects highest). | Default behaviour — no event. |
| **IS-04 Registration API** | ❌ Not supported. Quote: *"Currently there is no support for IS-04 Registration and Query API's"*. | Direct-address mode (no Registry). When Controller targets a Lawo VSM directly, fire `nmos_registry_not_supported` once per session. |
| **IS-04 Query API** | ❌ Not supported (same quote). | Same as above — fall back to per-Node walking. |
| **IS-05 Connection Management** | ✅ Supported v1.0 – v1.1. `single/senders`, `single/receivers` only. | Full support; bulk path absent on peer is OK. |
| **IS-05 activation modes** | ⚠️ Only `activate_immediate`. Quote: *"activation time is always set to now for stream patching"*. | Detect rejection of `activate_scheduled_*`; fire `nmos_scheduled_activation_unsupported` and retry as `activate_immediate`. |
| **IS-05 SDP paths** | ⚠️ Only `active/` and `staged/`. | Don't probe `/constraints` against Lawo — the GET will 404. Fire `nmos_constraints_endpoint_missing` once. |
| **IS-07 Event & Tally** | ❌ Not supported (no WebSocket, no MQTT). | Fire `nmos_is07_unsupported` if Controller asks for tally on a Lawo Node. |
| **IS-08, IS-09, IS-12, MS-05** | ❌ Not mentioned in driver doc. | Fire `nmos_is<NN>_unsupported` on first failed probe. Treat as "absent" until proven otherwise live. |
| **BCP-002 / BCP-004 / BCP-006 / BCP-008** | ❌ Not mentioned. | If we send BCP-004 caps and they're ignored, no harm — they layer into IS-04 JSON. No event. |
| **WebSocket transport** | ❌ Not supported. Quote: *"NMOS communication is via Http and currently this implementation only supports Http, no Websockets or MQTT"*. | Already covered by `nmos_is07_unsupported` and the absence of IS-12. |
| **MQTT transport** | ❌ Not supported (same quote). | Already covered. |
| **DNS-SD / mDNS** | ⚠️ *Implied unsupported*: doc says "direct device addressing required via CSV". | Direct-address mode required. See "Deployment modes" below. |

Practical implication: against a Lawo VSM peer, dhs operates in
**unicast direct-address mode**, never advertises a Registry, and
expects the Controller side (us OR Lawo) to walk Node API URLs from a
config list — not from a Query API.

### nmos-cpp (BBC reference implementation)

Source: <https://github.com/sony/nmos-cpp>
Status: TODO — verify live. Reference impl, expected to be 100%
spec-compliant; useful as a positive control for our codec.

### Embrionix / Sony / others

Status: TODO — add rows as we verify against each peer.

---

## Deployment modes (what to expose in dhs CLI)

The matrix-vendor reality forces dhs to support more than one
deployment topology. Each mode maps to a CLI flag set:

| Mode | When to use | CLI flags |
|---|---|---|
| **Full mDNS + Registry** | Greenfield / lab / spec-compliant peers (nmos-cpp). | `dhs registry nmos serve --mdns`<br>`dhs producer nmos serve` (Node auto-discovers Registry)<br>`dhs consumer nmos walk` (Controller auto-discovers Registry) |
| **Unicast Registry** (mDNS off, static Registry hint) | Hardened deployments where mDNS is firewalled but a Registry still exists. | `dhs registry nmos serve --no-mdns --advertise-host <ip>:<port>`<br>`dhs producer nmos serve --no-mdns --registry <ip>:<port>`<br>`dhs consumer nmos walk <registry-ip>:<port>` |
| **Direct-Node** (no Registry at all) | Lawo VSM, vendor environments without IS-04 registration support, end-user environments where mDNS is blocked. | `dhs producer nmos serve --no-mdns --no-registry`<br>`dhs consumer nmos walk-nodes --peer-list peers.csv`<br>`dhs consumer nmos walk-node <node-ip>:<port>` |
| **CSV bootstrap** | Operations team gives dhs a static list of Node URLs (Lawo VSM convention). | `--peer-list peers.csv` (one Node URL per line) |

Default mode: **Full mDNS + Registry**. Deviations fire a startup-log
banner naming the chosen mode so debugging is unambiguous.

---

## Compliance event catalogue (NMOS)

Each event is fired at most once per (session, peer, deviation) tuple
to avoid log spam. All names use snake_case prefixed with `nmos_`.

```
# IS-04 deviations
nmos_registry_not_supported          peer rejects POST /resource → fall back to direct-Node mode
nmos_query_api_missing               peer has no /x-nmos/query/* → can't browse, must walk per-Node
nmos_node_api_version_downgrade      peer offers only v1.0/v1.1, we wanted v1.3
nmos_p2p_only                        no Registry discovered, falling back to peer-to-peer

# IS-05 deviations
nmos_scheduled_activation_unsupported peer rejected activate_scheduled_*; retried as activate_immediate
nmos_constraints_endpoint_missing    GET /constraints returned 404
nmos_bulk_unsupported                POST /bulk/* returned 404 or 405
nmos_master_enable_ignored           PATCH set master_enable=true, GET /active still shows false

# IS-07 / IS-12 / MS-05 deviations
nmos_is07_unsupported                Source advertises events but no WS/MQTT endpoint
nmos_is12_unsupported                Device controls array has no urn:x-nmos:control:ncp/*
nmos_ms05_class_missing              ClassManager doesn't expose class we expected (per BCP-008)

# Discovery deviations
nmos_mdns_disabled                   user passed --no-mdns
nmos_mdns_no_response                mDNS browser ran for full timeout, found nothing
nmos_csv_peer_unreachable            entry in --peer-list refused TCP connect

# Wire deviations
nmos_resource_schema_violation       resource JSON failed BCP-004 / IS-04 schema validation but parsed
nmos_subscription_dropped            WS subscription closed unexpectedly; reconnect attempted
nmos_heartbeat_late                  Node missed POST /health by > 1× ttl interval (warn before purge)
```

Every event is also surfaced via the standard dhs metrics counter
(`dhs_connector_compliance_events_total{proto="nmos",event="..."}`)
once Phase 4+ wires NMOS into `internal/metrics/`.

---

## Rule for new entries

Before adding a row to the vendor matrix:

1. Either **link the vendor's own documentation** stating the
   limitation (preferred), OR
2. **Capture a pcap + write an integration test** reproducing the
   deviation (`tests/integration/nmos/<vendor>_<deviation>_test.go`).

Never add a vendor row from second-hand reports or single-anecdote
observation — the salvo-deviation rule (top-level CLAUDE.md "spec vs.
every shipping controller") requires at least two independent live
controllers OR an explicit vendor doc before we treat any deviation as
"the way the field actually behaves".
