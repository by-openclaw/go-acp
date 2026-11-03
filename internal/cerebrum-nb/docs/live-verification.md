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
| `device-config` | superseded, see 2026-11-02. **refused by the server**: `<DEVICE_CONFIGURATION TYPE="ADD" …>` → `NACK 2:UNKNOWN_COMMAND`. The command itself is unknown to this Cerebrum, so no device — router or virtual — can be created over NB here |

## Write verbs, what 2026-09-26 did not cover — 2026-09-27

Same peer, same change-then-restore rule; the plant was byte-identical
to the pre-test export afterwards.

| Case | Result |
|---|---|
| `category --op modify-all` / `modify` / `modify-desc` / `delete-item` | all applied. **`delete-item` renumbers**: deleting item 1 of 2 leaves the other as `ITEM_1` |
| `import` growing a category (1 → 3 items) | `changed=3`, then 0 |
| `import` **shrinking** a category | **was refused**: extra slots were cleared with `MODIFY_ITEM ITEM_TYPE=BLANK` → `NACK 8`. Now removed with `DELETE_ITEM`, last slot first: `changed=2`, then 0 |
| `import` creating a category from CSV | `CREATE` + items, `changed=3`, then 0 |
| `category --op create` | needs `--name` only (it wrongly required `--category`) |
| `lock` **without `--mode`** | **was refused**: it sent `LOCK="PROTECT"` → `NACK 8`. Default is now `PROTECTED` |
| `lock --mode locked` / `protected` | applied; the readback reports the new state (`was RELEASED` → `LOCKED` → `PROTECTED`) |
| `lock --mode locked_path` / `protected_path` | **refused by this server** on a routemaster destination (`NACK 8`) |


## Known gaps on this connector

- **Metrics reach `listen` and `watch`.** `--metrics-addr :9100` serves
  Prometheus `/metrics` + `/snapshot.json` with `proto` / `device` /
  `role` labels, and one counter set survives a reconnect —
  `dhs metrics show` scrapes it (verified 2026-09-26:
  `dhs_connector_rx_bytes_total{device="Cerebrum",proto="cerebrum-nb",role="consumer"} 1432`).
  A one-shot `export` reports its counters to the log sink.
- **`alarm suggest` cannot reach this connector.** It connects without
  LOGIN and then fails `no slot answered a walk`, because `Plugin.Walk`
  and `Plugin.GetSlotInfo` are deliberate "not applicable" stubs. Every
  neutral verb built on `Walk` is blind here; the connector's own verbs
  are not.
- The alarm template is filed `Cerebrum@unknown` because the server
  publishes no identity object.
- Redundancy, federation, SQL AlwaysOn replicas and per-service
  `Ext. Log Status` cannot be read on a standalone — their objects are
  `available=0` or empty here.

## Closing the connector — 2026-11-02

Same peer, Cerebrum 2.9.3 b15692. Destination 16 (virtual, no receiver).

| Case | Result |
|---|---|
| `route --level 1`, `DEST_LEVEL_ID` | level 1 moves, level 2 stays. The released `LEVEL_ID` moved both: **Cerebrum follows §4.1.1, the "every level moves" of 2026-09-26 was ours** |
| a route on a virtual | the levels with no route turn from empty to the marker 4294967294; not clearable over ROUTE |
| `device-config add` / `remove`, SNMP device | accepted inside `<ACTION>`; the RESULT has no MTID and comes before the ACK; the remove is in the LIST after more than 3 s; list identical after |
| `listen --syslog-addr`, 2 route changes | 2 records, none for the initial state, none for the marker moving |
| `listen`, stop | 5 SUBSCRIBE, 5 UNSUBSCRIBE |
| `listen --metrics-addr` | `/metrics` serves the counters |
| `get-route` | by id, by mnemonic, one level |
| `extract-all` | DMs written for SHUFFLE 6.0.0, VMI3608-2Q7 1.13.2; roots from their ACP2 models |
| every root spelling | refused on every device tried |
| `cerebrum-nb-verbs.yml` | failed=0, run twice, routes identical after |

## Nothing left out — 2026-11-02, same peer

| Case | Result |
|---|---|
| Cross-level `route --srce-level 1` on dest 16 level 2 | applied; second run changed 0; `get-route` reads `2 "CONVERT 2" from level 1 "Video"` |
| The same through `export`, `import` | `dest,srce,levels,srce_level` round trip, `import --check` then 0 |
| `route --use-tags` | applied |
| `listen`, `SRCE_LOCK` | granted once `LEVEL_ID` is sent, 6 subscribed, 0 failed; a lock and a release seen as events. "Every Cerebrum refuses it" was the missing attribute |
| `lock --kind SRCE_LOCK` | state read before the send: `--check`, second run nothing sent. `-srce-lock.csv` through `export` and `import` |
| `rm-add --total` | 16 present, adds 1, second run adds 0, new id read back; deleted after |
| `assoc` read-back | a real sender reads back in `RM_IP_UID`; a wrong name on a level with no device: ACK, exit 1 after 20 s |
| Session closed every 31 s | **ours since #989**: an EMPTY WebSocket Ping gets a Pong, then a TCP FIN, no Close frame. A Ping with a payload is answered and the session stays. No POLL needed |
| TCP keep-alive alone (no probe) | subscribed 260 s, 0 losses; replies to one session dropped: found dead after 153 s |
| `listen`, socket killed (`ss -K`) | reconnected after 1 s, subscriptions restored, the next route event received |
| BUSY | not reached: 10 parallel exports, then 1,000 OBTAIN pipelined on one session (96,000 rows answered), 0 BUSY. Unit-tested only |
| `lock` modes, RouteMaster | `locked`, `protected`: applied on destination and source. `locked_path`, `protected_path`: NACK 8 on both. §3.2: support depends on the target |
| `lock` modes, Neuron router | all four answered ACK and none applied: no event, the state stays RELEASED. `lock` now reads the state back and exits 1 |
| `rm-add --virtual`, `set-mnemonic`, `rm-delete` | pair created, renamed on both sides, deleted; confirmed by RCP reads |
| `assoc --kind SRCE_ASSOC`, virtual router | source on level 5 bound to device level 1, input 2; RCP reads `"Virtual Router"/1 io 2` |
| Device object label | `set-value` on `UserLabel` applied. An empty value is NACK 8, with or without `VALUE`: Northbound cannot clear a field |

## Northbound gap: the levels of the RouteMaster

A router reaches the RouteMaster through its level: Default Device and Default
Level on the Levels page. An association to a router needs that set first.
Northbound has no command for a level's properties; only its name (`LEVEL_MNE`).
RCP 2.5.3 on 2.9.3 answers `404` on `/routemaster/levels`; the 2.6.1 document
lists them as reads only. So a level, its device, Multiple Devices and IP Filter
are set in the user interface today. For EVS.
