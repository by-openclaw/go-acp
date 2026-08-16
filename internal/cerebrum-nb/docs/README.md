# Cerebrum NB plugin

EVS **Cerebrum Northbound API 0v16** (authoritative) — also branded
**Neuron Bridge**. XML over WebSocket, default port **40007**.
0v13 is the historical baseline; 0v16 is a superset.

## Quick links

| Doc | What it covers |
|---|---|
| [keys.md](keys.md) | Authoritative element / attribute / enum catalogue (the wire facts) |
| [verbs.md](verbs.md) | 13-section verb + sample reference (device-config, 5-mode lock, datastore, inventory + snapshot export/import ensure) |
| [consumer.md](consumer.md) | CLI walkthrough + portable Windows install recipe |
| [runbook.md](runbook.md) | Operator quick-reference card |
| [provider.md](provider.md) | Provider rationale — consumer-only by design (N/A) |
| [../CLAUDE.md](../CLAUDE.md) | Atomic per-protocol context — wire layer, mtid, quirks, "what NOT to do" |

## Verb catalogue (complete)

| Group | Verb | What it does |
|---|---|---|
| Session | `connect` | POLL round-trip (auto-LOGIN with `--user/--pass`) |
| | `listen` | SUBSCRIBE watcher — routing / category / salvo / device events until Ctrl+C |
| | `keepalive-probe` | diagnostic idle hold (TCP keep-alive observation) |
| **Inventory** (one-shot OBTAIN, no subscribe) | `list-sources` | every configured source: **ID · capability levels · label · alt labels** (`--id N` single, `--out` CSV) |
| | `list-dests` (alias `list-destinations`) | same for destinations |
| | `list-levels` | the level catalogue: ID + name (+ alts) |
| **Snapshot** | `export` | one-shot OBTAIN reads → CSVs. Crosspoints only: `--out FILE` (`--level N` per-level). Full Route-Master snapshot: `--out-dir DIR --prefix P` → `P-src.csv` / `P-dst.csv` / `P-level.csv` / `P-xpoint.csv` (uniform `alt_1..alt_N` columns) |
| | `import` | **ENSURE (ADR-0007)**: read live state, diff vs CSVs, converge only differences. `--in-dir DIR [--prefix P]` (reads the set `export --out-dir` wrote) or per-file `--xpoint` (routes; never disconnects) + `--src/--dst/--levels` (labels, any slot incl. alternates). `--check` = would_change report, sends nothing. Empty cell = untouched; `--allow-clear` makes an empty managed cell a clear-write (**live-UNVERIFIED**). Run-twice = 0 |
| Routing | `route` | single / batch (`--route d:s:l`) / `--csv` crosspoint takes |
| | `lock` / `unlock` | 5-mode DEST/SRCE lock |
| Labels | `set-mnemonic` | one label write — primary or `--alt N` slot |
| | `set-tags` | Routemaster tags |
| Reads | `list-devices` / `device-details` / `device-value` | device domain |
| | `list-categories` / `category-details` | category domain (items = SOURCE/DEST names) |
| | `list-salvo-groups` / `list-salvo-instances` / `salvo-instance-details` | salvo domain |
| | `tree` | NB catalogue as the canonical ASCII / PlantUML tree — Categories (§5.2: categories → SOURCE/DEST/nested-CATEGORY items) + Salvos (§5.3: groups → instances → metadata; salvo item rows are not exposed over NB) — `--domain salvos\|categories\|all` |
| | `obtain-datastore` | data store fetch |
| Writes | `salvo` (run/save/rename/description/delete) · `category` (create/modify/delete) · `set-value` · `device-config` | §4 actions |

Common flags: `--port` (default 40007) · `--user/--pass` (or `$DHS_CEREBRUM_USER/_PASS`)
· `--tls` · `--timeout` · `--debug` (RX/TX XML to stderr) · **`--log FILE`**
(full debug log incl. RX/TX XML to a clean UTF-8 file — no PowerShell `2>`
wrapping; note it contains the LOGIN frame in clear text).

## Status

- Consumer plugin **complete at 0v16**; provider **N/A by design**
  ([provider.md](provider.md)).
- **Live-validated against a production Cerebrum (NOC, 2026-08)**: login,
  listen, list-sources/dests/levels (12k+ resources, ID+labels+capability
  levels verified against the Route-Master UI), full snapshot export,
  crosspoint export (sentinel-filtered), route takes, and the full
  `import` ensure loop — untouched snapshot `--check` = would_change=0
  (1.7k xpoints + 25.9k label rows), a single CSV edit detected as
  exactly 1 change, and apply wrote that one alternate label
  (confirmed in the Cerebrum UI).
- **Pending staging validation**: `--allow-clear` (the `MNEMONIC=""`
  clear form) only.

## Live-wire truths (production-verified; NOT in the 0v16 PDF)

- `ASSOCIATION_n` **index = Routemaster level** of the binding; association
  `SRCE_ID/DEST_ID` is a device-port UID, `RM_LEVEL_ID` is the level inside
  the bound device. A row with no ASSOCIATIONS block = resource unbound.
- ROUTE snapshot covers **every dest × level cell**; `SOURCE_ID` `0`,
  `4294967294` (0xFFFFFFFE) and `4294967295` are **no-route sentinels**.
- OBTAIN/SUBSCRIBE filters must carry **every wildcardable attribute**
  (`LEVEL_ID="*"` included) or the server NACKs (9/10). RX rows echo the
  filter ("attributes as per TX") — `LEVEL_ID="*"` in a reply is the echo,
  never data.
- Only the MTID-carrying `WILDCARD_COMPLETE` ends a wildcard read; the
  server also emits spurious MTID-less ones per event.
- Alternate label sets are addressed **by index only**; the set names are
  Cerebrum config (this plant: 1=Panels, 2=Ref_Short_Edit, 3=Ref_Long_Edit,
  4=Engineer, 5=Ref_Long_Tech). No NB command lists that mapping.
- The Cerebrum cluster does **not** sync NB credentials between
  primary/backup.

## Wire samples

Wire samples in these docs come from the committed
[codec-generated fixtures](../testdata/fixtures/README.md), cross-checked
2026-08 against live captures from the production NOC Cerebrum.

## Spec sources

- Authoritative: `assets/Cerebrum Northbound API 0v16.pdf`
- Historical baseline: `assets/Cerebrum Northbound API 0v13.docx` +
  `assets/cerebrum_northbound_api_full_v0_13.docx`
- Live wire captures from production Cerebrum servers, when an NB licence
  is available, override spec text where the two disagree.
