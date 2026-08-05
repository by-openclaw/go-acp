# CLI Verb Reference — consumer + producer, per protocol

**Purpose:** the single, non-ambiguous list of every verb each connector exposes,
for **consumer** and **producer**, with description, scope, and live status.
This is the human cheatsheet; the **binding authority** is
[ADR-0002](../adr/0002-canonical-cli-verbs-flags.md) (canonical verbs + flags) and
[ADR-0007](../adr/0007-ensure-verb.md) (`ensure`). This doc adds what those lack
(per-verb descriptions, per-protocol scope, examples) — it does not restate the
decision (ADR-0015).

Status legend: ✅ verified live · ○ implemented, untested · ⚠️ implemented with a
gap · ❌ not implemented / not CLI-wired.
Source of truth for "what's registered": `dhs list-protocols` + `cmd/dhs/main.go`.

---

## 0. Roles and the encode/decode split (separation of concerns)

Three roles, each its own command tree:

| Role | Command | Direction | Meaning |
|---|---|---|---|
| **consumer** | `dhs consumer <proto> <verb> <target>` | outbound | connect to a device, query/control it |
| **producer** | `dhs producer <proto> serve` | inbound | serve a canonical tree AS a device |
| **registry** | `dhs registry <plugin> serve` | dual-face | NMOS only — consumes registrations + provides catalogue |

**Encoder/decoder is NOT a role — it's the shared `codec`.** Each protocol has one
stdlib-only `internal/<proto>/codec` (lift-ready, ADR-0006) that does **both**
encode and decode. The two roles call it in **mirror directions**:

| | encodes | decodes |
|---|---|---|
| **consumer** | requests (outbound) | replies + announces (inbound) |
| **producer** | replies + announces (outbound) | requests (inbound) |

**Separation of concerns (verified 2026-06-07):** `internal/<proto>/consumer` and
`internal/<proto>/provider` never import each other in production code (only
`*_test.go` loopback tests do, which is correct — the trusted consumer verifies the
provider). Both depend **down** on `codec/` + the neutral registries
(`consumer.Protocol` / `provider.Provider`), never sideways on each other.

---

## 1. Protocol models (why verb sets differ — this is the vocabulary key)

The 11 registered protocols are **not** one uniform verb set. They fall into four
models, and verbs only make sense within a model:

| Model | Protocols | Object | Verb shape |
|---|---|---|---|
| **Tree / DM** | acp1, acp2, emberplus | object tree (slots → objects / Glow nodes) | `walk`/`get`/`set`/`export`/… |
| **Matrix** | probel-sw08p, probel-sw02p (+ emberplus `matrix`) | crosspoints (matrix/level/dst/src) | `interrogate`/`connect`/`tally-dump`/`protect-*` |
| **Push / stream** | osc-v10/v11, tsl-v31/v40/v50 | one-way messages/tally (no tree) | `watch`/`listen` ; producer `send`/`serve` |
| **Bridge** | cerebrum-nb (consumer-only) | NB command/response (routing/category/salvo) | `connect`/`listen`/`route`/`list-*` |

> **Open decision (vocabulary):** ADR-0002 says every connector exposes the *same*
> canonical verb set (stub if inapplicable). Reality: only the Tree/DM model does.
> Matrix/Push/Bridge have their own catalogues and do **not** implement
> `walk`/`get`/`set`. Either (a) accept per-model verb sets and document them (this
> doc), or (b) enforce ADR-0002 uniformity. **Recommend (a)** — the models are
> fundamentally different; forcing `walk` onto a tally-push protocol is noise.

---

## 2. Consumer verbs — Tree/DM model (acp1, acp2, emberplus)

The canonical generic set (dispatched in `cmd/dhs/main.go`):

