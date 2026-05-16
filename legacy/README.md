# legacy/

Local-only working copies of dhs 2016-era code and audit material that
predate the current Go rewrite. Everything under this tree is **scheduled
for removal** once all connectors meet the per-connector definition of
done (ADR-0025).

Nothing here is built, linked, or imported by current code. The contents
are kept on developer machines for reference during the rewrite (cross-
checking wire-format behaviour against historical implementations,
recovering test fixtures, etc.).

## Layout

| Path | Provenance | Why it's here |
|---|---|---|
| `audits/BY-COMMON/` | dhs 2016 common library | reference for shared structures used by the old DHS-CORE / DHS-HOST split |
| `audits/BY-DHS-CORE/` | dhs 2016 core framework | reference for the original device-registry / session model |
| `audits/BY-DHS-DEVICE-DRIVER-AXON-ACP/` | dhs 2016 ACP1/ACP2 driver | reference for Synapse ACP1/ACP2 wire handling |
| `audits/BY-DHS-DEVICE-DRIVER-EMBER/` | dhs 2016 Ember+ driver | reference for Glow/BER/S101 handling |
| `audits/BY-DHS-DEVICE-DRIVER-ROLLCALL/` | dhs 2016 RollCall driver | reference for Snell RollCall (future connector in this repo) |
| `audits/BY-DHS-HOST/` | dhs 2016 host process | reference for supervisor / IPC patterns |
| `audits/audits/`, `audits/cerebrum - forms/`, `audits/lawo-walk/`, `audits/vsm/` | mixed audit dumps | snapshots from real-device validation runs |
| `audits/*.json`, `audits/*.jsonl`, `audits/*.EmBER`, `audits/*.xml`, `audits/*.pptx`, `audits/*.md` | mixed | DM caches, S101 captures, vendor configs, the SMH Vision requirements deck |
| `emberplus/smh/` | BY-RESEARCH TS Ember+ emulator | reference TypeScript emulator (ports 9000 / 9090 / 9092); convention — targets labeled `1`, sources labeled `2`. Used historically as the testbed for Ember+ wire-format work |

## Tracking

The entire `legacy/` tree is gitignored (`/legacy/*` in the repo root
`.gitignore`); only this `README.md` is committed. Nested git repositories
inside (e.g. `emberplus/smh/common/asn1/.git/`) are local working-copy
state — they are not submodules, are not referenced from `.gitmodules`,
and are not tracked.

## Removal

When all connectors satisfy the five ADR-0025 deliverables (consumer +
producer + integration test driving the CLI + `.cache/dm` generator +
Wireshark `.lua`), this folder is removed in one commit. Do not add new
material here without recording its provenance in the table above.
