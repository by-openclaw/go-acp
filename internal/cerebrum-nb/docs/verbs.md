# Cerebrum NB — verbs & configuration reference

Per-connector verb reference. Same section order as every other connector
(see [`../../probel-sw02p/docs/verbs.md`](../../probel-sw02p/docs/verbs.md))
so the docs don't drift.

**Wire samples in this doc are codec-generated fixtures, not live
captures.** There is no live Cerebrum peer (the NB northbound licence is
currently missing) and no emulator, so the codec is the only authority for
real 0v16 wire bytes. Every XML sample below is one of the committed
fixtures under [`../testdata/fixtures/`](../testdata/fixtures/README.md),
each produced by a codec encoder (TX) or by `codec.Decode` of the page-cited
0v16 worked example (RX) and pinned byte-for-byte by a drift-guard. **A live
capture is pending the NB licence** and will be diffed against these
fixtures when available.

Authoritative spec: **EVS Cerebrum Northbound API 0v16**
([`../assets/Cerebrum Northbound API 0v16.pdf`](../assets/Cerebrum%20Northbound%20API%200v16.pdf)).
0v13 is the historical baseline (a strict subset). Wire format + quirks live
in [`../CLAUDE.md`](../CLAUDE.md); the element/attribute/enum catalogue is
[`keys.md`](keys.md).

Cerebrum is a **northbound control API we CONSUME** — it is not a device we
serve, so there is **no provider role** (see [`provider.md`](provider.md))
and no get/set/inc/dec/reset Tree-DM shape. Consumer verbs: `connect
listen route lock unlock device-config set-mnemonic set-tags salvo category
set-value list-devices device-details device-value list-categories
category-details list-salvo-groups list-salvo-instances
salvo-instance-details obtain-datastore keepalive-probe`.

---

## 1. Transport configs

Cerebrum NB speaks **one transport** — XML over WebSocket (see
[`../CLAUDE.md`](../CLAUDE.md) "Wire layer"):

| Transport | Flag | Port | Notes |
|---|---|---:|---|
| WebSocket (plain) | (default) | 40007 | `ws://host:port`, no URL path. One XML document per WS text message, UTF-8. |
| WebSocket (TLS) | `--tls` | 40007 | `wss://host:port`; `--insecure-skip-verify` to skip cert validation. |

There is **no UDP** and **no `--transport` flag** — only WebSocket exists.
The target is `host:port`; port defaults to 40007 when omitted.

```
dhs consumer cerebrum-nb connect 10.6.239.50
dhs consumer cerebrum-nb connect 10.6.239.50:40008 --tls
```

LOGIN is the first frame on every authenticated session. Codec-generated
fixture ([`tx_login.xml`](../testdata/fixtures/tx_login.xml), from
`codec.EncodeLogin`):

```xml
<LOGIN USERNAME="admin" PASSWORD="s3cr3t" MTID="1"/>
```

A 0v16 LOGIN_REPLY carries the `<DATASTORES>` body listing the data stores
assigned to the API (§2.1 p7). Codec-generated fixture
([`rx_login_reply_datastores.xml`](../testdata/fixtures/rx_login_reply_datastores.xml)):

```xml
<login_reply api_ver="0.1" mtid="1"><datastores><datastore available="1" name="Global Data Files\Index.xml"/><datastore available="0" name="Data Files\Device.xml"/></datastores></login_reply>
```

(RX frames are lowercase — the decoder case-folds the AST; TX frames are
UPPERCASE — the wire-actual canonical case.)

## 2. Controllers & redundancy

**One licence per WebSocket session.** Cerebrum supports redundant primary
/ secondary servers; the `connect` verb sends a POLL whose reply reports
which server is active:

- `<poll_reply CONNECTED_SERVER_ACTIVE PRIMARY_SERVER_STATE
  SECONDARY_SERVER_STATE/>` — decoded into `codec.PollReply`.
- Run one consumer session per Cerebrum VIP for a redundant pair; the
  licence is consumed per session, so size the licence count to the number
  of concurrent dhs sessions.

