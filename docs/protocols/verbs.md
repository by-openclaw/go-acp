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

| Protocol | Verbs | Notes |
|---|---|---|
| acp1 | `serve` (+ `admin`, `fuzz` — acp1-only) | `--tree --port --host`; `--announce-demo*` |
| acp2 | `serve` | `--tree --port`; `--announce-demo*` |
| emberplus | `serve` | `--tree --port` (+ `--mdns`, `--stream-ttl`, `--admin`) |
| probel-sw08p | `serve` | `--tree matrix.json --port 2008` |
| osc-v10 / osc-v11 | `send`, `fader`, `serve` | push model: emit / high-rate fader / bind+log |
| **tsl** | ❌ **not CLI-wired** | docs claim `send`/`serve`; not in `producer -h` / dispatch |
| **probel-sw02p** | ❌ **not CLI-wired** | provider pkg may exist; no producer command |
| cerebrum-nb | ❌ none | consumer-only by design |

## 7. Registry (NMOS only)
`dhs registry nmos serve` — dual-face (consumes IS-04 registrations + serves Query API).

---

## 8. Gaps this matrix surfaces (for "released + compliant")

1. **`ensure`** — implemented + verified on **acp1** (the idempotency primitive, ADR-0007); still missing on the other connectors.
2. **`status`, `replay` missing** — in ADR-0002 canonical list, not in CLI (`replay` deferred per ADR-0021).
3. **`set` validation exit code** — returns 1, should be 2 (error-codes.md); no client-side ValueValidator on acp1 (emberplus has it).
4. **`tree`** — acp1 now nests sub-group sections (DOWN CONV / TRANSPARENT / …) as parents (2026-06-12); other Tree/DM connectors still render shallow.
5. **tsl + probel-sw02p producers not CLI-wired** — provider code may exist but no `dhs producer` path.
6. **`-h` help stale** — `consumer -h` omits tsl + probel-sw02p; `producer -h` omits tsl. `list-protocols` is authoritative.
7. **ADR-0002 uniformity vs reality** — only Tree/DM implements the canonical set; Matrix/Push/Bridge diverge (see §1 open decision).

---

## 9. Per-verb — definition + worked example

One clear invocation per verb, from a protocol that implements it. Hosts/ports are
lab examples — swap for your device. Consumer form is
`dhs consumer <proto> <verb> <host> [flags]`; producer is
`dhs producer <proto> serve [flags]`. Flags beyond the example are listed by
`dhs consumer <proto> <verb> --help`.

### 9.1 Tree/DM (acp1 — identical shape for acp2 / emberplus)

| Verb | What it does | Example |
|---|---|---|
| `info` | device + per-slot card status (no walk) | `dhs consumer acp1 info 10.100.0.102 --port 2071 --transport tcp` |
| `walk` | enumerate every object on a slot | `dhs consumer acp1 walk 10.100.0.102 --port 2071 --transport tcp --slot 1` |
| `tree` | render the tree; sub-groups nest as parents | `dhs consumer acp1 tree 10.100.0.102 --port 2071 --transport tcp --slot 1 --path "control.DOWN CONV"` |
| `get` | read one value by label (walks on cache-miss) | `dhs consumer acp1 get 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --label Out-Mode` |
| `set` | write one value | `dhs consumer acp1 set 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --label Out-Mode --value Crossed` |
| `inc` | step up by the object's step (setIncValue) | `dhs consumer acp1 inc 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --id 9` |
| `dec` | step down by the object's step (setDecValue) | `dhs consumer acp1 dec 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --id 9` |
| `reset` | restore the object default (setDefValue) | `dhs consumer acp1 reset 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --id 9` |
| `watch` | live announcements (TCP/AN2 cross-VLAN; UDP bcast same-VLAN) | `dhs consumer acp1 watch 10.100.0.102 --port 2071 --transport tcp --slot 1` |
| `export` | snapshot a slot to a file (format by extension) | `dhs consumer acp1 export 10.100.0.102 --port 2071 --transport tcp --slot 1 --out slot1.json` |
| `import` | apply a snapshot (dry-run first with `--check`) | `dhs consumer acp1 import 10.100.0.102 --port 2071 --transport tcp --file slot1.json --check` |
| `extract` | capture a per-product DM triple into testdata layout | `dhs consumer acp1 extract 10.100.0.102 --port 2071 --transport tcp --slot 1` |
| `diff` | compare two canonical trees offline | `dhs consumer acp1 diff before.json after.json` |
| `convert` | translate a snapshot between formats offline | `dhs consumer acp1 convert slot1.json slot1.csv` |
| `discover` | scan the subnet for devices | `dhs consumer acp1 discover --port 2071` |
| `profile` | classify provider compliance (strict/partial) | `dhs consumer acp1 profile 10.100.0.102 --port 2071 --transport tcp` |
| `validate` | replay a capture offline; write the decoded snapshot | `dhs consumer acp1 validate frames.jsonl --out-tree tree.json` &nbsp;·&nbsp; `… --out-params params.csv` |
| `ensure` | idempotently converge one object (Ansible primitive) | `dhs consumer acp1 ensure 10.100.0.102 --port 2071 --transport tcp --slot 1 --group control --label Out-Mode --value Crossed --check` |
| producer `serve` | serve a frame AS the device | `dhs producer acp1 serve --manifest synapse-test.json --cache-dir .cache --host 0.0.0.0 --transport all --port 2071` |

acp2 adds **`diag`** (AN2 probe): `dhs consumer acp2 diag <host> --port 2072`.
emberplus adds **`matrix` / `invoke` / `stream` / `bench`** (see §2).

### 9.2 Matrix (probel-sw08p)

| Verb | What it does | Example |
|---|---|---|
| `interrogate` | read the source on one crosspoint | `dhs consumer probel-sw08p interrogate <host> --port 2008 --matrix 0 --level 0 --dst 5` |
| `connect` | route source → destination | `dhs consumer probel-sw08p connect <host> --port 2008 --matrix 0 --level 0 --dst 5 --src 12` |
| `tally-dump` | stream every crosspoint on a level | `dhs consumer probel-sw08p tally-dump <host> --port 2008 --matrix 0 --level 0` |
| `protect-connect` | owner-locked route | `dhs consumer probel-sw08p protect-connect <host> --port 2008 --matrix 0 --level 0 --dst 5 --src 12` |

### 9.3 Push / stream

| Verb | What it does | Example |
|---|---|---|
| osc `watch` | bind a port, print received OSC | `dhs consumer osc-v11 watch --listen udp:8000 --pattern "/ch/*/fader"` |
| tsl `listen` | bind a tally listener | `dhs consumer tsl-v50 listen --port 8900 --tcp` |
| osc producer `send` | emit one OSC message | `dhs producer osc-v11 send --target udp:9000 --address /ch/1/fader --args 0.75` |

### 9.4 Bridge (cerebrum-nb, consumer-only)

| Verb | What it does | Example |
|---|---|---|
| `connect` | POLL with login | `dhs consumer cerebrum-nb connect <host> --user admin --pass ***` |
| `route` | single ROUTE action | `dhs consumer cerebrum-nb route <host> --dest 10 --srce 4 --level 1` |
| `listen` | subscribe to routing/category/salvo events | `dhs consumer cerebrum-nb listen <host>` |

> **Idempotency (ADR-0025).** Every state-changing verb (`set`/`inc`/`dec`/`reset`/
> `connect`/`route`/`ensure`) must be drivable from an **Ansible** play and prove
> idempotency (run-twice = 0 changes). Integration validates these against the
> vendor emulator + real device — **never our own provider** (oracle-per-tier).
