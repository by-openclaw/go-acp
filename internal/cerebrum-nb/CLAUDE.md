# CLAUDE.md — Cerebrum NB (EVS Cerebrum Northbound API / Neuron Bridge)

Atomic per-protocol context. Read the root `CLAUDE.md` first.

---

## Authoritative spec

- **EVS Cerebrum Northbound API 0v16** (authoritative) — at
  [assets/Cerebrum Northbound API 0v16.pdf](assets/Cerebrum%20Northbound%20API%200v16.pdf).
  The codec, consumer, CLI, fixtures, dissector and docs all target 0v16.
- **0v13** is the **historical baseline** retained for reference at
  [assets/Cerebrum Northbound API 0v13.docx](assets/Cerebrum%20Northbound%20API%200v13.docx)
  (image-heavy original) and the text-only export at
  [assets/cerebrum_northbound_api_full_v0_13.docx](assets/cerebrum_northbound_api_full_v0_13.docx).
  0v16 is a superset of 0v13 — the 0v13 roots still decode unchanged; the
  0v16 additions (DEVICE_CONFIGURATION + RESULT, CONTINUE,
  WILDCARD_COMPLETE, the LOGIN_REPLY DATASTORES body, enriched `*_change`
  events, the 5-value LOCK enum) layer on top.
- The wire-key catalogue derived from the spec lives at
  [docs/keys.md](docs/keys.md). That file is fact-only — element /
  attribute / enum names verbatim.
- Operator + integrator docs: [docs/README.md](docs/README.md),
  [docs/consumer.md](docs/consumer.md), [docs/verbs.md](docs/verbs.md),
  [docs/runbook.md](docs/runbook.md), [docs/provider.md](docs/provider.md)
  (consumer-only by design), [docs/keys.md](docs/keys.md).

The spec is the source of truth. Do not document deviations as truths
in this file; deviations are absorbed by the codec and surfaced as
named compliance events emitted from the codec/consumer paths
(catalogue + descriptions in [docs/consumer.md](docs/consumer.md)
"Compliance events").

---

## Folder layout

```
internal/cerebrum-nb/
├── CLAUDE.md             ← this file (spec entry-point only)
├── codec/                stdlib-only XML codec for §2/§4/§5 elements
│   └── ws/               stdlib-only RFC 6455 WebSocket client
├── consumer/             package cerebrum_nb — implements consumer.Protocol
├── wireshark/            dhs_cerebrum_nb.lua — full WS-frame + XML
│                         payload dissector
├── docs/
│   ├── keys.md           wire-key catalogue (spec extract)
│   ├── consumer.md       CLI walkthrough
│   ├── verbs.md          13-section verb + sample reference (0v16)
│   ├── runbook.md        operator quick-reference card
│   ├── provider.md       provider rationale — consumer-only by design (N/A)
│   └── README.md         one-page overview
├── testdata/
│   ├── fixtures/         codec-generated 0v16 wire frames + drift-guard
│   └── exports/          (reserved; consumer-only — no served tree)
└── assets/               spec PDF / DOCX / OCR
```

---

## Plugin scope

| Aspect | Value |
|---|---|
| Registry name | `cerebrum-nb` |
| Default port | **40007** (configurable in the Cerebrum app) |
| Transport | WebSocket — `ws://host:40007` (TLS via `wss://`) |
| URL path | none — connect to the host:port only |
| Framing | one XML document per WebSocket text message (UTF-8) |
| Licensing | one northbound licence per active WebSocket session |

**Consumer-only by design.** Cerebrum is a northbound API we *consume*;
there is no "serve Cerebrum" role to implement, so the provider deliverable
is **N/A, not deferred** — see [docs/provider.md](docs/provider.md) for the
full rationale.

---

## Wire layer

### WebSocket (RFC 6455) — hand-rolled

Implemented in [codec/ws/](codec/ws/). Stdlib-only, no `dhs/*` imports —
lift-ready per root CLAUDE.md "Architecture principles" (Library
independence).

### XML

UPPERCASE on the wire (per live captures); decoder accepts any case
(case-folded AST), encoder emits UPPERCASE.

### MTID

Unsigned 32-bit, decimal-as-string on the wire. Allocated via
`atomic.Uint32` starting at 1; reuse fires `cerebrum_mtid_reused`.

---

## Compliance events

Every spec deviation absorbed by the decoder fires a named event via
`compliance.Profile`. Catalogue + descriptions in
[docs/consumer.md](docs/consumer.md) "Compliance events". NACK codes
from §6 each become a `cerebrum_nack_<code>` event.

---

## Read the spec before designing

When in doubt, the wire format and command catalogue are in the **0v16
PDF** in [assets/](assets/) — every element, every attribute, every
enum is documented there. The 0v13 DOCX is the historical baseline.
`docs/keys.md` is a reading aid; if it disagrees with the 0v16 PDF, the
PDF wins.
