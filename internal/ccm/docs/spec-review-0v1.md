# CCM spec review — Protocol Description 0v1 (EVS, June 2026)

Reviewed 2026-08-22 against the checklist in ../CLAUDE.md. Source:
`assets/EVS CCM Protocol Description 0v1.pdf` (24 pp, Ian Hollamby).

**Verdict: STRONG FIT. Every dhs canonical verb has a natural CCM
backing, the no-polling doctrine is EVS's own (§13.3), and the stack
aligns with our dhs-srv standards choice (OAS 3.1 + problem details +
WS). Build estimate: medium — heavy reuse of existing dhs parts.**

**SCOPE (owner, 2026-08-22): CCM's job in dhs is the acp2 successor —
Tree/DM device control/config/monitoring of Neuron cards. It is NOT a
router protocol for us: routing stays with the Route Master
(Cerebrum) and the router protocols (probel sw08/…), and salvos live
inside Cerebrum. The §17 matrix section is therefore OUT OF SCOPE for
the dhs connector (read-only consumption at most, if ever); the
undefined matrix-write is downgraded from blocking to informational.
An acp2↔CCM diff report is nice-to-have only — no differences
expected, not a deliverable.**

## 1. Transport, spec, auth

| Aspect | CCM 0v1 | dhs consequence |
|---|---|---|
| Spec | OpenAPI **3.1.x**, served at `/api/docs/openapi.yml`, downloadable unauthenticated | same OAS tooling as dhs-srv; day-1 capture = this file |
| Transport | HTTPS default (http lab-only), port 443 default | TLS client + `--insecure`/custom-CA flags; http mode for tshark captures |
| Auth | JWT Bearer (§23, PROPOSAL): `/api/auth/login|refresh|change-password`; refresh-on-401 | token manager in the session layer; creds via .secrets like cerebrum |
| Errors | §12 `GenericApiMessage{code,message}` for mutations; §23.6 RFC 7807 problem details for auth | TWO error shapes — EVS Q3 below |
| JSON | camelCase, human-readable enums, ISO 8601/epoch datetimes, spinal-case URIs | canonicalization layer maps to our vocabulary |

## 2. Canonical verb mapping (the acceptance bar — MET)

| dhs verb | CCM backing |
|---|---|
| `info` | `GET /api/self` — productName/productVersion/apiVersion/components → **DM identity = productName@productVersion**, slots ≈ components |
| `walk` | §4.4 recursive partial-path discovery: every `{id}` segment GETs as an id array, recursively → a GENERIC schema-free tree walk; every uuid object MUST carry `name` → names-not-ids natively |
| `get` | `GET` resource (1:1 with subscribable payloads) |
| `set` / `ensure` | `PATCH` (MANDATORY per §11.2, maps not arrays) → **202 async** + split request/status (§11.3): converge confirmation = read-back or subscription, not the HTTP reply. ADR-0007 fits; the check loop just moves after the 202 |
| `watch` | WS `/ws`: CreateSubscription (wildcard `*` per path param), initial FULL state as one JSON Patch `replace ""` event **sent before the subscription response** (§13.3.8), then incremental RFC 6902 patches; dedup not guaranteed; reconnect ⇒ discard cache + resubscribe |
| `matrix` / `usage` / `replace` | §17: `GET /matrix/{id}/info` (MatrixInfo: sources/destinations as path refs or inline MatrixEntry; per-channel slot uuids; `slot_type` for mixed essences; **named levels** with read/write flags e.g. current(ro)/main/backup) + `GET /matrix/{id}/state` (dst-uuid → src-uuid map; multi-level extended form). usage = invert the state map; levels column carries level NAMES (our grammar already stores levels as strings) |
| `export` (router pack) | descriptor from info (state map ⇒ behavior 1toN), xpoint.csv `dest,srce,levels` with uuids, src/dst.csv from mandatory `name`s |
| `discover` | `GET /api` root domains + `/api/self` + matrix enumeration |

## 3. Reuse map (why the build is medium, not large)

| Existing dhs part | Reused for |
|---|---|
| `internal/cerebrum-nb/codec/ws` (stdlib RFC 6455 client, lift-ready) | the `/ws` backchannel |
| ensure/ADR-0007 framework, run-twice asserts | PATCH converge + post-202 confirm |
| canonical tree + DM/manifest (ADR-0022) | identity + resource tree snapshot |
| router pack grammar + usage/replace helpers (#722/#738/#746) | matrix legs |
| compliance.Profile | absorbing spec Qs/deviations |
| New codec pieces | JSON Patch applier (RFC 6902) + JSON Pointer (6901), WS message types, OpenAPI doc loader — all stdlib JSON |

Wireshark: `dhs_ccm.lua` dissects HTTP+JSON and WS frames; TLS
captures need lab http mode or SSLKEYLOGFILE — noted for the runbook.

## 4. Questions for EVS (re-prioritized per the dhs scope: Tree/DM
## device control — routing/salvos stay with Cerebrum/probel)

1. §5.2 IO endpoint minimum expectations — "Paul to specify" (open in
   their doc). This is now our TOP question: the IO/config resources
   ARE the acp2-successor surface.
2. Auth §23 is PROPOSAL — confirm the on-site Neuron firmware's actual
   state (and whether lab http is enabled on it).
3. Which error schema governs non-auth mutations — §12
   GenericApiMessage or RFC 7807 (§23.6)? (Recommend 7807/9457
   everywhere — matches their own auth section.)
4. §8 by-name alias routes (`/senders/by-name/...`) — their own open Q;
   we'd consume it gladly (names-first UX).
5. Rate limiting per subscription "future" (§13.3.2) — any current
   server-side caps we should respect?
6. Informational only (matrix is out of dhs scope — owner decision):
   §17 defines no matrix WRITE (GET info/state only), and salvos/
   locks/protects are absent — consistent with routing being the
   control system's job, worth confirming that reading of intent.

## 4b. Oracles (owner facts, 2026-08-22)

- **Cerebrum implements a CCM client driver** (owner's installed
  version). Consequences:
  - Tier-3 oracle for a future `dhs producer ccm serve`: emulate a
    Neuron (or any device) over CCM and the objects appear in the
    Cerebrum UI — the emulate-before-hardware pillar on a protocol
    designed for discovery, expected to avoid the acp2
    browser-partial-subtree class entirely.
  - Cerebrum-side wire captures of CCM (Cerebrum ↔ real Neuron)
    become a second spec source alongside the PDF, exactly like the
    NB/acp2 methodology.
- Tier-2 oracle for the consumer: the on-site real Neuron (arriving
  week of 2026-08-24).

## 5. Day-1 plan when the on-site Neuron arrives

1. `GET /api/docs/openapi.yml` → commit to assets/ (the live spec
   artifact, #706's unpark trigger).
2. Root discovery + `/self` + full recursive walk capture (http lab
   mode if available, else SSLKEYLOGFILE) → `dhs_ccm.lua` first cut
   from real frames.
3. Subscribe wildcard on one resource class; capture initial-state +
   patch flow.
4. Diff the real endpoint surface against this review; update the
   EVS question list; then owner decides the build go (#706).
