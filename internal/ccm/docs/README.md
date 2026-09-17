# CCM docs

Written during the spec review (see ../CLAUDE.md checklist):

- `keys.md` — endpoint/field catalogue extracted from the OpenAPI
  spec (fact-only, names verbatim — same discipline as
  cerebrum-nb/docs/keys.md)
- `consumer.md` — CLI walkthrough once the connector exists
- `acp2-parity.md` — per-object CCM↔acp2 matrix (the migration note
  for mixed-firmware fleets)
- `runbook.md` — operate the **provider** (`dhs producer ccm serve`):
  capture a device, replay it as a CCM device for Cerebrum, verify,
  troubleshoot. The provider's own tech doc is served rendered at
  `/x-dhs/readme` (source: `../provider/README.md`).
