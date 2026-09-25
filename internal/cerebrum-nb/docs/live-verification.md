# Cerebrum NB — live verification log

What has actually been run against a real Cerebrum, and what it did.
Written so an audit reads results instead of re-deriving them, and so a
claim in another doc can be checked against something dated.

Peer: **`vm-cerebrum-stg-01`, 10.6.250.5:40009** — a single, standalone,
licensed server (`Is_Redundant_System=0`, `Northbound_API_Licenses=10`
with `In_Use=0`). Credentials via `$DHS_CEREBRUM_USER` /
`$DHS_CEREBRUM_PASS` from `.secrets/staging-cerebrum.json`.

Plant: 4 sources, 4 destinations, 3 levels, 2 categories, 0 salvo
groups, 0 datastores.

## Read verbs — 2026-09-25

All pass: `health`, `connect`, `keepalive-probe`, `list-devices`
(+`--names`), `device-details`, `device-value`, `list-categories`,
`category-details`, `list-salvo-groups`, `list-sources`, `list-dests`,
`list-levels`, `tree` (`--domain all` and `--device`), `get`,
`extract`, `export` (crosspoints and `--out-dir`), `usage`
(`--srce`/`--dest`), `listen`, `watch`.

`obtain-datastore` NACKs 15 — correctly: this server's login reply
carries an empty `<DATASTORES/>`, so there is no datastore to obtain.

The codec was also read against the wire: a captured session replayed
through `validate` decoded **64 of 64 frames** (54 rx, 10 tx) with no
NACK, no decode error and no case deviation.

## Write verbs — 2026-09-26

Every test below was change-then-restore, and the whole plant was
diffed against a pre-test export afterwards: **all seven exported files
byte-identical**.

| Verb | Result |
|---|---|
| `route` | applied and restored. **The server moves every level even when one is named** — `--level 1` on dest 1 moved levels 1, 2 and 3 |
| `category` create / modify / delete | all three, including a nested `CATEGORY` item — the cat → sub-cat → resources navigation renders in `tree --domain categories`. `--name` is required on create |
| `set-mnemonic` | applied and restored (`"Proc 2"` → `"DHS TMP"` → `"Proc 2"`) |
| `lock` / `unlock` | applied and released across all three levels; `LOCKED_BY` carried the session user and cleared on release — this closes the "release is untested" note in [keys.md](keys.md) |
| `set-value` | writes (`CorLinkStats.ResetStats` `"0"` → `"1"`) and is idempotent: writing a value it already holds reports `already converged` and sends nothing |
| `import` | `--check` reports nothing on an unchanged plant and exactly the edited rows after one row is changed; applying reports `changed=3`, and a second run changes nothing |
| `device-config` | **refused by the server**: `<DEVICE_CONFIGURATION TYPE="ADD" …>` → `NACK 2:UNKNOWN_COMMAND`. The command itself is unknown to this Cerebrum, so no device — router or virtual — can be created over NB here |

## Known gaps on this connector

- **Metrics reach only `watch`.** `watch --metrics-addr :9100` serves
  Prometheus `/metrics` + `/snapshot.json` with `proto` / `device` /
  `role` labels, and one counter set survives a reconnect —
  `dhs metrics show` scrapes it (verified 2026-09-26:
  `dhs_connector_rx_bytes_total{device="Cerebrum",proto="cerebrum-nb",role="consumer"} 1432`).
  **No other verb serves one**, so a plain `export` or `listen` still
  reports its counters to the log sink and nowhere else.
- **`alarm suggest` cannot reach this connector.** It connects without
  LOGIN and then fails `no slot answered a walk`, because `Plugin.Walk`
  and `Plugin.GetSlotInfo` are deliberate "not applicable" stubs. Every
  neutral verb built on `Walk` is blind here; the connector's own verbs
  are not.
- **No `--virtualise` flag**, though the codec carries the field.
- The alarm template is filed `Cerebrum@unknown` because the server
  publishes no identity object.
- Redundancy, federation, SQL AlwaysOn replicas and per-service
  `Ext. Log Status` cannot be read on a standalone — their objects are
  `available=0` or empty here.