There is no provider tier (consumer-only).

## 3. Logging & severity

**Logs go to stderr, data to stdout** (so `2>` captures logs, `1>` captures
the result). `--debug` enables verbose RX/TX XML logging.

| Flag | Values |
|---|---|
| `--debug` | off (default) / on — dumps every RX/TX XML document |
| `--timeout DUR` | per-request timeout (default 5s — fail fast) |

```
dhs consumer cerebrum-nb listen 10.6.239.50 --debug 2>run.log 1>events.out
```

## 4. info / walk (list-devices / details / value)

Cerebrum has no single "walk" primitive; discovery is per-domain via
`OBTAIN`:

| Verb | Wire | What it does |
|---|---|---|
| `list-devices` | `OBTAIN <device_change type='LIST'/>` | Every device, `--device-type Router\|SNMP\|Device` filter |
| `device-details` | `OBTAIN <device_change type='DETAILS'/>` | One device's vendor metadata |
| `device-value` | `OBTAIN <device_change type='VALUE'/>` | One device object value |

A 0v16 `DEVICE_CHANGE TYPE=VALUE` reply carries the full object descriptor
(§5.4.3 p29). Codec-generated fixture
([`rx_device_change_value.xml`](../testdata/fixtures/rx_device_change_value.xml)):

```xml
<device_change ip_address="10.1.1.1" object="Status.Connected" sub_device="0" type="VALUE"><object_value available="1" data_type="INTEGER" default="0" enum_list="On,Off" label="" object="Status.Connected" readable="1" units="" value="1" writable="0"/></device_change>
```

## 5. route / lock / set-value / obtain-datastore (the get/set analogues)

Cerebrum's "set" surface is the §4 `ACTION` envelope plus the §4.5
`DEVICE_CONFIGURATION` command and the §5.5 data store obtain.

| Verb | Wire | What it does |
|---|---|---|
| `route` | `ACTION <ROUTING TYPE='ROUTE'/>` | Apply a crosspoint route |
| `lock` / `unlock` | `ACTION <ROUTING LOCK='…'/>` | Lock / unlock (5-mode enum, §6) |
| `set-mnemonic` | `ACTION <ROUTING TYPE='*_MNE'/>` | Set a mnemonic |
| `set-tags` | `ACTION <ROUTING TYPE='RM_*_TAGS'/>` | Routemaster tags |
| `salvo` | `ACTION <SALVO TYPE='…'/>` | Run / save / rename / delete a salvo |
| `category` | `ACTION <CATEGORY TYPE='…'/>` | Create / modify / delete a category |
| `set-value` | `ACTION <DEVICE TYPE='SET_VALUE'/>` | Write a device object value |
| `device-config` | `<DEVICE_CONFIGURATION/>` (§7) | Device-tree CRUD |
| `obtain-datastore` | `OBTAIN <datastore_change path='…'/>` | Fetch a data store |

### route

```
dhs consumer cerebrum-nb route 10.6.239.50 --dest 60 --srce 60 --level 1
```

Codec-generated fixture ([`tx_action_route.xml`](../testdata/fixtures/tx_action_route.xml),
from `codec.EncodeAction(&RoutingAction{Type:"ROUTE"})`):

```xml
<ACTION MTID="2"><ROUTING TYPE="ROUTE" DEVICE_NAME="MTX1" DEVICE_TYPE="ROUTER" SRCE_ID="60" SRCE_LEVEL_ID="1" DEST_ID="60" DEST_LEVEL_ID="1"/></ACTION>
```

The async confirmation is a `ROUTING_CHANGE TYPE=ROUTE` (§5.1.1) carrying a
`<route>` child. Codec-generated fixture
([`rx_routing_change_route.xml`](../testdata/fixtures/rx_routing_change_route.xml)):

```xml
<routing_change dest_id="60" device_name="MTX1" device_type="ROUTER" level_id="1" type="ROUTE"><route available="1" source_id="60" source_level_id="1"/></routing_change>
```

### obtain-datastore — PATH vs NAME ambiguity

