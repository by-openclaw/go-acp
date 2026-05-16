# archive/ — local working scratch (gitignored)

This directory is gitignored except for this README. It holds:

- Wire-trace audits and ad-hoc debug exports that historically lived in
  `.audit/` and `audits/` at the repo root.
- One-off CSV/JSON exports produced during interactive debugging
  (`acp1-slot1.*`, `frame-*.json`, `logan.json`, `neuron-senders.csv`,
  `slot1.csv`, etc.).
- Stale issue notes that were dropped under `bin/` over time.
- Per-protocol demo trees superseded by `.cache/dm/<proto>/<identity>.json`
  (the canonical DM hot-load location per ADR-0022).

Files here are NOT loaded by tests, the CLI, or any plugin. They exist
only so that historical traces stay reachable on disk without polluting
the source tree.

If you need to share a trace with someone, copy it out — do not commit
it. Wire traces are local-only per ADR-0021.
