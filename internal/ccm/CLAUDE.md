# CLAUDE.md — CCM (EVS Neuron REST API)

Atomic per-protocol context for the CCM connector — the REST API EVS
positions as the acp2 successor on Neuron (#706). Read the root
`CLAUDE.md` first.

**STATUS: BUILDING (owner go 2026-09-03).** Spec review done; the
device's own OpenAPI (`/api/v1/docs/api.yml`) is captured. Unit 1
shipped: stdlib UUID-keyed codec + `dhs consumer ccm walk` + `ccm
export` (stores api.yml schema + walked tree DM + extract, keyed by
productName@productVersion for firmware diff). The acp2 connector
stays regardless: this bridge runs acp2 + REST/CCM + NMOS at once
(mixed-firmware, multi-protocol box).

**Unit 2 (PR #1065): the PROVIDER.** `dhs producer ccm serve` replays a
captured device model (a dm-tree: resource path → resource JSON, the
shape `ccm export` captures) so Cerebrum drives dhs as a CCM device —
emulate before hardware. Compliance boundary, enforced by construction:
`/api/v1` is the CCM protocol 100% to the spec (self-describing tree,
the OpenAPI at `/api/v1/docs/api.yml`, §11 PUT + PATCH with an empty
202, §12 `{code,message}`, `uuid`/`id` immutable, `/status` and the
spec GET-only); every dhs addition (landing, rendered README,
capabilities) lives under `/x-dhs` via the one `HandleExtension` entry
point and can never change what a controller observes. Matrix (§17) is
**in scope** (owner reversed the 2026-08-22 exclusion on 2026-09-09 —
it was made unaware matrix is in the protocol): 16 `/v1/matrix/...`
paths, per-essence matrices, UUID-addressed, `main`/`backup` are
ST 2022-7 redundancy levels, `current` read-only; the emulator stores
writes but does not yet run routing semantics — next matrix unit. The
`/ws` change stream is a later unit. Operate it per `docs/runbook.md`.

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
├── docs/        spec review, keys/endpoint catalogue, runbook.md
│                  (operate the provider), README index
├── codec/       shipped — stdlib-only UUID-keyed model (Device,
│                  Stream, Leg) + testdata/neuron-api-1.0.0.yml, the
│                  device's real OpenAPI 3.1.2
├── consumer/    shipped — package ccm: walk + export
├── provider/    shipped (PR #1065) — package ccm: replays a captured
│                  dm-tree as a CCM device. /api/v1 = the protocol,
│                  100% to spec; /x-dhs = dhs additions only. Own
│                  README.md served rendered at /x-dhs/readme
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