```
dhs consumer cerebrum-nb obtain-datastore 10.6.239.50 --name "Global Data Files\Index.xml"
```

Codec-generated fixture ([`tx_obtain_datastore.xml`](../testdata/fixtures/tx_obtain_datastore.xml),
from `codec.EncodeObtain(&DatastoreChange{Path:…})`):

```xml
<OBTAIN MTID="5"><DATASTORE_CHANGE PATH="Global Data Files\Index.xml" MTID="5"/></OBTAIN>
```

> **PATH vs NAME ambiguity (§5.5.1 p30).** The worked TX example uses
> `PATH=…`, but the attribute table immediately below names the field
> `NAME`. They cannot both be literal. The codec **emits `PATH`** (matching
> the only concrete example) and **accepts both on decode**.
> **Confirm against a live capture when an NB licence is available** and
> tighten if the server rejects `PATH`. See `codec/events.go`
> (`DatastoreChange`).

The reply carries a `<DATA>` body (§5.5.1). Codec-generated fixture
([`rx_datastore_change_data.xml`](../testdata/fixtures/rx_datastore_change_data.xml)):

```xml
<datastore_change available="1" type="FILE"><data><presets><preset_1 name="Active"/></presets></data></datastore_change>
```

## 6. lock — the 0v16 five-mode lock

0v16 §3.2 defines a **five-value** LOCK_STATE enum (p9):

| `--mode` | LOCK_STATE | Meaning |
|---|---|---|
| `unlocked` | `UNLOCKED` | no lock |
| `locked` | `LOCKED` | crosspoint locked |
| `protected` | `PROTECTED` | write-protected |
| `locked_path` | `LOCKED_PATH` | whole path locked |
| `protected_path` | `PROTECTED_PATH` | whole path protected |

```
dhs consumer cerebrum-nb lock   10.6.239.50 --kind DEST_LOCK --dest 60 --level 1 --mode locked
dhs consumer cerebrum-nb unlock 10.6.239.50 --kind DEST_LOCK --dest 60 --level 1
```

The lock detail is reported back in a `ROUTING_CHANGE` `<VALUE>` (SRCE_LOCK,
§5.1.2) or `<LOCK>` (DEST_LOCK, §5.1.3) child carrying `LOCK_STATE`. The
pre-0v13 `PROTECT` / `RELEASE` strings survive only as deprecated action
verbs, never as reported LOCK_STATE values (see `codec/actions.go`).

## 7. device-config — the 0v16 §4.5 device-tree CRUD

`device-config` is the headline 0v16 verb: add / modify / remove a device
in the Cerebrum device tree. `--device-type` selects the body shape
(`generic` / `panel` / `router` / `snmp`).

```
dhs consumer cerebrum-nb device-config add 10.6.239.50 \
  --device-type generic --ip 10.10.10.1 \
  --device "Shotoku STAR Protocol" --version Latest --name "Shotoku STAR Protocol" \
  --connection-type TCP --port-number 8000 --timeout-ms 5000 --poll-period 3000
```

Codec-generated fixture
([`tx_device_config_add_generic.xml`](../testdata/fixtures/tx_device_config_add_generic.xml),
from `codec.EncodeDeviceConfiguration(7, GENERIC ADD)`, §4.5.1.1 p20):

```xml
<DEVICE_CONFIGURATION TYPE="ADD" IP_ADDRESS="10.10.10.1" DEVICE_TYPE="GENERIC" MTID="7"><CONFIGURATION DEVICE="Shotoku STAR Protocol" VERSION="Latest" NAME="Shotoku STAR Protocol"><PROTOCOL_CONFIGURATION CONNECTION_TYPE="TCP" PORT_NUMBER="8000" TIMEOUT="5000" POLL_PERIOD="3000"/></CONFIGURATION></DEVICE_CONFIGURATION>
```

The server replies with the same envelope wrapping a `<RESULT
VALUE="ACCEPTED|FAILED"/>` (§4.5.1.1 p20). Codec-generated fixture
([`rx_device_config_result_accepted.xml`](../testdata/fixtures/rx_device_config_result_accepted.xml)):

