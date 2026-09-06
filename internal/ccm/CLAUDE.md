# CLAUDE.md — CCM (EVS Neuron REST API)

Atomic per-protocol context for the CCM connector — the REST API EVS
positions as the acp2 successor on Neuron (#706). Read the root
`CLAUDE.md` first.

**STATUS: BUILDING (owner go 2026-09-03).** Spec review done; the
device's own OpenAPI (`/api/v1/docs/api.yml`) is captured. Unit 1
shipped: stdlib UUID-keyed codec + `dhs consumer ccm walk` + `ccm
export` (stores api.yml schema + walked tree DM + extract, keyed by
productName@productVersion for firmware diff). Unit 2 shipped:
**recursive full-DM walk** — `codec.ClassifyBody` (branch = JSON array
of child names; resource = object / array-of-objects) + consumer
`WalkTree` follows the device's own shape (no wildcard; GET each node,
recurse only listed children) seeded from the API root OR explicit node
paths (`--start`, for the "root doesn't exist / know the path" case).
`ccm walk --tree` and `ccm export` (dm-tree.json) now capture the WHOLE
model (live BRIDGE@7.0.2: 41 resources / 13 nodes — io/ip, io/madi,
io/sdi, all of matrix, misc, processing, self), not just the six io/ip
stream paths the old Walk hardcoded. The acp2 connector stays
regardless: this bridge runs acp2 + REST/CCM + NMOS at once
(mixed-firmware, multi-protocol box).

**CCM WebSocket (notifications) — NOT served on 7.0.2 (EVS gap, verified
2026-09-03 by packet capture).** 443 negotiates http/1.1 only; the WS is
plain HTTP/1.1-Upgrade-over-TLS on 443 (no separate port; nmap-clean).
Every candidate path 404s (`/api/v1/ws`, `/ws`, ~25 more) via the
confirmed-correct handshake, and **EVS's own Cerebrum 2.9.0 CCM driver
(Prefix `/api/v1`, WS enabled) also fails** — the 1 MB persistent 443
conn in a Cerebrum-side capture is the REST poll, not a live WS. So
`ccm watch` is blocked on an EVS firmware that actually serves the WS;
the message protocol is fully known (PDF §13) and the client is a
ready-to-build unit (h1 Upgrade over TLS, CreateSubscription/relativeUrl/
initial `replace ""`/RFC 6902), live-verifiable only once EVS serves it.

**Firmware reality (BRIDGE 6.7.4, verified live on 10.6.255.102):**
this build serves the CCM resource MODEL (UUID-addressed REST, `/self`,
recursive `{uuid}` paths) but a SUBSET of the CCM 0v1 PROTOCOL — it has
GET+PUT only (no PATCH-maps per §11.2) and **no `/ws`** (verified with
a real Upgrade handshake — every candidate path 404s, not a connection
error). So `watch` over `/ws` is not available on this firmware; the
owner is upgrading to a CCM-enabled build where it is. Deviations are
absorbed + reported, never worked around (spec-strict posture). The
api.yml is stored versioned so the upgrade's diff shows what CCM adds.

---

## Folder layout (target shape, per root CLAUDE.md conventions)

```
internal/ccm/
├── CLAUDE.md    ← this file
├── assets/      ← DROP ZONE: everything EVS provides goes here
│                  (OpenAPI/swagger JSON, PDFs, examples, postman
│                  collections, firmware release notes)
├── docs/        keys/endpoint catalogue + consumer.md (written
│                  during spec review)
├── codec/       (later) stdlib-only — likely thin: HTTP+JSON, the
│                  "codec" is the OpenAPI schema types
├── consumer/    (later) package ccm — implements consumer.Protocol
└── wireshark/   (later) dhs_ccm.lua — HTTP/JSON dissection with
                   per-endpoint Info columns (repo rule: every
                   protocol ships a dissector, no exceptions)
```

## Spec sources (fill as they arrive)

| Artifact | Where | Status |
|---|---|---|
| Live swagger from the on-site Neuron (`GET /swagger.json` or similar) | `assets/` | PENDING — day-1 capture when the device arrives (week of 2026-08-24) |
| EVS-provided docs | `assets/` | PENDING — owner storing them here |

## Review checklist (run when assets land)

1. OpenAPI version + auth model (basic? token? none?) — feeds the
   OAS tooling choice shared with dhs-srv.
2. Endpoint → verb mapping: which canonical verbs (info/walk/get/
   set/ensure/export/watch) the REST surface can back, and what the
   subscription story is (polling? SSE? websocket?) — no-polling rule
   applies if the wire allows better.
3. Object model vs the acp2 tree: can CCM serve the SAME canonical
   tree (label paths, DM identity Model@SwRev) so DMs/manifests/packs
   stay protocol-agnostic? That is the acceptance bar.
4. Parity matrix CCM↔acp2 per object: what disappears, what's new —
   becomes the migration note for mixed fleets.
5. Rate limits / concurrency — scale targets in root CLAUDE.md apply.

## What NOT to do

- No code before the checklist verdict + owner go.
- Never delete or bypass the acp2 connector — mixed-firmware fleets
  keep both.
