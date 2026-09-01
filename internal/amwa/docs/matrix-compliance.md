# NMOS — matrix-vendor compliance tracker

Per the dhs spec-strict / no-workaround posture (top-level CLAUDE.md
"Spec-strict, no-workaround posture"), we implement every NMOS
specification exactly as written. When a real-world peer deviates from
the spec, we **absorb the deviation, keep running, fire a compliance
event naming what we saw**, and document the vendor here.

This file is the audit trail. Add a row whenever a new vendor is
verified live; never silently work around a deviation.

> Same audit-trail pattern as every other dhs protocol: absorb peer
> deviations into the codec, fire a named compliance event, never
> silently work around. See root `CLAUDE.md` "Spec-strict,
> no-workaround posture".

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
| **Direct-Node** (no Registry at all) | Lawo VSM, vendor environments without IS-04 registration support, end-user environments where mDNS is blocked. | `dhs producer nmos serve --no-mdns --no-registry`<br>`dhs consumer nmos walk --node http://<node-ip>:<port>` (one call per Node) |
| **mDNS direct-Node** (mDNS on, no Registry) | EVS Cerebrum peer-to-peer mode and any deployment where mDNS works but no Registry is provisioned. Nodes are mDNS-discovered on `_nmos-node._tcp` (peer service type) and addressed directly. See [`cerebrum-interop.md`](cerebrum-interop.md). | `dhs producer nmos serve --mdns --no-registry`<br>`dhs consumer nmos discover --mdns --service _nmos-node._tcp`, then `walk --node http://<host>:<port>` |
| **CSV bootstrap** | Operations team gives dhs a static list of Node addresses (Lawo VSM convention). | `--peer-list peers.csv` on `discover` / `system` (CSV: `host,port[,api_ver]` per line) |

Default mode: **Full mDNS + Registry**. Deviations fire a startup-log
banner naming the chosen mode so debugging is unambiguous.

---

## Compliance event catalogue (NMOS)

The events the code emits today (every emission site lives under
`internal/amwa/`). All names use snake_case prefixed with `nmos_`.
Events fire on each occurrence — there is no per-(session, peer,
deviation) deduplication in the reporter today.

```
# IS-04 deviations
nmos_is04_schema_deviation           decoded resource does not match the AMWA schema at that minor; absorbed at Warn
nmos_is04_unknown_field              peer sent a field the modelled resource does not carry; absorbed + recorded

# IS-05 deviations
nmos_is05_no_transport_file          sender served no transport file; receiver still targeted by sender id
nmos_is05_empty_transport_file       sender served an empty transport file
nmos_is05_active_unreadable          receiver's /active state unreadable during a dry-run read-back
nmos_is05_master_enable_ignored      stage accepted but device reports master_enable=false; no signal will flow
nmos_is05_destination_ignored        device kept destination_ip empty / 0.0.0.0 after a stage that set it

# IS-09 deviations
nmos_is09_global_deviation           /global fails the IS-09 schema; absorbed and used anyway

# Version negotiation
nmos_no_common_api_ver               no common IS-04 api_ver between us and the peer's advertised set

# Query API
nmos_query_collection_failed         one catalogue collection failed during a walk; snapshot is partial
```

Events surface through `spec.Reporter` (`codec/spec/compliance.go`):
production wires a logger-backed reporter in `cmd/dhs/cmd_nmos.go`;
tests assert against `spec.SliceReporter`.

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