```xml
<device_configuration device_type="GENERIC" ip_address="10.10.10.1" type="ADD"><result value="ACCEPTED"/></device_configuration>
```

`REMOVE` carries no body (self-closing `<DEVICE_CONFIGURATION …/>`, §4.5.2
p23). `PANEL` / `ROUTER` / `SNMP` bodies are documented in `codec/actions.go`
and pinned in `codec/codec_0v16_test.go`.

## 8. listen / watch (async events)

`listen` subscribes to routing / category / salvo / device events and prints
one line per dispatched frame until Ctrl-C:

```
dhs consumer cerebrum-nb listen 10.6.239.50 --user admin --pass s3cr3t
```

A snapshot subscription ends with the 0v16 `<WILDCARD_COMPLETE>` sentinel
(§1.6 p6). Codec-generated fixture
([`rx_wildcard_complete.xml`](../testdata/fixtures/rx_wildcard_complete.xml)):

```xml
<wildcard_complete mtid="1"/>
```

Enriched event examples (all codec-generated fixtures):

- Salvo instance details (§5.3.3 p28,
  [`rx_salvo_change_instance_details.xml`](../testdata/fixtures/rx_salvo_change_instance_details.xml)):

  ```xml
  <salvo_change group="Multiviewer 1" instance="Line up" type="INSTANCE_DETAILS"><details active="1" available="1" date="02/07/2017" description="The layout for line up" time="17:33:22:12"/></salvo_change>
  ```

- Category details with an item GROUP (§5.2.2 p28,
  [`rx_category_change_details.xml`](../testdata/fixtures/rx_category_change_details.xml)):

  ```xml
  <category_change category="EVS" type="CATEGORY_DETAILS"><details available="1" description="All EVS Sources" label="EVS Sources"/><items><item_1 group="Multiviewer 1" type="SALVO" value="Line up"/></items></category_change>
  ```

The category create TX is a fixture too
([`tx_action_category_create.xml`](../testdata/fixtures/tx_action_category_create.xml)) —
`<ACTION MTID="4"><CATEGORY TYPE="CREATE" …/></ACTION>`; likewise salvo run
([`tx_action_salvo_run.xml`](../testdata/fixtures/tx_action_salvo_run.xml)).

## 9. flow control — CONTINUE after BUSY

When the server is busy it returns `<BUSY/>`; the client waits for the 0v16
`<CONTINUE/>` flow-control frame before resending (§1.4 p5). Codec-generated
fixture ([`rx_continue.xml`](../testdata/fixtures/rx_continue.xml)):

```xml
<continue/>
```

The codec surfaces these as `KindBusy` / `KindContinue` frames; the consumer
gates resends on `CONTINUE`.

## 10. Wireshark

Dissector:
[`../wireshark/dhs_cerebrum_nb.lua`](../wireshark/dhs_cerebrum_nb.lua) —
decodes the WebSocket handshake + RFC 6455 framing + every Cerebrum NB XML
root, including the 0v16 additions (`DEVICE_CONFIGURATION` + `RESULT`,
`CONTINUE`, `WILDCARD_COMPLETE`, the `DATASTORES` body). The Info column
surfaces root + `MTID` + `TYPE` + `DEVICE_TYPE` + `RESULT VALUE` +
`ERROR`/`ERROR_CODE`; an unknown root fires a Wireshark expert WARNING (not
a silent fallthrough).

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
# copy the dissector, then filter on the dhs proto:
#   display filter:  dhs_cerebrum_nb
#   port decode:     tcp.port == 40007
tshark -r capture.pcapng -O dhs_cerebrum_nb -Y dhs_cerebrum_nb
```

## 11. Ansible (the exclusive integration driver — no .ps1)

[`../../../ansible/playbooks/cerebrum-nb-integration.yml`](../../../ansible/playbooks/cerebrum-nb-integration.yml)
is a **verb-driven external-oracle** play, mirroring acp1's pattern (NOT
acp2's loopback — Cerebrum has **no provider / no emulator**, so there is no
loopback tier). It runs **read-only** verbs (`connect`, `list-devices`,
`device-details`) against a **live, licensed Cerebrum** and asserts on
stdout.

It is **gated** on `CEREBRUM_TEST_HOST` (+ `DHS_CEREBRUM_USER` /
`DHS_CEREBRUM_PASS`, optional `CEREBRUM_TEST_PORT` default 40007) and
`meta: end_play` skips cleanly when unset — so it is safe to invoke
unconditionally.

> The NB licence is now present (2026-06) and the read-side verbs were
> live-verified against a production Cerebrum (2026-08). The play still
> gates on the env vars and skips cleanly when unset.

```
CEREBRUM_TEST_HOST=10.6.239.50 DHS_CEREBRUM_USER=admin DHS_CEREBRUM_PASS=s3cr3t \
  ansible-playbook -i inventory/hosts.ini playbooks/cerebrum-nb-integration.yml
