# CLAUDE.md — CCM (EVS Neuron REST API)

Atomic per-protocol context for the CCM connector — the REST API EVS
positions as the acp2 successor on Neuron (#706). Read the root
`CLAUDE.md` first.

**STATUS: SPEC-COLLECTION PHASE — no code lands in this tree until
the spec review below is done and the owner gives the build go
(design-first rule).** The acp2 connector stays regardless: the fleet
is mixed-firmware.

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
