# Verb-parity audit — 2026-08-22

Scope: every consumer verb × every connector — same flags, same output
shapes, same exit codes (owner mandate: "each protocol has same verbs,
same results"). This audit is also the endpoint inventory for the
dhs-srv OpenAPI 3.0 spec (objective B): one uniform verb = one uniform
endpoint.

Binding refs: ADR-0002 (canonical verbs/flags), ADR-0007 (ensure),
ADR-0028 (artifact homes), [verbs.md](../verbs.md) (per-model verb
matrix). Per verbs.md §1, parity is judged WITHIN a protocol model —
forcing `walk` onto a tally-push wire is noise, not parity.

## What is already uniform (verified)

| Contract | State |
|---|---|
| Exit codes | LOCKED 0 (ok) / 1 (transport, device) / 2 (validation) — pinned by `TestExitCode_LockedContract`, cross-OS |
| ensure semantics | ADR-0007 `--check` / `diff[]` / run-twice=0 on every write-converge verb, all connectors |
| usage / replace | all 4 matrix connectors, one grammar (#722, #746) |
| export file-set | router pack #738: canonical `dest,srce,levels`, meta.json, omit-don't-fake |
| `--capture` JSONL | tree protos (walk/get/set/…), probel-sw08p (global), probel-sw02p (global) |
| ADR-0028 default homes | exports/usage/extract land in snapshots/… everywhere they exist |
| `--timeout` | present on every dialing verb |
| Ember+/matrix behavior rules | 1to1/1toN absolute, NtoN explicit disconnect — pinned |

## Gap list → units (priority order)

| # | Gap | Connectors | Unit |
|---|---|---|---|
| G1 | `--output json` missing on point-read verbs (`get`, `info`, probel `interrogate`/`tally-dump`/`dual-status`/name reads, sw02p `interrogate`/`status`/`router-config`, cerebrum reads already have it) — ansible/scripting needs machine output without full export | tree protos, probel×2 | one PR per connector family |
| G2 | probel-sw02p has `export` but no `import` — round-trip broken on one connector (xpoints apply via rx 02/66 from the canonical CSV, ensure semantics) | probel-sw02p | one PR |
| G3 | listen/watch naming split: tsl says `listen`, osc says `watch` for the same bind-and-print role | tsl, osc | alias both, document canonical (`watch`) |
| G4 | names-not-ids: default display resolves labels where the wire has them (walk/get/tree on tree protos show label paths — mostly true; matrix reads show bare ids unless `--names`) — make `--names` accepted uniformly incl. cerebrum spelling parity | all | one PR |
| G5 | per-verb `--help`: args complete + one runnable example per verb, CI-verified (topic 8) | all | one PR per dispatcher |
| G6 | log severity contract: slog levels exist; missing `--log-format syslog` (RFC 5424, incl. critical mapping) + Grafana/Loki wiring doc | all | one PR |
| G7 | ansible: generic `dhs_verb` role (protocol/verb/args/idempotency assert) + example plays for get/set/extract/export/ensure | ops | one PR (role + plays) |
| G8 | discover parity inside the matrix model: sw08p has `discover`, sw02p does not (dual-status + router-config + tally sweep = same survey) | probel-sw02p | one PR |

Out of scope here: NMOS verbs (own epic A), producer verbs (separate
table in verbs.md), REST/WebUI (objective B — consumes this audit as
its endpoint inventory).

## Verb surface snapshot (2026-08-22)

- Tree/DM generic table (acp1/acp2/emberplus): 28 verbs — info walk
  tree get set inc dec reset ensure watch export import extract diff
  convert discover matrix usage replace invoke stream profile diag
  validate health status bench (+ per-proto gating documented in help).
- probel-sw08p: 27 verbs (crosspoint + protect + names + salvo +
  usage/replace + export/import + bench + discover).
- probel-sw02p: 19 verbs (crosspoint + salvo + protect + status +
  router-config + usage/replace/export + watch). Missing vs model:
  import (G2), discover (G8).
- cerebrum-nb: 33 verbs (bridge model — routing, mnemonics, locks,
  categories, salvos, device tree/DM domain, usage/replace, export/
  import).
- tsl: listen (G3) + validate; osc: watch/send/fader/validate.