```

Read-only verbs → `changed_when: false` → idempotent by construction
(run-twice = 0 changes). dhs logs go to stderr → tasks `register` and
`debug` the combined `stdout_lines + stderr_lines`.

## 12. Inventory + snapshot — list-* / export / import (ensure)

The probel-sw08p-parity operator surface: enumerate the Route-Master,
snapshot it to CSVs, and converge live state back from (possibly edited)
CSVs. All reads are **one-shot OBTAIN** (§2.4) — never SUBSCRIBE; the read
ends on the MTID-carrying `WILDCARD_COMPLETE` (§1.6).

### list-sources / list-dests (alias `list-destinations`) / list-levels

```
dhs consumer cerebrum-nb list-sources 10.6.239.50 --user U --pass P [--id N] [--out FILE]
```

One row per resource: `ID · LEVELS · LABEL · ALTS`. **Capability levels**
come from the `ASSOCIATION_n` indices (index = Routemaster level — live-wire
fact, not in the 0v16 PDF); a resource with no ASSOCIATIONS block is
unbound and shows no levels. Alternate labels print as `1=Black 4=ENG`
(slot index, §4.1.5 `ALT_MNE=n`); the slot→set-name mapping is Cerebrum
config and not exposed over NB. `--id N` narrows to one resource, `--out`
writes CSV instead of the table.

### export — full snapshot

```
# crosspoints only (optionally one level):
dhs consumer cerebrum-nb export HOST --user U --pass P --out xp.csv [--level N]
# full Route-Master snapshot:
dhs consumer cerebrum-nb export HOST --user U --pass P --out-dir DIR --prefix noc
#   → noc-src.csv  noc-dst.csv  noc-level.csv  noc-xpoint.csv
```

- Mnemonic CSVs: `<kind>_id,levels,mnemonic,alt_1..alt_N` — alt columns are
  **uniform plant-wide** (sized to the highest used slot across the whole
  snapshot), `levels` is `;`-separated capability levels.
- Xpoint CSV: `dest,srce,level` — one row per **routed** cell. The server
  answers every dest × level cell; `SOURCE_ID` `0` / `4294967294` /
  `4294967295` are undocumented **no-route sentinels** and are filtered.
- Cross-level routes (src level ≠ dst level) are skipped with a warning —
  shuffle representation is not yet supported (parked).

### import — ENSURE (ADR-0007)

```
# mirror of export --out-dir — reads the same file set back:
dhs consumer cerebrum-nb import HOST --user U --pass P --in-dir DIR --prefix noc [--check] [--allow-clear]
# or per-file (partial scope):
dhs consumer cerebrum-nb import HOST --user U --pass P \
  [--xpoint xp.csv] [--src s.csv] [--dst d.csv] [--levels l.csv] [--check] [--allow-clear]