| Verb | Description | Scope | acp1 status |
|---|---|---|---|
| `info` | device info: slot count + per-slot card status | read-only, no walk | ✅ |
| `walk` | enumerate every object on a slot | full DFS of one slot | ✅ |
| `tree` | render object tree as ASCII or PlantUML mindmap | view only | ✅ nests sub-group sections (DOWN CONV/…) as parents |
| `get` | read one object value by slot/group/label (or path) | single object | ✅ walks on cache-miss to resolve `--label`/`--path` |
| `set` | write one object value | single object; validates type/range/enum | ✅ |
| `inc` | step an object up by its step (ACP1 setIncValue) | single RW numeric/enum | ✅ |
| `dec` | step an object down by its step (ACP1 setDecValue) | single RW numeric/enum | ✅ |
| `reset` | reset an object to its default (ACP1 setDefValue) | single object with setDef access | ✅ |
| `watch` | subscribe to live announcements | until Ctrl-C | ✅ UDP bcast same-VLAN; TCP/AN2 session-announce cross-VLAN |
| `export` | dump a walked slot/device → json / yaml / csv | snapshot out | ✅ |
| `import` | apply values from a snapshot (`--check` dry-run first) | RW objects only; mismatches skipped non-blocking | ○ |
| `extract` | capture a per-product DM triple (meta+wire+tree) | fixture-layout capture | ○ |
| `diff` | compare two canonical `tree.json` (text or CHANGELOG) | offline | ○ |
| `convert` | translate a snapshot json↔yaml↔csv | offline | ○ |
| `discover` | passive+active subnet scan for devices | same broadcast domain | ⚠️ native scan only; mDNS deferred to AMWA |
| `profile` | classify provider compliance strict / partial | read-only | ✅ STRICT |
| `validate` | replay a captured `frames.jsonl` offline; `--out-tree`/`--out-params` write the snapshot (ADR-0021) | offline | ✅ |
| `health` | 3-layer session health: reachable / connected / live | read-only | ○ |
| **`ensure`** | declarative converge: read→compare→set-if-diff (`--check` dry-run) | **idempotent — the Ansible primitive (ADR-0007)** | ✅ |
| `status` | session/service state | — | ❌ not in CLI |
| `replay` | re-emit a captured `frames.jsonl` on the wire | — | (deferred, ADR-0021) |

Per-protocol additions (Tree/DM): **emberplus** adds `matrix`, `invoke`, `stream`,
`bench`; **acp2** adds `diag` (AN2 diagnostic probes).

---

## 3. Consumer verbs — Matrix model

### probel-sw08p (SW-P-08 / SW-P-88)
Global flag: `--capture FILE.jsonl`. Coordinates: `--matrix --level --dst --src`.

| Verb | Description | Scope |
|---|---|---|
| `discover` | one-shot: dual-status + src/dst labels + tally-dump | survey |
| `interrogate` | query current source on one (matrix, level, dst) | read one crosspoint |
| `connect` | route a source to a destination | write one crosspoint |
| `watch` | subscribe to async tallies | until Ctrl-C |
| `maintenance` | maintenance message (reset / clear-protects) | control |
| `dual-status` | read 1:1 redundancy state | read |
| `tally-dump` | dump every crosspoint on (matrix, level) | bulk read (streaming) |
| `all/single-source-names`, `all/single-dest-names`, `all/single-source-assoc-names` | fetch labels | read |
| `protect-interrogate/connect/disconnect/name/dump`, `master-protect` | protect (write-lock) family | owner-only authority |
| `bench` | scale benchmark (interrogate-all + connect-all) | perf |

### probel-sw02p (SW-P-02)
Global flags: `--mtx-id --level --dsts --srcs` (`--dsts` enables bootstrap rx01 sweep + keep-alive).

| Verb | Description | Scope |
|---|---|---|
| `watch` | subscribe to async tallies (`--timeout`) | until Ctrl-C / timeout |

> sw02p consumer is **watch-only today**; interrogate/connect/protect are gated
> behind `approve seq N` (per-command queue). Most commands not yet wired.

---

## 4. Consumer verbs — Push/stream model

| Protocol | Verb | Description | Scope |
|---|---|---|---|
| osc-v10 / osc-v11 | `watch` | bind a port, print every received OSC message (`--listen <transport>:<port> [--pattern]`) | listener/monitor |
| tsl-v31 / v40 / v50 | `listen` | bind UDP (or v5.0 TCP `--tcp`) listener, print every decoded tally frame | one-way receiver |

---

## 5. Consumer verbs — Bridge model (cerebrum-nb, consumer-only)

| Verb | Description |
|---|---|
| `connect` | POLL (auto LOGIN with `--user/--pass`) |
| `listen` | SUBSCRIBE to routing / category / salvo / device events |
| `route` | ROUTE action — single (`--dest --srce --level`), batch (`--route d:s:l`), or `--csv` |
| `list-devices` | OBTAIN device list (`--device-type Router\|SNMP\|Device`) |
| `device-details` / `device-value` | per-device snapshots |
| `list-categories` / `category-details` | category browse |
| `list-salvo-groups` / `list-salvo-instances` / `salvo-instance-details` | salvo browse |
| `keepalive-probe` | diagnostic — hold WS, observe TCP keep-alives |

---

## 6. Producer verbs (inbound — serve as a device)

**Common lifecycle verbs** (generic in `dispatchProducer`, same behaviour for every
slot/matrix protocol below): `serve · tree · status · stop · ensure · validate`.

