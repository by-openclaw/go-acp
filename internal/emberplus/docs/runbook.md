# Ember+ — operational runbook

> Status: **PR δ in progress** — draft under codeowner review.
> Source-of-truth for verb-by-verb validation against the
> integration-test fixture set. Capture batch (PR γ) lands separately.

## Scope

| Dimension | Coverage |
|---|---|
| **Protocol** | Ember+ DTD `2.60` strict — older DTDs not in scope (per `internal/emberplus/CLAUDE.md`) |
| **Roles** | dhs as **consumer** (outbound) and **provider** (inbound); both exercised against the integration-test fixture set |
| **Producer source** | `internal/emberplus/testdata/integration-test/` — manifest + 7 strict DMs (identity, oneToN, oneToOne, nToN, dynamic, functions, glow-types). Regen script: `scripts/emberplus/gen-emberplus-demo-dms.ps1` |
| **OS** | Windows 11 (primary host); Linux LXCs (Debian/Ubuntu/Rocky) parity |
| **Out of scope** | mDNS discovery (R18 #477); `health` verb (#300); export/import full round-trip (#461); `validate --lua` (R12 #473) — listed in [Pending R-items](#pending-r-items) |

Merged + live on `main`:

| PR | Title |
|---|---|
| #463 | refactor(emberplus): testdata fixtures moved into `internal/emberplus/testdata/integration-test/` |
| #480 (replaces #464) | feat(emberplus): scope refresh — DTD 2.60, 16×16 matrices, dynamic 128×128 sparse 16, stream id=0 |
| #466 | fix(manifest): BuildExport reroots strict-canonical slot DM paths under synthetic root |
| #467 | fix(emberplus/codec/matrix): oneToOne source-steal accepted + compliance event |
| #479 | feat(emberplus/consumer): stream --id accepts CSV for multi-subscribe |
| #459 | fix(emberplus/consumer): GetSlotInfo IsOnline correct |
| #457 | fix(emberplus/provider): builtins return error on unresolved matrix |
| #453 | fix(emberplus/consumer): set --value typed validation, no silent coerce |

## Setup — build + serve

```powershell
git switch main
git pull --ff-only origin main
go build -o bin\dhs.exe ./cmd/dhs

.\bin\dhs.exe producer emberplus serve `
    --manifest internal\emberplus\testdata\integration-test\manifest\emberplus-integration.json `
    --cache-dir internal\emberplus\testdata\integration-test `
    --port 9100 `
    --log-level info
```

Producer listens on `[::]:9100`. Expected log:

```
producer manifest loaded device=dhs-emberplus-integration endpoints=1 frames=1
listening addr=[::]:9100 tree_size=1361
streamer started stream_count=3 interval=500ms
```

> Tree size approximate (±10 across revisions). Track PID to
> `.audit/producer.pid` for clean shutdown (see [Cleanup](#cleanup)).

## Tree shape

```
dhs-emberplus-integration (synthetic root, oid "1")
├── identity        (1.0)  — product / company / version / dtdVersion=2.60
├── oneToN          (1.1)  — 16×16, Pri+Sec labels, target 2 PRE-LOCKED
├── oneToOne        (1.2)  — 16×16, Pri+Sec labels, source-steal on SET
├── nToN            (1.3)  — 16×16, Pri+Sec, maxConnectsPerTarget=4, multi-src seed
├── dynamic         (1.4)  — 128×128 declared, 16 sparse with gaps, target 99 LOCKED
├── functions       (1.5)  — setLock / listLocks / storeSalvo / recallSalvo / listSalvos / getSalvo
└── types           (1.6)  — every ParameterType + 3 streams (id=0, id=1001, id=1002)
```

Every matrix carries `sourceParams[N].gain`, `targetParams[N].gain`, and (nToN only) per-XPT gain via `parametersLocation`.

## Verb catalogue

| Verb | Wire shape | Returns |
|---|---|---|
| `info` | GetDirectory(root) + identity walk | per-slot online + status |
| `walk` | GetDirectory recursive | every object in the tree |
| `get` | GetDirectory(path) | one value |
| `set` | SetValue | confirmed value (or ValidationError) |
| `watch` | Subscribe(30) on streams + implicit on params/matrices | live stream of value/connection changes |
| `matrix` | Connect/Disconnect/Absolute on a Matrix | applied connection state |
| `invoke` | Invoke + InvocationResult | tuple result + Success=true/false |
| `stream` | Explicit subscribe to streamIdentifier | stream feed |
| `profile` | Walk + count compliance.Profile events | classification (strict / partial) |
| `export` | Walk + write JSON/YAML/CSV | file at --out |
| `import` | Read JSON/YAML/CSV + apply | tree state on remote |
| `extract` | Walk + write per-product DM (meta+wire+tree) | files in fixture layout |
| `diff` | Two tree.json compared | text diff or CHANGELOG section |
| `convert` | Translate between JSON / YAML / CSV | offline file write |
| `validate` | Decode captured frames.jsonl through codec | offline decoder check |
| `bench` | Fire N matrix ops over 1 TCP session | latency + ops/s |
| `health` | 3-layer session liveness | not implemented for Ember+ (#300) |
| `discover` | mDNS scan | no mDNS for Ember+ today |

---

## 1. `info` — device summary

### Happy

```powershell
.\bin\dhs.exe consumer emberplus info 127.0.0.1 --port 9100
```

Expected:

```
device       127.0.0.1:9100
protocol     emberplus v1
slots        1

per-slot status:
  slot  0   status=present    online=true        ← post #459
```

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| missing host | `dhs consumer emberplus info --port 9100` | `error: usage: dhs consumer <proto> info <host> [...]` | 2 |
| connection refused | `dhs consumer emberplus info 127.0.0.1 --port 9999 --timeout 2s` | `s101 framing error: connect 127.0.0.1:9999: ... actively refused it` | 1 |
| unreachable host | `dhs consumer emberplus info 192.0.2.1 --port 9100 --timeout 2s` | `s101 framing error: ... i/o timeout` | 1 |

---

## 2. `walk` — full tree enumeration

### Happy

```powershell
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9100 --capture .audit\walks\demo.jsonl
```

Expected: 548 lines of tree dump, `tree_size=1361` objects, DM auto-extracted to `.cache/dm/emberplus/dhs-emberplus-integration@1.0.0.json` for subsequent `--dm` hot-load.

### Errors

| Trigger | Command | Expected |
|---|---|---|
| connection refused | `walk ... --port 9999` | s101 framing error |
| timeout too small | `walk ... --timeout 1ms` | walk for "walk": context deadline exceeded |

---

## 3. `get` — read one Parameter

### Happy

```powershell
# Each ParameterType
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vInteger
# value = 42

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vReal
# value = 3.14    (carries factor=100, format=%.2f, unit=dB)

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vString
# value = "hello"

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vBoolean
# value = true

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vEnum
# value = 1    (enumMap: Off=0, Low=1, Medium=2, High=3)

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vOctets
# value = "aGVsbG8="    (known broken in Cerebrum + EmberPlusView UIs, wire OK)

# Stream parameters (id=0 + id>0)
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vu_zero
# value = -60.00    (streamIdentifier=0 — must NOT be treated as absent)

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vu_left
# value ≈ -60.00 → updates live

# Identity
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.identity.dtdVersion
# value = "2.60"
```

### Errors

| Trigger | Command | Expected |
|---|---|---|
| wrong path | `get ... --path bogus.path.here` | `object not found (tree has 1361 entries)` |
| missing required path | `get 127.0.0.1 --port 9100` | usage error |

---

## 4. `set` — write one Parameter

### Happy

```powershell
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.gain --value -25
# confirmed value = -25

.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.mute --value true
# confirmed value = true

.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vEnum --value 3
# confirmed value = 3
```

### Errors (post #453)

| Trigger | Command | Expected |
|---|---|---|
| unparseable int (#445) | `set ... gain --value -25.5` | `validation: value: invalid integer "-25.5"`, exit 2 |
| unparseable int (letters) | `set ... gain --value abc` | `validation: value: invalid integer "abc"`, exit 2 |
| unparseable real | `set ... vReal --value not-a-float` | `validation: value: invalid real "not-a-float"`, exit 2 |
| enum overflow | `set ... vEnum --value 999` | `validation: value: invalid enum index "999"`, exit 2 |
| write to read-only stream | `set ... vu_zero --value -10` | protocol error (provider rejects) |
| wrong path | `set --path bogus --value 1` | object not found |

---

## 5. `watch` — live stream

### Happy

```powershell
# Watch all stream Parameters (3 streams: id=0, 1001, 1002)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 --streams-only
# Updates every ~500ms

# Watch one specific path
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vu_zero --streams-only
# Only vu_zero updates

# Watch the locked-target tally on oneToN
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix
```

### Errors

| Trigger | Command | Expected |
|---|---|---|
| connection refused | `watch ... --port 9999` | s101 framing error |
| bad path filter | `watch --path bogus` | no events (filter matches nothing) |

---

## 6. `matrix` — Connect / Disconnect / Absolute

### Happy

```powershell
# nToN — multi-source SET
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --target 10 --sources 3,4,5 --op absolute
# matrix connect: target 10 ← sources [3 4 5] (op=absolute)

# nToN — disconnect one source
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --target 10 --sources 4 --op disconnect
# matrix connect: target 10 ← sources [4] (op=disconnect)

# oneToN — replace single source
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix `
    --target 0 --sources 7 --op absolute
# (target 0 now routed from src 7; prior src 0 dropped)

# oneToOne — source steal (post #467)
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToOne.matrix `
    --target 0 --sources 5 --op absolute
# matrix connect: target 0 ← sources [5] (op=absolute)
# Source 5 was on target 5; now stolen to target 0 — target 5 implicitly disconnected.
# Profile counter: onetoone_source_steal_accepted += 1
```

### Errors

| Trigger | Command | Expected |
|---|---|---|
| oneToN over-cardinality | `matrix oneToN --target 0 --sources 1,2 --op absolute` | `matrix validation: oneToN matrix: target 0 would have 2 sources (max 1) [spec p.33]`, exit 1 |
| nToN over capacity | `matrix nToN --target 0 --sources 0,1,2,3,4,5 --op absolute` | `matrix validation: nToN matrix: target 0 would have 5 sources (max 4 per target)`, exit 1 |
| locked target | `matrix oneToN --target 2 --sources 5 --op absolute` | rejected by provider; tally announce echoes back unchanged with `disposition: locked` |
| wrong matrix path | `matrix --path bogus` | matrix not found at path "bogus" |

---

## 7. `invoke` — function RPC

### Happy

```powershell
# Lock target 3 on oneToN (matrix-OID or dotted path both work)
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "1.1.3,3,true"
# invocation 1: success=true
# result: [false]       (previous lock state — was unlocked)

# Same via dotted matrixRef (post #466)
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "dhs-emberplus-integration.oneToN.matrix,4,true"
# invocation 1: success=true
# result: [false]

# List locked targets
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.listLocks `
    --args "1.1.3"
# result: [2,3,4]   (2 was pre-locked from seed)

# Store salvo — all current connections on oneToN
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.storeSalvo `
    --args "1.1.3,99,"
# result: [true]

# Get salvo dump
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.getSalvo `
    --args "1.1.3,99"
# result: ["0=0;1=1;3=3;..."]   (tgt=src list semicolon-separated)

# Recall salvo
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.recallSalvo `
    --args "1.1.3,99"
# result: [N]    (rows restored)
```

### Errors (post #455 / #457)

| Trigger | Command | Expected |
|---|---|---|
| bogus matrixRef | `invoke setLock --args "bogus.path,1,true"` | `invocation 1: success=false` (was silent success pre-#457) |
| missing arg | `invoke setLock --args "1.1.3"` | `setLock: need (matrixPath, target, locked)` |
| wrong type arg | `invoke setLock --args "1.1.3,abc,true"` | bad arg types |

---

## 8. `stream` — explicit subscribe

### Happy

```powershell
.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0
# Subscribes to streamIdentifier=0 only (vu_zero updates flow)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 1001
# Subscribes to vu_left only

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001
# Subscribes to {vu_zero, vu_left} — R10 multi-subscribe

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001,1002
# Subscribes to all three explicitly (equivalent to no flag)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100
# Subscribes to ALL streams (3 today)
```

### Errors

| Case | Command | Error |
|---|---|---|
| bad token | `stream ... --id abc` | `validation: --id: bad token "abc"` |
| empty token mid-csv | `stream ... --id 0,,1001` | `validation: --id: empty token in "0,,1001"` |
| trailing comma | `stream ... --id 0,1001,` | `validation: --id: empty token in "0,1001,"` |

---

## 9. `profile` — compliance classification

### Happy

```powershell
.\bin\dhs.exe consumer emberplus profile 127.0.0.1 --port 9100
# host             127.0.0.1:9100
# objects walked   1355
# classification   strict
# no tolerance events observed — provider is fully spec-compliant
```

After triggering source-steal (post #467), the counter `onetoone_source_steal_accepted` appears with its count.

---

## 10. `export` / `import` — round-trip (PARTIAL — #461)

```powershell
.\bin\dhs.exe consumer emberplus export 127.0.0.1 --port 9100 --format yaml --out demo.yaml
# Writes demo.yaml

.\bin\dhs.exe consumer emberplus import demo.yaml --port 9100
# (partial — Glow type coverage not complete; see #461)
```

---

## 11. `extract` — capture per-product DM triple

```powershell
.\bin\dhs.exe consumer emberplus extract 127.0.0.1 --port 9100 `
    --manufacturer BY-Systems --product dhs-emberplus-integration `
    --direction in --version 1.0.0 --out .audit/extract-demo
# Writes meta.json + wire.pcapng + tree.json under the fixture layout
```

---

## 12. `validate` — offline frame decode (ADR-0021)

```powershell
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl
# Decodes each frame through the codec; emits PASS / FAIL per frame
```

---

## 13. `bench` — matrix latency

```powershell
.\bin\dhs.exe consumer emberplus bench 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --n 1000 --op connect --targets 16 --sources 16
# total: 1000 ops in N ms, M ops/s
```

Flags: `--path` (matrix path), `--n` (op count, default 100), `--op` (connect / absolute / disconnect, default connect), `--targets` / `--sources` (wrap modulo, default 4). No percentiles today — RFC 2544/8219 profile + cold-start + recovery pending **R13 #474**.

---

## Use-case status — Ember+ (Consumer + Provider)

Current state on `main`. ✅ working, 🟡 partial, ❌ not implemented.

| Seq | Use case | Consumer | Provider | Notes / refs |
|---|---|---|---|---|
| UC-1 | `info` — device summary | ✅ | ✅ | Online correct post #459 |
| UC-2 | `walk` — full enumeration | ✅ | ✅ | tree_size ≈ 1361; DM auto-extracted to `.cache/dm/emberplus/<identity>@<rev>.json` |
| UC-3 | `get` — single Parameter | ✅ | ✅ | every ParameterType + stream id=0 + id>0 + identity.dtdVersion |
| UC-4 | `set` — single Parameter | ✅ | ✅ | typed-validation errors post #453; out-of-range / range-step rounding pending **R16** |
| UC-5 | `watch` — live updates | ✅ | ✅ | streams + matrix tally + param announces |
| UC-6 | `matrix` — Connect / Disconnect / Absolute | ✅ | ✅ | oneToOne source-steal accepted post #467 (fires `onetoone_source_steal_accepted`); oneToN over-cardinality + nToN capacity rejected client-side; lock honored |
| UC-7 | `invoke` — function RPC | ✅ | ✅ | bogus matrixRef now errors post #457; dotted matrixRef resolves under synthetic root post #466 |
| UC-8 | `stream` — explicit subscribe | ✅ R10 CSV | ✅ | `--id 0,1001` multi-subscribe post #479 |
| UC-9 | `profile` — compliance counter | ✅ | n/a | reads provider profile; classification = strict by default |
| UC-10 | `export` → `import` round-trip | 🟡 partial | 🟡 partial | Glow-type coverage gap — **#461 / R4**; `--dry-run` + per-type pass/fail/skip + `--scope` proposed in #461 comment |
| UC-11 | `extract` — per-product DM triple | ✅ | n/a | writes meta.json + wire.pcapng + tree.json |
| UC-12 | `validate` — offline jsonl decode | ✅ | n/a | Go-codec PASS/FAIL per frame; Wireshark `--lua` mode pending **R12 #473** |
| UC-13 | `bench` — matrix latency | 🟡 | ✅ | total + ops/s only today; no p50/p95/p99, no cold-start, no recovery — **R13 #474** |
| UC-14 | `health` — 3-layer liveness | ❌ #300 | ✅ | not implemented for Ember+ consumer; protocol returns `does not implement HealthChecker yet` |
| UC-15 | `discover` — mDNS | ❌ R18 #477 | ❌ R18 #477 | producer announce + consumer scan on `_ember._tcp.local.` both pending |
| UC-16 | `diff` / `convert` | ✅ | n/a | tree-vs-tree diff + format conversion both work offline |

---

## Pending R-items

| R# | Issue | Status | Scope |
|---|---|---|---|
| R1 | [#468](https://github.com/by-openclaw/go-acp/issues/468) | open | layered error-code taxonomy |
| R2/R3/R7 | (folded into PR δ) | pending | runbook prose + per-OS firewall snippet |
| R4 | [#461](https://github.com/by-openclaw/go-acp/issues/461) | open + comment | export/import round-trip + `--dry-run` + `--scope` + per-type tally |
| R5b | [#469](https://github.com/by-openclaw/go-acp/issues/469) | open | standalone `tree` verb + PlantUML |
| R6 | [#470](https://github.com/by-openclaw/go-acp/issues/470) | open | `info` reads DTD version from device |
| R8 | [#471](https://github.com/by-openclaw/go-acp/issues/471) | open | service installer epic (per-OS) |
| R9 | [#472](https://github.com/by-openclaw/go-acp/issues/472) | open | provider stream idle-TTL eviction |
| R10 | [#478](https://github.com/by-openclaw/go-acp/issues/478) | **landed #479** | `stream --id` CSV multi-subscribe |
| R11 | TBD | not yet filed | `getSalvo` human format `tgt N <- Src [a,b,c]` |
| R12 | [#473](https://github.com/by-openclaw/go-acp/issues/473) | open | `validate --lua` via tshark |
| R13 | [#474](https://github.com/by-openclaw/go-acp/issues/474) | open | `bench` RFC 2544 + cold-start + recovery |
| R14 | [#475](https://github.com/by-openclaw/go-acp/issues/475) | open | `--ensure {present\|absent\|dryrun}` (Ansible) |
| R15 | [#476](https://github.com/by-openclaw/go-acp/issues/476) | open | `-v` ladder + `--log-format {text\|json\|loki}` + `--log-only` |
| R16 | TBD | not yet filed | `set` range/step round + enum-by-label |
| R17 | (folded into R8) | pending | per-OS firewall rules + runas-admin |
| R18 | [#477](https://github.com/by-openclaw/go-acp/issues/477) | open | bidirectional mDNS on `_ember._tcp.local.` |
| R19 | TBD | not yet filed | audit pass: consumer-vs-provider parity per use case |
| R20 | TBD | not yet filed | matrix doc at `docs/protocols/use-cases/emberplus.md` |

---

## Cleanup

```powershell
# Preferred: Ctrl-C in the producer window (S101 keepalive + sessions
# get a clean shutdown event, compliance counters flush to log).
#
# If running detached, kill by tracked PID:
$pid = Get-Content .audit/producer.pid
Stop-Process -Id $pid -Force
```

> ⚠ **Do NOT** broad-`Stop-Process dhs` while a Cerebrum control session
> is live — sessions get torn mid-frame and the NB driver can hang
> reconnect logic (memory: `feedback_no_multilayer_bundle`,
> `feedback_dhs_process_discipline`). Track the PID at producer start
> and use targeted shutdown.