```

`--in-dir` resolves `<prefix>-xpoint.csv / -src.csv / -dst.csv /
-level.csv`; files absent from the directory are simply out of scope, and
an explicit per-file flag overrides its `--in-dir` counterpart.

Diff-first: reads live state over the same OBTAINs, diffs against the
CSVs, sends **only the differences** (`ROUTE` actions / `*_MNE` writes via
`ALT_MNE=n`). Contract:

- `--check` — online dry-run: prints `[would-*] …` lines + `would_change=N`,
  sends nothing (a host is still required — ensure reads live state).
- Partial CSV = partial scope: rows/files absent are never touched.
- Empty label cell = untouched, **unless** `--allow-clear` AND the column is
  managed (present in the CSV header; primary always managed) AND the live
  value is set → clear-write (`MNEMONIC=""`). ⚠ the clear wire-form is
  **live-unverified** — staging only.
- Route import never disconnects — an unrouted live cell only changes if
  the CSV routes it.
- Run-twice = 0 changes (idempotent by construction).
- `--output json` — the canonical ADR-0007 shape on stdout
  (`{changed|would_change, diff[]}`, one `diff[]` entry per cell:
  `route.<dest>.<level>` / `<kind>.<id>.<slot>`); per-change narration
  moves to stderr so Ansible can parse stdout.

## 12b. Tree/DM domain — get / watch / extract / validate (D2, #700)

Devices behind Cerebrum ride the **Tree/DM template** (the acp2/ember+
model), while the RM/routers ride the Matrix template above. One dotted
path grammar everywhere — `DEVICE.SUB.OBJECT…`, DEVICE_NAME taken
verbatim (incl. the live trailing-whitespace quirk):

```
# canonical read of one object (§5.4.3 VALUE obtain):
dhs consumer cerebrum-nb get HOST --user U --pass P \
  --path "bm-n-nncvt-001 .1.PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay"

# canonical subscribe (exact leaf — wildcards refused, live-verified):
dhs consumer cerebrum-nb watch HOST --user U --pass P \
  --device "bm-n-nncvt-001 " --by-name --sub-device 1 \
  --object "PROCESSING AUDIO.AUDIO DELAY.BANK 1.Delay"
```

### extract — ADR-0022 card data model

Walks one device's object tree (same walk contract as `tree --device`:
seeded start groups, recursion, self-echo leaf re-classification) and
persists the DM + manifest pair:

```
dhs consumer cerebrum-nb extract HOST --user U --pass P \
  --device "bm-n-nncvt-001 " --by-name --sub-device 1 \
  --path "PROCESSING AUDIO;INPUT;OUTPUT" --version 6.7.4
#   → .cache/dm/cerebrum-nb/<Model@SwRev>.json   (flat canonical Objects)
#   → .cache/manifest/<device-slug>.json         (device → sub-device → DM ref)
```

- **Model** auto-probes from the device DETAILS vendor type; `--product`
  overrides. **`--version` is required** — the NB wire exposes no
  firmware/software version anywhere.
- A walk that hits `--max-requests` **fails** rather than persisting a
  truncated DM (a partial model is not a device model).
- The printed `sha256:` fingerprint is the evidence anchor; the DM +
  manifest are committed under `testdata/integration-test/` like every
  other connector's, so the acp2 extract of the same CONVERT is
  diffable against the cerebrum one (dual-oracle, S9).

### validate — offline decoder oracle

```
dhs consumer cerebrum-nb validate frames.jsonl --out-tree tree.json [--out-params p.csv]
```

Replays a `--capture` JSONL through the codec offline: per-document
counts, NACKs, case deviations; `--out-tree` aggregates the observed
§5.4.3 VALUE rows into the same canonical tree shape `extract` writes —
capture-derived and live-extracted views of one device stay
byte-comparable.

## 13. See also

- [`../CLAUDE.md`](../CLAUDE.md) — wire format, mtid, quirks, "what NOT to do"
- [`README.md`](README.md) · [`consumer.md`](consumer.md) · [`runbook.md`](runbook.md) · [`provider.md`](provider.md) · [`keys.md`](keys.md)
- [`../testdata/fixtures/README.md`](../testdata/fixtures/README.md) — how every wire sample above was codec-generated
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
