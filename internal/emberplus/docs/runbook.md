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
| `tree` | ✅ | GetDirectory recursive (or DM hot-load) | ASCII or PlantUML mindmap of the tree (post R5b [#469](https://github.com/by-openclaw/go-acp/issues/469)) |
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

> Today `--path` accepts the dotted label form only. Accepting OIDs in `--path` (e.g. `--path 1.6.1`) so operators can address by either form is pending **R21 [#486](https://github.com/by-openclaw/go-acp/issues/486)** (per memory `project_path_by_id`).

---

## Exit codes & error taxonomy

The binary emits a stable `<layer>:<code>: <human message>` string on stderr + a small-integer exit code per standard Unix (`0`/`1`/`2`). **Diagnosis lives exclusively in the error string — never in the exit code.** Locked by R1 [#468](https://github.com/by-openclaw/go-acp/issues/468) (delivered across PRs [#491](https://github.com/by-openclaw/go-acp/pull/491) · [#492](https://github.com/by-openclaw/go-acp/pull/492) · [#493](https://github.com/by-openclaw/go-acp/pull/493) · [#494](https://github.com/by-openclaw/go-acp/pull/494) · [#495](https://github.com/by-openclaw/go-acp/pull/495) · [#496](https://github.com/by-openclaw/go-acp/pull/496) · [#497](https://github.com/by-openclaw/go-acp/pull/497) · this final pass). Canonical list: [`docs/protocols/error-codes.md`](../../../docs/protocols/error-codes.md).

### Exit code classes — Unix-standard, cross-OS uniform

| Exit | Class | When |
|---|---|---|
| **0** | success | verb completed; no errors |
| **1** | any runtime error — caller reads stderr for the precise code | `transport:*`, `s101:*`, `glow:*`, `matrix:*`, `emberplus:*`, `session:*` |
| **2** | usage / state error — Go `flag` parse failure or caller's input fault | `validation:*`, `plugin:object-not-found`, `plugin:not-connected`, missing required flag |

Never `3+`. Cross-OS: PowerShell `$LASTEXITCODE`, Bash `$?`, `cmd.exe %ERRORLEVEL%` all read the same scheme.

### Layer prefixes

| Prefix | Owned by | Examples |
|---|---|---|
| `transport:` | `internal/transport/` + OS sockets | `transport:refused`, `transport:timeout`, `transport:reset` |
| `s101:` | `internal/emberplus/codec/s101/` | `s101:crc-mismatch`, `s101:bad-escape`, `s101:multi-frame-truncated` |
| `glow:` | `internal/emberplus/codec/glow/` + `codec/ber/` | `glow:bad-tag`, `glow:bad-length`, `glow:bad-real`, `glow:unknown-application-tag` |
| `matrix:` | `internal/emberplus/codec/matrix/` | `matrix:cardinality-exceeded`, `matrix:target-locked`, `matrix:max-connects-per-target` |
| `emberplus:` | `internal/emberplus/consumer/` + `provider/` | `emberplus:invocation-failed`, `emberplus:invocation-failed-with-description` |
| `validation:` | `internal/consumer/` validation layer | `validation:invalid-integer`, `validation:out-of-range-low`, `validation:invalid-enum-label` |
| `plugin:` | `internal/<proto>/` plugin state | `plugin:not-connected`, `plugin:not-walked`, `plugin:object-not-found` |
| `session:` | session-state layer | `session:write-timeout`, `session:write-coerced`, `session:dead` |

### Ember+ doesn't define wire-level error codes

Unlike ACP2 (`stat=1..5`) or HTTP (`4xx/5xx`), the Ember+ wire format carries **no native error codes**. The only error signals on the wire are:

| Wire signal | Meaning |
|---|---|
| `InvocationResult.Success=false` | Invoke failed — **no reason code** |
| `Connection.Disposition=locked` | target locked, write rejected |
| `offlineElement` marker | element no longer valid (tombstone) |
| free-text `description` field | sometimes used by providers for human reason |

The `emberplus:*` and `matrix:*` codes are this project's invention layered on top of those signals; operators won't find them in the Ember+ Documentation PDF.

### Scripting dispatch (Ansible / shell)

```powershell
$out = (.\bin\dhs.exe consumer emberplus set --path types.vInteger --value abc 2>&1)
switch -Regex ($out) {
    '^validation:'  { 'caller error — bad input' }
    '^transport:'   { 'network problem — retry' }
    '^matrix:'      { 'protocol rejection — investigate' }
}
# $LASTEXITCODE is one of 0 / 1 / 2 / 3
```

```yaml
# Ansible task example
- name: gain set
  command: dhs consumer emberplus set --path ... --value -25
  register: r
  failed_when:
    - r.rc != 0
    - r.stderr is not search('^validation:invalid-enum-label')   # tolerate one specific code
```

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
protocol     emberplus v1
dtd_version  2.60                                 ← post R6 #470
slots        1

per-slot status:
  slot  0   status=present    online=true        ← post #459
```

> `dtd_version` is the wire-level Glow DTD revision the connected device advertises. It is captured from the S101 app-bytes on the first EmBER frame; if a provider emits the older 5-byte S101 header (no app-bytes), the plugin falls back to walking `identity.dtdVersion`. Both routes are best-effort — when neither produces a value the line reads `dtd_version unknown` and `info` still exits 0. Refs **R6 [#470](https://github.com/by-openclaw/go-acp/issues/470)**.

### Errors

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| missing host | `info --port 9100` | (usage error) | 2 |
| connection refused | `info 127.0.0.1 --port 9999 --timeout 2s` | `transport:refused` | 1 |
| unreachable host | `info 192.0.2.1 --port 9100 --timeout 2s` | `transport:timeout` | 1 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| connection refused | `walk ... --port 9999` | `transport:refused` | 1 |
| timeout too small | `walk ... --timeout 1ms` | `transport:timeout` | 1 |
| missing host | `walk --port 9100` | (usage error) | 2 |
| bad S101 frame | (corrupted peer) | `s101:crc-mismatch` / `s101:bad-escape` | 1 |
| unknown Glow tag | (peer emits non-spec tag) | `glow:unknown-application-tag` | 1 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| wrong path | `get ... --path bogus.path.here` | `plugin:object-not-found` | 2 |
| invalid OID syntax (post R21) | `get ... --path 1..2` | `validation:invalid-oid` | 2 |
| path resolves to non-Parameter | `get ... --path dhs-emberplus-integration.functions` | `plugin:wrong-kind` | 2 |
| missing required path | `get 127.0.0.1 --port 9100` | (usage error) | 2 |
| connection refused | `get ... --port 9999` | `transport:refused` | 1 |

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

> Out-of-range / step-mismatch / enum-by-label semantics are live as of **R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)**:
>
> - Numeric `--value` below/above the Parameter's declared `minimum`/`maximum` (Ember+ Contents [3]/[4]) → `validation:out-of-range-low` / `validation:out-of-range-high` (exit `2`).
> - Numeric `--value` not aligned to the declared `step` (Contents [11]) → `validation:step-misaligned` (exit `2`); the message names the nearest legal grid point. Pass `--round` to snap and continue.
> - Enum `--value` as a **label** (e.g. `--value Low` on `{Off=0, Low=1, Medium=2, High=3}`) → resolved via the Parameter's enumMap to the integer index, then sent on the wire.
> - Enum label not present in enumMap → `validation:invalid-enum-label` (exit `2`); stderr lists valid labels alphabetised.
> - Enum label on a Parameter that ships **no** enumMap → `validation:enum-not-supported` (exit `2`) — address by integer index.
> - `--round` on a non-numeric Parameter (string / enum / bool) → `validation:round-not-applicable` (exit `2`).

### Errors (post #453 + post R16 [#483](https://github.com/by-openclaw/go-acp/issues/483))

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| unparseable int (#445) | `set ... gain --value -25.5` | `validation:invalid-integer` | 2 |
| unparseable int (letters) | `set ... gain --value abc` | `validation:invalid-integer` | 2 |
| unparseable real | `set ... vReal --value not-a-float` | `validation:invalid-real` | 2 |
| enum overflow | `set ... vEnum --value 999` | `validation:invalid-enum-index` | 2 |
| **out of range low (R16)** | `set ... vInteger --value -200` (min=-100) | `validation:out-of-range-low` | 2 |
| **out of range high (R16)** | `set ... vInteger --value 200` (max=100) | `validation:out-of-range-high` | 2 |
| **step misaligned (R16)** | `set ... vReal --value 1.3` (step=0.5) | `validation:step-misaligned` | 2 |
| **invalid enum label (R16)** | `set ... vEnum --value Bogus` | `validation:invalid-enum-label` | 2 |
| write to read-only stream | `set ... vu_zero --value -10` | `emberplus:invocation-failed` (provider rejects) | 1 |
| wrong path | `set --path bogus --value 1` | `plugin:object-not-found` | 2 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| connection refused | `watch ... --port 9999` | `transport:refused` | 1 |
| bad path filter | `watch --path bogus` | (no error — filter matches nothing; exits 0 on Ctrl-C) | 0 |
| missing host | `watch --port 9100` | (usage error) | 2 |
| connection lost mid-stream | (peer kills TCP) | `transport:reset` | 1 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| oneToN over-cardinality | `matrix oneToN --target 0 --sources 1,2 --op absolute` | `matrix:cardinality-exceeded` | 1 |
| nToN over capacity | `matrix nToN --target 0 --sources 0,1,2,3,4,5 --op absolute` | `matrix:max-connects-per-target` | 1 |
| nToN total capacity | (sum exceeds `maxTotalConnects`) | `matrix:max-total-connects` | 1 |
| locked target | `matrix oneToN --target 2 --sources 5 --op absolute` | `matrix:target-locked` (provider echoes `disposition:locked`) | 1 |
| wrong matrix path | `matrix --path bogus` | `plugin:object-not-found` | 2 |
| path resolves to non-Matrix | `matrix --path dhs-emberplus-integration.types.vInteger` | `plugin:wrong-kind` | 2 |
| missing target | `matrix ... --sources 1` (no `--target`) | (usage error) | 2 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| bogus matrixRef | `invoke setLock --args "bogus.path,1,true"` | `emberplus:invocation-failed` (provider returns `Success=false`) | 1 |
| description on failure | (provider returns `Success=false` + description) | `emberplus:invocation-failed-with-description` (description in stderr) | 1 |
| missing arg | `invoke setLock --args "1.1.3"` | `validation:invocation-args-count` | 2 |
| wrong type arg | `invoke setLock --args "1.1.3,abc,true"` | `validation:invocation-arg-type` | 2 |
| missing `--path` | `invoke --args "1.1.3,1,true"` | (usage error) | 2 |
| `--format bogus` (R11 [#482](https://github.com/by-openclaw/go-acp/issues/482)) | `invoke ... --format bogus` | `validation:invalid-format` | 2 |

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

| Case | Command | Error code | Exit |
|---|---|---|---|
| bad token | `stream ... --id abc` | `validation:invalid-id-token` | 2 |
| empty token mid-csv | `stream ... --id 0,,1001` | `validation:invalid-id-token` (empty) | 2 |
| trailing comma | `stream ... --id 0,1001,` | `validation:invalid-id-token` (empty) | 2 |
| connection refused | `stream ... --port 9999` | `transport:refused` | 1 |
| missing host | `stream --port 9100` | (usage error) | 2 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| connection refused | `profile ... --port 9999` | `transport:refused` | 1 |
| missing host | `profile --port 9100` | (usage error) | 2 |
| invalid `--format` (R22 [#487](https://github.com/by-openclaw/go-acp/issues/487)) | `profile ... --format yaml` | `validation:invalid-format` | 2 |
| invalid `--since` (R22) | `profile ... --since 5xx` | `validation:invalid-duration` | 2 |
| `--by-session` without R24 (R22) | `profile ... --by-session` | `plugin:by-session-unavailable` | 2 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| target dir unwritable | `extract ... --out /no/perm/` | `transport:report-target-unwritable` | 1 |
| missing required flag | `extract ... --product foo` (no `--version`) | (usage error) | 2 |
| connection refused | `extract ... --port 9999` | `transport:refused` | 1 |
| invalid direction | `extract ... --direction sideways` | `validation:invalid-direction` | 2 |

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

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| missing `--path` | `bench --port 9100 --n 100` | (usage error) | 2 |
| n ≤ 0 | `bench ... --n -1` | `validation:invalid-n` | 2 |
| invalid op | `bench ... --op spin` | `validation:invalid-op` | 2 |
| path resolves to non-Matrix | `bench ... --path dhs-emberplus-integration.types.vInteger` | `plugin:wrong-kind` | 2 |
| connection refused | `bench ... --port 9999` | `transport:refused` | 1 |

---

## Use-case status — Ember+ (Consumer + Provider)

Current state on `main`. ✅ working, 🟡 partial, ❌ not implemented.

| Seq | Use case | Consumer | Provider | Notes / refs |
|---|---|---|---|---|
| UC-1 | `info` — device summary | ✅ | ✅ | Online correct post #459 |
| UC-2 | `walk` — full enumeration | ✅ | ✅ | tree_size ≈ 1361; DM auto-extracted to `.cache/dm/emberplus/<identity>@<rev>.json` |
| UC-3 | `get` — single Parameter | ✅ | ✅ | every ParameterType + stream id=0 + id>0 + identity.dtdVersion |
| UC-4 | `set` — single Parameter | ✅ | ✅ | typed-validation errors post #453; range / step / enum-by-label live post R16 [#483](https://github.com/by-openclaw/go-acp/issues/483) (with `--round` snap) |
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
| R2 | n/a — this doc | folded | runbook prose (verb-by-verb walkthrough — this doc) |
| R3 | n/a — this doc | folded | runbook error coverage (each verb's Errors table) |
| R4 | [#461](https://github.com/by-openclaw/go-acp/issues/461) | open + comment | export/import round-trip + `--dry-run` + `--scope` + per-type tally — full spec in [`runbook/export-import.md`](runbook/export-import.md) |
| R5b | [#469](https://github.com/by-openclaw/go-acp/issues/469) | open | standalone `tree` verb + PlantUML |
| R6 | [#470](https://github.com/by-openclaw/go-acp/issues/470) | open | `info` reads DTD version from device (kills the "emberplus v1" line) |
| R7 | n/a — this doc | folded | runbook prose continued |
| R8 | [#471](https://github.com/by-openclaw/go-acp/issues/471) | open | service installer epic (per-OS) |
| R9 | [#472](https://github.com/by-openclaw/go-acp/issues/472) | open | provider stream idle-TTL eviction (no-keepalive → unsub streams) |
| R10 | [#478](https://github.com/by-openclaw/go-acp/issues/478) | **landed [#479](https://github.com/by-openclaw/go-acp/pull/479)** | `stream --id` CSV multi-subscribe |
| R11 | [#482](https://github.com/by-openclaw/go-acp/issues/482) | open | `invoke --format human` pretty-prints `getSalvo` result |
| R12 | [#473](https://github.com/by-openclaw/go-acp/issues/473) | open | `validate --lua` via tshark — see [`runbook/validate.md`](runbook/validate.md) |
| R13 | [#474](https://github.com/by-openclaw/go-acp/issues/474) | open | `bench` RFC 2544 + cold-start + recovery; expanded scope: glow / function / stream / transport per-op-kind profiles |
| R14 | [#475](https://github.com/by-openclaw/go-acp/issues/475) | open | `--ensure {present\|absent\|dryrun}` (Ansible) |
| R15 | [#476](https://github.com/by-openclaw/go-acp/issues/476) | open | `-v` ladder + `--log-format {text\|json\|loki}` + `--log-only` |
| R16 | [#483](https://github.com/by-openclaw/go-acp/issues/483) | open | `set` range / step round + enum-by-label client-side validation |
| R17 | folded into R8 | pending | per-OS firewall rules + runas-admin |
| R18 | [#477](https://github.com/by-openclaw/go-acp/issues/477) | open | bidirectional mDNS on `_ember._tcp.local.` (consumer + provider) |
| R19 | [#484](https://github.com/by-openclaw/go-acp/issues/484) | open | audit pass — consumer ↔ provider parity per use case + error-code surface |
| R20 | [#485](https://github.com/by-openclaw/go-acp/issues/485) | open | per-protocol use-case matrix at `docs/protocols/use-cases/<proto>.md` |
| R21 | [#486](https://github.com/by-openclaw/go-acp/issues/486) | open | `--path` accepts numeric OID alongside dotted label (per memory `project_path_by_id`) |
| R22 | [#487](https://github.com/by-openclaw/go-acp/issues/487) | open | `profile` enhancements: per-event-kind + JSON + `--since` + `--by-session` + `--show-events` |
| R23 | [#488](https://github.com/by-openclaw/go-acp/issues/488) | open | `validate --report <path.md\|path.json>` structured report |
| R24 | [#489](https://github.com/by-openclaw/go-acp/issues/489) | open | provider session inventory — local-socket CLI + minimal HTML5 admin page on separate port |
| R25 | [#490](https://github.com/by-openclaw/go-acp/issues/490) | open | provider runtime admin verbs — hot-reload toggles via local socket |

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
