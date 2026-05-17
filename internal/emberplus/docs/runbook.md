# Ember+ — operational runbook

> Status: **draft** under codeowner review (this PR).
> Source-of-truth for verb-by-verb validation against the
> integration-test fixture set. The capture batch (per-type fixtures
> under `internal/emberplus/testdata/protocol_types/`) lands separately.

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

| Verb | Status | Wire shape | Returns |
|---|---|---|---|
| `info` | ✅ | GetDirectory(root) + identity walk | per-slot online + status |
| `walk` | ✅ | GetDirectory recursive | every object in the tree |
| `get` | ✅ | GetDirectory(path) | one value |
| `set` | ✅ | SetValue | confirmed value (or ValidationError) |
| `watch` | ✅ | Subscribe(30) on streams + implicit on params/matrices | live stream of value/connection changes |
| `matrix` | ✅ | Connect/Disconnect/Absolute on a Matrix | applied connection state |
| `invoke` | ✅ | Invoke + InvocationResult | tuple result + Success=true/false |
| `stream` | ✅ | Subscribe(30) on streamIdentifier filter | stream feed |
| `profile` | ✅ | Walk + count compliance.Profile events | classification (strict / partial) |
| `export` | 🟡 | Walk + write JSON/YAML/CSV | file at `--out` (Glow-type coverage gap — [#461](https://github.com/by-openclaw/go-acp/issues/461)) |
| `import` | 🟡 | Read JSON/YAML/CSV + apply | tree state on remote (same gap) |
| `extract` | ✅ | Walk + write per-product DM (meta+wire+tree) | files in fixture layout |
| `diff` | ✅ | Two tree.json compared | text diff or CHANGELOG section |
| `convert` | ✅ | Translate between JSON / YAML / CSV | offline file write |
| `validate` | ✅ | Decode captured `frames.jsonl` through codec | PASS/FAIL per frame |
| `bench` | 🟡 | Fire N matrix ops over 1 TCP session | total + ops/s (RFC 2544 + percentiles + cold-start pending [#474](https://github.com/by-openclaw/go-acp/issues/474)) |
| `health` | ❌ **TODO** | 3-layer session liveness | not wired for Ember+ — [#300](https://github.com/by-openclaw/go-acp/issues/300) (consumer + provider) |
| `discover` | ❌ **TODO** | mDNS scan on `_ember._tcp.local.` | pending [#477](https://github.com/by-openclaw/go-acp/issues/477) (consumer + provider) |

Legend: ✅ working · 🟡 partial · ❌ not implemented.

---

## Addressing — by path vs by OID

Every Ember+ object has two addresses: a **numeric OID** (e.g. `1.6.1`) and a **dotted label path** (e.g. `dhs-emberplus-integration.types.vInteger`). The runbook shows the **dotted path** form in commands; the **OID** is printed as a comment alongside each Happy-path example so operators can cross-reference Wireshark dissector output (`dhs_emberplus.*` fields) and capture files.

Top-level OID map (current integration-test provider):

| OID | Label | Notes |
|---|---|---|
| `1` | `dhs-emberplus-integration` | synthetic root |
| `1.0` | `identity` | product / company / version / dtdVersion |
| `1.1` | `oneToN` | 16×16 matrix |
| `1.2` | `oneToOne` | 16×16 matrix |
| `1.3` | `nToN` | 16×16 matrix |
| `1.4` | `dynamic` | 128×128 declared, 16 sparse |
| `1.5` | `functions` | builtins |
| `1.6` | `types` | every ParameterType + 3 streams |

> Today `--path` accepts the dotted label form only. Accepting OIDs in `--path` (e.g. `--path 1.6.1`) so operators can address by either form is pending **R21** (per memory `project_path_by_id`).

---

## 1. `info` — device summary

### Happy

```powershell
.\bin\dhs.exe consumer emberplus info 127.0.0.1 --port 9100
# OID = 1 (root)
```

Expected:

```
device       127.0.0.1:9100
protocol     emberplus v1                ← TODO: surface DTD `2.60` from identity, not "v1" — R6 #470
slots        1

per-slot status:
  slot  0   status=present    online=true        ← post #459
```

> The `protocol emberplus v1` line shows the internal plugin version, not the device's Ember+ DTD revision. **R6 [#470](https://github.com/by-openclaw/go-acp/issues/470)** rewires `info` to read `identity.dtdVersion` from the device and display the real DTD revision (e.g. `dtd 2.60`).

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
# OID = 1 (root) — recursive
```

Expected: ≈548 lines of tree dump, `tree_size ≈ 1361` objects. Two files written:

- `--capture` target: [.audit/walks/demo.jsonl](../../../.audit/walks/demo.jsonl) — raw S101 frames (hex + dir + ts per line, one frame per line) — per ADR-0021
- DM auto-extract: [.cache/dm/emberplus/dhs-emberplus-integration@1.0.0.json](../../../.cache/dm/emberplus/) — feeds the `--dm` hot-load on subsequent verbs (per ADR-0022 card data model)

> The DM file is the same byte-for-byte shape as the source-of-truth fixture at [`internal/emberplus/testdata/integration-test/dm/emberplus/`](../testdata/integration-test/dm/emberplus/). Captured walk → cached DM → hot-load saves the per-call wire walk on subsequent verbs.

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| connection refused | `walk ... --port 9999` | s101 framing error | 1 |
| timeout too small | `walk ... --timeout 1ms` | `walk for "walk": context deadline exceeded` | 1 |
| missing host | `walk --port 9100` | usage error | 2 |

---

## 3. `get` — read one Parameter

### Happy

| OID | Path | Example value |
|---|---|---|
| `1.6.1` | `types.vInteger` | `42` |
| `1.6.2` | `types.vReal` | `3.14` (factor=100, format=`%.2f`, unit=dB) |
| `1.6.3` | `types.vString` | `"hello"` |
| `1.6.4` | `types.vBoolean` | `true` |
| `1.6.5` | `types.vTrigger` | trigger-type (read-only "tap") |
| `1.6.6` | `types.vEnum` | `1` (enumMap: Off=0, Low=1, Medium=2, High=3) |
| `1.6.7` | `types.vOctets` | `"aGVsbG8="` (known broken in some UIs; wire OK) |
| `1.6.10` | `types.vu_zero` | `-60.00` (streamIdentifier=0 — must NOT be treated as absent) |
| `1.6.11` | `types.vu_left` | live stream |
| `1.6.12` | `types.vu_right` | live stream |
| `1.0.4` | `identity.dtdVersion` | `"2.60"` |

```powershell
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vInteger
# OID = 1.6.1
# value = 42

.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.identity.dtdVersion
# OID = 1.0.4
# value = "2.60"
```

> Address by OID directly (`--path 1.6.1`) is pending **R21** (per [Addressing](#addressing--by-path-vs-by-oid)).

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| wrong path | `get ... --path bogus.path.here` | `object not found (tree has 1361 entries)` | 1 |
| missing required path | `get 127.0.0.1 --port 9100` | usage error | 2 |
| connection refused | `get ... --port 9999` | s101 framing error | 1 |

---

## 4. `set` — write one Parameter

### Happy

```powershell
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.gain --value -25
# OID = 1.3.targetParams.0.gain  (numeric form varies by parametersLocation seed)
# confirmed value = -25

.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.mute --value true
# confirmed value = true

.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vEnum --value 3
# OID = 1.6.6
# confirmed value = 3
```

> Out-of-range / step-mismatch / enum-by-label semantics pending **R16** (not yet filed).

### Errors (post #453)

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| unparseable int (#445) | `set ... gain --value -25.5` | `validation: value: invalid integer "-25.5"` | 2 |
| unparseable int (letters) | `set ... gain --value abc` | `validation: value: invalid integer "abc"` | 2 |
| unparseable real | `set ... vReal --value not-a-float` | `validation: value: invalid real "not-a-float"` | 2 |
| enum overflow | `set ... vEnum --value 999` | `validation: value: invalid enum index "999"` | 2 |
| write to read-only stream | `set ... vu_zero --value -10` | protocol error (provider rejects) | 1 |
| wrong path | `set --path bogus --value 1` | `object not found` | 1 |

---

## 5. `watch` — live updates

Three event sources, all delivered through the same `watch` feed:

| Source | What it carries | Default behaviour |
|---|---|---|
| **stream params** | `streamIdentifier` payload — high-frequency vu / fader values | gated by Subscribe(30); use `--streams-only` to limit to these |
| **glow params** | Parameter value-change announces (non-stream) | per `internal/emberplus/CLAUDE.md` "Known deviations" — provider broadcasts plain-Parameter announces to every connected session (libember-cpp / Lawo parity); no explicit subscribe required |
| **matrix tally** | `Matrix.Connection` change announces (crosspoint set / disconnect) | as soon as the walker discovers a `Matrix` element, the consumer is implicitly subscribed; you receive every tally change without an explicit `Subscribe`. Default is "subscribe to changes", not "stream + glow" — adjust with `--streams-only` or `--path` to scope |

### Happy

```powershell
# All updates — stream + glow + matrix tally — one merged feed
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100
# OID scope = 1 (root) — every announce flows through

# Streams only (suppress glow + tally noise)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 --streams-only
# Updates every ~500ms

# Watch one stream Parameter
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vu_zero --streams-only
# OID = 1.6.10 — only vu_zero updates

# Watch a glow Parameter (non-stream) — change-of-value announces
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.gain
# Emits whenever target-0 gain changes (set by us or another session)

# Watch matrix tally — every crosspoint connect/disconnect on the matrix
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix
# OID = 1.1 — tally announces fire on every Connect / Disconnect / Absolute
```

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| connection refused | `watch ... --port 9999` | s101 framing error | 1 |
| bad path filter | `watch --path bogus` | no events (filter matches nothing); exits clean on Ctrl-C | 0 / 1 |
| missing host | `watch --port 9100` | usage error | 2 |

---

## 6. `matrix` — Connect / Disconnect / Absolute

| Matrix | OID | Type | Capacity |
|---|---|---|---|
| oneToN | `1.1` | one source per target; multiple targets may share a source | 16×16 |
| oneToOne | `1.2` | strict 1:1 — source-steal accepted (post #467) | 16×16 |
| nToN | `1.3` | up to 4 sources/target, 64 total | 16×16 |
| dynamic | `1.4` | sparse — 16 of 128² declared | 128×128 |

### Happy

```powershell
# nToN — multi-source SET
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --target 10 --sources 3,4,5 --op absolute
# OID = 1.3
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
# OID = 1.1 — target 0 now routed from src 7; prior src 0 dropped

# oneToOne — source steal (post #467)
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToOne.matrix `
    --target 0 --sources 5 --op absolute
# OID = 1.2 — matrix connect: target 0 ← sources [5] (op=absolute)
# Source 5 was on target 5; now stolen to target 0 — target 5 implicitly disconnected.
# Profile counter: onetoone_source_steal_accepted += 1
```

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| oneToN over-cardinality | `matrix oneToN --target 0 --sources 1,2 --op absolute` | `matrix validation: oneToN matrix: target 0 would have 2 sources (max 1) [spec p.33]` | 1 |
| nToN over capacity | `matrix nToN --target 0 --sources 0,1,2,3,4,5 --op absolute` | `matrix validation: nToN matrix: target 0 would have 5 sources (max 4 per target)` | 1 |
| locked target | `matrix oneToN --target 2 --sources 5 --op absolute` | rejected by provider; tally announce echoes back unchanged with `disposition: locked` | 1 |
| wrong matrix path | `matrix --path bogus` | `matrix not found at path "bogus"` | 1 |
| missing target | `matrix ... --sources 1` (no `--target`) | usage error | 2 |

---

## 7. `invoke` — function RPC

Function subtree at OID `1.5` carries six builtins:

| OID | Path | Args (tuple) | Returns |
|---|---|---|---|
| `1.5.1` | `functions.setLock` | `(matrixRef, target, locked)` | `[previousLockState]` |
| `1.5.2` | `functions.listLocks` | `(matrixRef)` | `[list of locked target indices]` |
| `1.5.3` | `functions.storeSalvo` | `(matrixRef, slot, label)` | `[true]` |
| `1.5.4` | `functions.recallSalvo` | `(matrixRef, slot)` | `[rowsRestored]` |
| `1.5.5` | `functions.listSalvos` | `(matrixRef)` | `[list of slot IDs]` |
| `1.5.6` | `functions.getSalvo` | `(matrixRef, slot)` | `[serialized tgt=src list]` — human format pending **R11** |

`matrixRef` accepts both OID (`1.1.3` → `oneToN.matrix`) and dotted path (`dhs-emberplus-integration.oneToN.matrix`) post #466.

### Happy

```powershell
# Lock target 3 on oneToN — by matrix OID
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "1.1.3,3,true"
# OID = 1.5.1 — invocation 1: success=true
# result: [false]       (previous lock state)

# Same via dotted matrixRef (post #466)
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "dhs-emberplus-integration.oneToN.matrix,4,true"
# invocation 1: success=true · result: [false]

# List locked targets
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.listLocks `
    --args "1.1.3"
# OID = 1.5.2 — result: [2,3,4]   (2 was pre-locked from seed)

# Store salvo — all current connections on oneToN
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.storeSalvo `
    --args "1.1.3,99,"
# OID = 1.5.3 — result: [true]

# Get salvo dump
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.getSalvo `
    --args "1.1.3,99"
# OID = 1.5.6 — result: ["0=0;1=1;3=3;..."]   (tgt=src semicolon-separated; human format pending R11)

# Recall salvo
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.recallSalvo `
    --args "1.1.3,99"
# OID = 1.5.4 — result: [N]    (rows restored)
```

### Errors (post #455 / #457)

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| bogus matrixRef | `invoke setLock --args "bogus.path,1,true"` | `invocation 1: success=false` (was silent success pre-#457) | 1 |
| missing arg | `invoke setLock --args "1.1.3"` | `setLock: need (matrixPath, target, locked)` | 1 |
| wrong type arg | `invoke setLock --args "1.1.3,abc,true"` | bad arg types | 1 |
| missing `--path` | `invoke --args "1.1.3,1,true"` | usage error | 2 |

---

## 8. `stream` — explicit subscribe

Three streamIdentifier values declared by the integration-test provider:

| streamIdentifier | OID | Path | Notes |
|---|---|---|---|
| `0` | `1.6.10` | `types.vu_zero` | id=0 is valid (NOT "absent") |
| `1001` | `1.6.11` | `types.vu_left` | live audio meter |
| `1002` | `1.6.12` | `types.vu_right` | live audio meter |

### Happy

```powershell
.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0
# Subscribes to streamIdentifier=0 only (vu_zero at OID 1.6.10)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 1001
# Subscribes to vu_left only (OID 1.6.11)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001
# Subscribes to {vu_zero, vu_left} — R10 multi-subscribe post #479

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001,1002
# All three explicitly (equivalent to no flag)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100
# Subscribes to ALL streams (3 today)
```

### Provider idle-eviction (pending R9 [#472](https://github.com/by-openclaw/go-acp/issues/472))

If the consumer goes silent on S101 keepalive, the provider currently keeps streaming forever — accumulating dead subscribers. **R9** evicts an idle subscriber after N missed keepalive intervals (Subscribe(31) implicit on the provider side, free the slot).

Today the operator-visible behavior on an abruptly-killed consumer:

- producer log: `streamer pushed N frames to dead subscriber sid=...` (after R9: `streamer evicted idle subscriber sid=... idle=Xs`)
- network: streams keep flowing to the consumer's port until OS-level FIN

### Errors

| Case | Command | Error | Exit |
|---|---|---|---|
| bad token | `stream ... --id abc` | `validation: --id: bad token "abc"` | 2 |
| empty token mid-csv | `stream ... --id 0,,1001` | `validation: --id: empty token in "0,,1001"` | 2 |
| trailing comma | `stream ... --id 0,1001,` | `validation: --id: empty token in "0,1001,"` | 2 |
| connection refused | `stream ... --port 9999` | s101 framing error | 1 |
| missing host | `stream --port 9100` | usage error | 2 |

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

After triggering oneToOne source-steal (post #467), the counter `onetoone_source_steal_accepted` appears with its count.

### Enhancements pending — R22 (not yet filed)

Today `profile` aggregates into one classification line + an event count. The codeowner asked: could it be enhanced? Proposed scope for **R22**:

| Enhancement | Today | After R22 |
|---|---|---|
| Per-event-kind breakdown | aggregate counter only | one row per `compliance.Event` kind with count + first/last timestamp |
| Per-session attribution | n/a (aggregated across all sessions) | optional `--by-session` to group by remote peer |
| Machine-readable output | text only | `--format json` for CI ingestion |
| Time-windowed filter | n/a | `--since 5m` to only count recent events |
| Event log dump | counter only | `--show-events` prints every observed `compliance.Event` with detail |

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| connection refused | `profile ... --port 9999` | s101 framing error | 1 |
| missing host | `profile --port 9100` | usage error | 2 |

---

## 10. `export` / `import` — round-trip (PARTIAL — [#461](https://github.com/by-openclaw/go-acp/issues/461))

Quick form:

```powershell
.\bin\dhs.exe consumer emberplus export 127.0.0.1 --port 9100 --format yaml --out demo.yaml
.\bin\dhs.exe consumer emberplus import demo.yaml --port 9100
```

Full reference — `--format {json|yaml|csv}` matrix, partial-export header, `--dry-run` import with per-Glow-type pass/fail/skip tally, `--scope <path>` for sample exports, error codes, and the complete R4 [#461](https://github.com/by-openclaw/go-acp/issues/461) round-trip spec:

→ **[`runbook/export-import.md`](runbook/export-import.md)**

---

## 11. `extract` — capture per-product DM triple

```powershell
.\bin\dhs.exe consumer emberplus extract 127.0.0.1 --port 9100 `
    --manufacturer BY-Systems --product dhs-emberplus-integration `
    --direction in --version 1.0.0 --out .audit/extract-demo
# OID = 1 (root) — recursive walk
# Writes meta.json + wire.pcapng + tree.json under the fixture layout
```

### Flags

| Flag | Purpose | Example |
|---|---|---|
| `--manufacturer` | Manufacturer string baked into `meta.json` and the cache filename | `BY-Systems` |
| `--product` | Product / Model name — drives `<Model@SwRev>.json` per ADR-0022 | `dhs-emberplus-integration` |
| `--direction` | Capture direction from the **consumer's** perspective: `in` = device→consumer (announces, replies); `out` = consumer→device (commands). Stored in `meta.json` so replay tooling knows whether to feed frames forward or reverse | `in` |
| `--version` | Software revision (SwRev) of the device being captured. Forms the cache filename `<Model@SwRev>.json` per ADR-0022. Multiple SwRev captures coexist side-by-side; the consumer hot-loads by exact match | `1.0.0` |
| `--out` | Output directory; the triple `meta.json` + `wire.pcapng` + `tree.json` lands inside | `.audit/extract-demo` |

Layout written:

```
.audit/extract-demo/
├── meta.json     manufacturer + product + version + direction + capturedAt + protocol
├── wire.pcapng   S101 frames replay-ready in Wireshark with dhs_emberplus.lua loaded
└── tree.json     decoded Glow tree (same shape as .cache/dm/emberplus/<Model@SwRev>.json)
```

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| target dir unwritable | `extract ... --out /no/perm/` | `extract: mkdir /no/perm: permission denied` | 1 |
| missing required flag | `extract ... --product foo` (no `--version`) | usage error | 2 |
| connection refused | `extract ... --port 9999` | s101 framing error | 1 |
| invalid direction | `extract ... --direction sideways` | `validation: --direction: must be "in" or "out"` | 2 |

---

## 12. `validate` — offline frame decode (ADR-0021)

Quick form:

```powershell
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl
# Decodes each frame through the codec; emits PASS / FAIL per frame + summary
```

Full reference — Wireshark `--lua` mode ([#473](https://github.com/by-openclaw/go-acp/issues/473)), `--report <md|json>` structured report (R23 not yet filed), error codes, and the per-layer pass-rate table format:

→ **[`runbook/validate.md`](runbook/validate.md)**

---

## 13. `bench` — matrix latency

```powershell
.\bin\dhs.exe consumer emberplus bench 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --n 1000 --op connect --targets 16 --sources 16
# OID = 1.3 — total: 1000 ops in N ms, M ops/s
```

Flags: `--path` (matrix path), `--n` (op count, default 100), `--op` (connect / absolute / disconnect, default connect), `--targets` / `--sources` (wrap modulo, default 4).

### Pending — R13 [#474](https://github.com/by-openclaw/go-acp/issues/474) (expanded scope)

Today only `matrix` ops are benchable. R13 [#474](https://github.com/by-openclaw/go-acp/issues/474) extends the verb with:

| Per-op-kind profile | What it measures | Spec anchor |
|---|---|---|
| `--profile latency` | round-trip p50/p95/p99/p99.9 µs per op kind: `get` / `set` / `matrix` / `invoke` / **glow-walk** / **stream-subscribe** / **function-invoke** / **s101-keepalive** | RFC 2544 |
| `--profile throughput` | max sustained ops/s with <1% frame loss over 60 s — per op kind | RFC 2544 §26 |
| `--profile cold-start` | producer launch → "tree fully exposed + listener bound + streamer running" — p50/p95/p99 across N=10 launches | new (per ADR-0025) |
| `--profile recovery` | TCP disconnect → first successful op after reconnect — p50/p95/p99 | new |
| `--transport tcp` (today) / `--transport <variant>` | Ember+ is TCP-only on the wire; `--transport` becomes a placeholder for future variants (Ember+/UDP draft, Ember+/TLS) — today rejects anything but `tcp` | per Ember+ spec p.22 |

### Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| missing `--path` | `bench --port 9100 --n 100` | `--path is required` | 1 |
| n ≤ 0 | `bench ... --n -1` | `--n must be > 0` | 1 |
| invalid op | `bench ... --op spin` | `op must be connect, absolute, or disconnect` | 1 |
| matrix path resolves to non-matrix | `bench ... --path dhs-emberplus-integration.types.vInteger` | `bench: target not a Matrix` | 1 |
| connection refused | `bench ... --port 9999` | s101 framing error | 1 |

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
| UC-14 | `health` — 3-layer liveness | ❌ [#300](https://github.com/by-openclaw/go-acp/issues/300) | ❌ [#300](https://github.com/by-openclaw/go-acp/issues/300) | TODO both: consumer-side verb returns `does not implement HealthChecker yet`; provider-side `IsOnline` aggregation per `project_session_health` exists but is not exposed through a control-plane endpoint |
| UC-15 | `discover` — mDNS | ❌ R18 [#477](https://github.com/by-openclaw/go-acp/issues/477) | ❌ R18 [#477](https://github.com/by-openclaw/go-acp/issues/477) | TODO both: producer announce + consumer scan on `_ember._tcp.local.` |
| UC-16 | `diff` / `convert` | ✅ | n/a | tree-vs-tree diff + format conversion both work offline |

---

## Pending R-items

| R# | Issue | Status | Scope |
|---|---|---|---|
| R1 | [#468](https://github.com/by-openclaw/go-acp/issues/468) | open | layered error-code taxonomy (covers exit-code mapping per error class) |
| R2 | TBD | folded into this runbook | runbook prose (verb-by-verb walkthrough — this doc) |
| R3 | TBD | folded into this runbook | runbook error coverage (each verb's Errors table) |
| R4 | [#461](https://github.com/by-openclaw/go-acp/issues/461) | open + comment | export/import round-trip + `--dry-run` + `--scope` + per-type tally — full spec in [`runbook/export-import.md`](runbook/export-import.md) |
| R5b | [#469](https://github.com/by-openclaw/go-acp/issues/469) | open | standalone `tree` verb + PlantUML |
| R6 | [#470](https://github.com/by-openclaw/go-acp/issues/470) | open | `info` reads DTD version from device (kills the "emberplus v1" line) |
| R7 | TBD | folded into this runbook | runbook prose continued |
| R8 | [#471](https://github.com/by-openclaw/go-acp/issues/471) | open | service installer epic (per-OS) |
| R9 | [#472](https://github.com/by-openclaw/go-acp/issues/472) | open | provider stream idle-TTL eviction (no-keepalive → unsub streams) |
| R10 | [#478](https://github.com/by-openclaw/go-acp/issues/478) | **landed [#479](https://github.com/by-openclaw/go-acp/pull/479)** | `stream --id` CSV multi-subscribe |
| R11 | TBD | not yet filed | `getSalvo` human format `tgt N <- Src [a,b,c]` |
| R12 | [#473](https://github.com/by-openclaw/go-acp/issues/473) | open | `validate --lua` via tshark — see [`runbook/validate.md`](runbook/validate.md) |
| R13 | [#474](https://github.com/by-openclaw/go-acp/issues/474) | open | `bench` RFC 2544 + cold-start + recovery; expanded scope: glow / function / stream / transport per-op-kind profiles |
| R14 | [#475](https://github.com/by-openclaw/go-acp/issues/475) | open | `--ensure {present\|absent\|dryrun}` (Ansible) |
| R15 | [#476](https://github.com/by-openclaw/go-acp/issues/476) | open | `-v` ladder + `--log-format {text\|json\|loki}` + `--log-only` |
| R16 | TBD | not yet filed | `set` range/step round + enum-by-label |
| R17 | folded into R8 | pending | per-OS firewall rules + runas-admin |
| R18 | [#477](https://github.com/by-openclaw/go-acp/issues/477) | open | bidirectional mDNS on `_ember._tcp.local.` (consumer + provider) |
| R19 | TBD | not yet filed | audit pass: consumer-vs-provider parity per use case |
| R20 | TBD | not yet filed | matrix doc at `docs/protocols/use-cases/emberplus.md` |
| **R21** | TBD | not yet filed | `--path` accepts OID alongside dotted label (per memory `project_path_by_id`) |
| **R22** | TBD | not yet filed | `profile` verb enhancements: per-event-kind tally + JSON output + history/filter |
| **R23** | TBD | not yet filed | `validate --report <md\|json>` structured report — see [`runbook/validate.md`](runbook/validate.md) |

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