| Verb | Description |
|---|---|
| `serve` | bind the transport(s) and serve the canonical tree (`--pidfile` optional, for `stop`/`ensure`) |
| `tree` | print the canonical tree that would be served — no bind |
| `status` | live runtime snapshot of a serving instance (`--url .../snapshot.json`; needs `serve --metrics-addr`) |
| `stop` | signal a `serve --pidfile PATH` instance to shut down (graceful SIGTERM on Unix, kill on Windows) |
| `ensure` | ADR-0007 converge to `--state present\|absent`, keyed on `--pidfile`; `--check`/`--output json`; `absent` = idempotent teardown, `present`-apply-when-stopped errors (start is the supervisor's job, not a faked change) |
| `validate` | offline-decode a captured `frames.jsonl` through the codec |

| Protocol | Verbs | Notes |
|---|---|---|
| acp1 | common + `admin`, `fuzz` (acp1-only) | `serve --tree --port --host`; `--announce-demo*` |
| acp2 | common | `serve --tree --port`; `--announce-demo*` |
| emberplus | common | `serve --tree --port` (+ `--mdns`, `--stream-ttl`, `--admin`) |
| probel-sw08p | common | `serve --tree matrix.json --port 2008` |
| probel-sw02p | common | reaches the generic dispatch; `serve` needs the sw02p provider plugin |
| osc-v10 / osc-v11 | `send`, `fader`, `serve` | push model (own dispatch): emit / high-rate fader / bind+log |
| tsl-v31/v40/v50 | `serve`, `send` | push model (own dispatch, `runTSLProducer`) — not the generic lifecycle set |
| cerebrum-nb | ❌ none | consumer-only by design |

## 7. Registry (NMOS only)
`dhs registry nmos serve` — dual-face (consumes IS-04 registrations + serves Query API).

---

## 8. Gaps this matrix surfaces (for "released + compliant")

1. **`ensure`** — the idempotency primitive (ADR-0007), now with `--output` + `diff[]` (#628): scalar `ensure` on the Tree/DM connectors; matrix/crosspoint/label/protect converge on emberplus + probel-sw08p/sw02p (read-back-diff-apply); producer lifecycle `ensure` on the serving side (#656). TSL ratified **N/A** (push-only) per the ADR-0007 amendment.
2. **`status`** now wired (consumer #648 + producer lifecycle); **`replay` still missing** (deferred per ADR-0021).
3. **`set` validation exit code** — returns 1, should be 2 (error-codes.md); no client-side ValueValidator on acp1 (emberplus has it).
4. **`tree`** — acp1 now nests sub-group sections (DOWN CONV / TRANSPARENT / …) as parents (2026-06-12); other Tree/DM connectors still render shallow.
5. **producer wiring** — tsl producer is CLI-wired via its own dispatch (`runTSLProducer`); probel-sw02p reaches the generic lifecycle dispatch (`serve` gated on its provider plugin).
6. **`-h` help** — `producer -h` now lists the lifecycle VERBS; `consumer -h` still omits tsl + probel-sw02p. `list-protocols` is authoritative.
7. **ADR-0002 uniformity vs reality** — only Tree/DM implements the canonical set; Matrix/Push/Bridge diverge (see §1 open decision).

---

## 9. Per-verb — definition + worked example

Grounded in the actual CLI (`cmd/dhs/*.go`); hosts/ports are lab examples.
**Object addressing** (acp1/acp2/emberplus `get`/`set`/`inc`/`dec`/`reset`/`ensure`)
accepts EITHER the numeric **OID** (`--group <g> --id <n>`), the **name**
(`--group <g> --label <name>`), or a **path** (`--path <dotted-or-OID>`) — both
forms are shown. Three verbs are **top-level** (`dhs <verb>`, *not* under
`consumer`): `extract`, `diff`, `convert`. `validate` is **offline** (positional
capture file, no host).

### 9.1 acp1 / acp2 / emberplus — Tree/DM model

acp1 is **Tree/DM only — it has no matrix verbs.** acp2 adds `diag`; emberplus
adds `matrix`/`invoke`/`stream`/`bench` (emberplus-only — see §2).

| Verb | What it does | Example (name and/or OID) |
|---|---|---|
| `info` | device + per-slot card status (no walk) | `dhs consumer acp1 info 10.100.0.102 --port 2071 --transport tcp` |
| `walk` | enumerate every object on a slot | `dhs consumer acp1 walk 10.100.0.102 --port 2071 --transport tcp --slot 1` |
| `tree` | render the tree (sub-groups nest as parents) | `dhs consumer acp1 tree 10.100.0.102 --port 2071 --transport tcp --slot 1 --path "control.DOWN CONV"` (or `--oid 1.2.29`) |
| `get` | read one value | name → `… get 10.100.0.102 --slot 1 --group control --label Out-Mode` · OID → `… get 10.100.0.102 --slot 1 --group control --id 10` |
| `set` | write one value | name → `… set 10.100.0.102 --slot 1 --group control --label Out-Mode --value Crossed` · OID → `… set … --group control --id 10 --value Crossed` |
| `inc` / `dec` | step ±1 step (setInc/DecValue; acp1) | OID → `dhs consumer acp1 inc 10.100.0.102 --slot 1 --group control --id 9` · name → `… inc … --group control --label H_delay` |
| `reset` | restore default (setDefValue; acp1) | `dhs consumer acp1 reset 10.100.0.102 --slot 1 --group control --id 9` (or `--label H_delay`) |
| `watch` | live announcements | `dhs consumer acp1 watch 10.100.0.102 --port 2071 --transport tcp --slot 1` — **`--transport udp` sees announcements on the same VLAN only (subnet broadcast); across VLANs use `--transport tcp` or `an2` (announce is unicast over the session).** |
| `export` | snapshot → json/yaml/csv (by `--format` or `--out` ext) | `dhs consumer acp1 export 10.100.0.102 --port 2071 --transport tcp --slot 1 --format json --out slot1.json` |
| `import` | apply a snapshot (`--dry-run` first; subset by `--id`/`--path`, **not** label) | `dhs consumer acp1 import 10.100.0.102 --port 2071 --transport tcp --file slot1.json --dry-run` |
| `profile` | walk + classify compliance (strict/partial) | `dhs consumer acp1 profile 10.100.0.102 --port 2071 --transport tcp` |
| `ensure` | converge one object idempotently — the verb **Ansible drives**: `--check` dry-run, `--json` result, `changed` flag (change is never signalled by exit code) | `dhs consumer acp1 ensure 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --label Out-Mode --value Crossed --check --json` |
| `validate` | **offline**: decode a captured `frames.jsonl`; optionally write the snapshot | `dhs consumer acp1 validate capture.jsonl --out-tree tree.json` · `… --out-params params.csv` |
| `discover` | **acp1 only**: one-shot same-subnet LAN scan (no host arg) | `dhs consumer acp1 discover --duration 5s --active --scan-port 2071` |
| `extract` | **top-level**: capture a per-product DM triple (meta+wire+tree) | `dhs extract 10.100.0.102 --protocol acp1 --manufacturer Axon --product 2GS110 --direction consumer --version 2728 --out testdata/dm --slot 1` |
| `diff` | **top-level**: compare two canonical trees offline | `dhs diff before.json after.json --format changelog --version 2.4` |
| `convert` | **top-level**: translate a snapshot json↔yaml↔csv offline | `dhs convert --in slot1.json --out slot1.csv` |
| producer `serve` | serve a frame AS the device | `dhs producer acp1 serve --manifest synapse-test.json --cache-dir .cache --host 0.0.0.0 --transport all --port 2071` |

emberplus matrix → `dhs consumer emberplus matrix <host> --path mixers.main --target 3 --sources 7 --op connect`.
acp2 diag → `dhs consumer acp2 diag <host> --port 2072`.

### 9.2 probel-sw08p / probel-sw02p — Matrix model (no Tree/DM verbs)

| Verb | What it does | Example |
|---|---|---|
| `interrogate` | read the source on one crosspoint | `dhs consumer probel-sw08p interrogate <host> --port 2008 --matrix 0 --level 0 --dst 5` |
| `connect` | route source → destination | `dhs consumer probel-sw08p connect <host> --port 2008 --matrix 0 --level 0 --dst 5 --src 12` |
| `tally-dump` | stream every crosspoint on a level | `dhs consumer probel-sw08p tally-dump <host> --port 2008 --matrix 0 --level 0` |
| `protect-connect` | owner-locked route | `dhs consumer probel-sw08p protect-connect <host> --port 2008 --matrix 0 --level 0 --dst 5 --src 12` |

probel-sw02p is **watch-only** today (interrogate/connect/protect gated behind `approve seq N`): `dhs consumer probel-sw02p watch <host> --mtx-id 0 --level 0 --dsts 1-32`.

### 9.3 osc-v10/v11, tsl-v31/v40/v50 — Push/stream model

| Verb | What it does | Example |
|---|---|---|
| osc `watch` | bind a port, print received OSC | `dhs consumer osc-v11 watch --listen udp:8000 --pattern "/ch/*/fader"` |
| osc producer `send` | emit one OSC message | `dhs producer osc-v11 send --target udp:9000 --address /ch/1/fader --args 0.75` |
| tsl `listen` | bind a tally listener (v5.0 adds `--tcp`) | `dhs consumer tsl-v50 listen --port 8900 --tcp` |

### 9.4 cerebrum-nb — Bridge model (consumer-only, no producer)

| Verb | What it does | Example |
|---|---|---|
| `connect` | POLL with login | `dhs consumer cerebrum-nb connect <host> --user admin --pass ***` |
| `route` | single ROUTE action | `dhs consumer cerebrum-nb route <host> --dest 10 --srce 4 --level 1` |
| `listen` | subscribe to routing/category/salvo events | `dhs consumer cerebrum-nb listen <host>` |

> **Idempotency (ADR-0025).** Every state-changing verb (`set`/`inc`/`dec`/`reset`/
> `connect`/`route`/`ensure`) must be drivable from an **Ansible** play and prove
> idempotency (run-twice = 0 changes). Integration validates these against the
> vendor emulator + a real device — **never our own provider** (oracle-per-tier).

---

## 10. Per-connector transport + support matrix

**Anybody needs to know what's supported.** Legend: ✅ supported · ⚠️ partial · 🟡 deferred · ❌ not implemented · — n/a.

### 10.1 Transport support (consumer / provider)

| Connector | Default port | Consumer transports | Provider transports | Limit / note |
|---|---|---|---|---|
| acp1 | 2071 | UDP (Mode A) · TCP (Mode B) · AN2 (Mode C, 2072) · `auto` | UDP · TCP · AN2 (`--transport all`) | msg ≤141 B; **UDP announce = same-VLAN only**, cross-VLAN ⇒ TCP/AN2 |
| acp2 | 2072 | **AN2/TCP only** | AN2/TCP only | AN2 init (EnableProtocolEvents) required before traffic |
| emberplus | 9000 | **TCP only** (S101) | TCP (S101) | Lawo ports vary (9000/9090/9092); DTD 2.60 |
| probel-sw08p | 2008 | **TCP only** (DLE framing) | TCP | ACK 1 s, 5 retries; DATA hard cap 255 B |
| probel-sw02p | 2008 | TCP | ❌ not CLI-wired | consumer watch-only today |
| osc-v10/v11 | 8000 | UDP · TCP-LP (v10) · TCP-SLIP (v11) | UDP · TCP | push/stream; no Tree/DM |
| tsl-v31/v40/v50 | 8900 | UDP · TCP (`--tcp`, v5.0) | ❌ not CLI-wired | one-way tally receiver |
| cerebrum-nb | — | XML-over-WebSocket | — (consumer-only) | Bridge; no provider by design |

### 10.2 Supported / deferred (acp1 = gold-template reference)

| Connector | Consumer | Producer | Dissector | docs/ set | Coverage floors | Status |
|---|---|---|---|---|---|---|
| **acp1** | ✅ all verbs | ✅ all transports | ✅ | ✅ | ✅ codec 100 / consumer 90 / provider 90 | **DONE (gold template)** |
| acp2 | ⚠️ core unvalidated (AN2-only, no emulator) | ⚠️ | ✅ | ⚠️ no runbook | ❌ | partial |
| emberplus | ✅ + matrix/invoke/stream | ✅ | ✅ | ✅ | ❌ | near-done |
| probel-sw08p | ✅ matrix verbs | ✅ | ✅ | ❌ | ❌ | partial |
| probel-sw02p | ⚠️ watch-only | ❌ not wired | ✅ | ❌ | ❌ | early |
| osc-v10/v11 | ✅ watch | ✅ send/serve | ✅ | ❌ | ❌ | functional |
| tsl-v31/40/50 | ✅ listen | ❌ not wired | ✅ | ❌ | ❌ | consumer-only wired |
| cerebrum-nb | ✅ 12 NB verbs | — by design | n/a | ⚠️ | ❌ | consumer-only |

**Deferred** (scoped out, not gaps): acp1 mDNS discovery → AMWA cycle · acp1 live-AN2 + AN2 dissector → no hardware · `replay` verb → ADR-0021 · acp2 emulator → none exists (device-gated).

To bring any non-acp1 connector to ✅, follow the **"Bringing a connector to DONE" playbook** in the root `CLAUDE.md` — the same audit → fix → unit → integration → verbs → dissector → fixtures → docs → CI sequence (ADR-0025).
