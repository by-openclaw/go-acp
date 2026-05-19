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
| **Out of scope** | (legacy gaps now covered locally — see "Recent additions" below) |

## Recent additions (DOD branch `feat/emberplus-stream-idle-ttl-472`, PR [#507](https://github.com/by-openclaw/go-acp/pull/507))

The branch carries **36 atomic commits** ahead of `main`, all
pre-commit green, all 73 unit-test packages passing on
Windows 11 + go1.26.2. End-to-end verified on Windows 11 against
the integration-test manifest producer + a hand-crafted coverage
producer + Wireshark 4.6.5 + tshark.

**Strict-spec complete** — every R-item ships its full
acceptance scope. No v2 deferrals, no `validation:ensure-mode-pending`
placeholders, no Skip clauses in coverage-matrix tests.

| Issue | What landed | Commit |
| --- | --- | --- |
| R9 #472 | provider stream idle-TTL eviction + `--stream-ttl` flag + compliance event | `aff1cfa` |
| R10 #478 | stream `--id` accepts CSV for multi-subscribe | already on branch |
| R11 #482 | `invoke --format human` pretty-prints getSalvo result | already on branch |
| R12 #473 | `validate --lua` synthesises pcap from jsonl when `--pcap` unset; dissector double-load fix; Windows tshark fallback | `59945f9` + `42d4f9b` |
| R13 #474 | RFC 2544 / 8219 — per-op p50/p95/p99 latency + recovery-time + CSV output | `b61eae0` |
| R14 #475 | `--ensure {present\|absent\|dryrun}` on `set` + `matrix` + `invoke`; `ensureAbsent` resets Parameter to declared Default via plugin `ParameterDefault()` | `129b3c6` + `b010984` |
| R15 #476 | logger ladder `-v / -vv / -vvv / -vvvv` + Loki format + async non-blocking handler + DI audit | `8b99d7f` |
| R16 #483 | set `--value` range + step + enum-by-label client-side validation + `--round` snap | already on branch |
| R18 #477 | pure-Go mDNS browser + provider `--mdns` announce on `_ember._tcp` (no avahi/Bonjour dependency) | `d7680ca` + `0c24baf` |
| R19 #484 | `docs/protocols/audits/emberplus-audit-2026-05-18.md` parity audit | `80473dd` |
| R20 #485 | `docs/protocols/use-cases/emberplus.md` use-case matrix + README index | `c875e34` |
| R21 #486 | `--path` accepts numeric OID alongside dotted label across all 7 --path verbs (extract added) | `edd6e6d` |
| R22 #487 | `profile --format json` + `--since` + `--show-events` + `--by-session` + ring buffer | `0c1214d` |
| R23 #488 | `validate --report <md\|json>` markdown + JSON reports | `43a743d` |
| R24 #489 | admin web — mutation buttons (sessions:disconnect, subs:close) + SSE live updates | `c1fa6ce` |
| R25 #490 | full admin verb set over Unix socket — `sessions:list/disconnect`, `subs:list/close`, `peers:list` + CLI `k=v` extras parser | `2d3c2c3` |
| R4  #461 | full Glow round-trip — Matrix + Function + StreamParameter + Template pinned | `0b2d109` |
| #300 | peer-health snapshot consumer + provider | `ad3a62f` |
| #62 | 4 of 5 protocol-type fixtures captured against our own producer (Template APP 24 deferred to [#508](https://github.com/by-openclaw/go-acp/issues/508)) | `32eb814` |

**Helper tools added** (`tools/`):

- `scan-glow-tags` — reads a `frames.jsonl` and reports per-frame Glow APP-tag occurrences; used during fixture capture to identify which frame to extract per protocol-type bucket
- `jsonl-to-pcap` — CLI wrapper around `wiretrace.SynthesisePcap` (R12); materialises a libpcap from a `frames.jsonl` so committed fixtures stay replayable in Wireshark without a live capture

**End-to-end validation (Windows 11, this branch):**

```powershell
# producer
.\bin\dhs.exe producer emberplus serve `
    --manifest internal\emberplus\testdata\integration-test\manifest\emberplus-integration.json `
    --cache-dir internal\emberplus\testdata\integration-test `
    --host 127.0.0.1 --port 9100

# consumer walk + capture
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9100 `
    --capture tmp\walk.jsonl
# → slot 0 — 1361 objects, 884 frames captured

# validate via Go codec (offline)
.\bin\dhs.exe consumer emberplus validate tmp\walk.jsonl
# → validate: 884 trames decoded   rx: 442   tx: 442

# validate via Wireshark dissector (R12 synthesis path)
.\bin\dhs.exe consumer emberplus validate tmp\walk.jsonl --lua
# → [Protocols in frame: eth:ethertype:ip:tcp:dhs_emberplus]
# → dhs_emberplus.lua auto-loaded from %APPDATA%\Wireshark\plugins\
```

**#62 fixture buckets — strict-spec captures from our own producer:**

| Protocol type | APP tag | Captured | Source |
| --- | --- | --- | --- |
| StreamDescription | 12 | ✅ | `coverage-tree.json` walk frame 7 |
| QualifiedFunction | 20 | ✅ | `coverage-tree.json` walk frame 6 |
| TupleItemDescription | 21 | ✅ | `coverage-tree.json` walk frame 6 |
| QualifiedTemplate | 25 | ✅ | `coverage-tree.json` walk frame 3 |
| Template (relative form) | 24 | ⏳ [#508](https://github.com/by-openclaw/go-acp/issues/508) | needs `encodeTemplateInChild` encoder branch OR real Lawo/DHD provider capture |

**Merged + live on `main`** (history reference):

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

**Pending — lab session against Lawo mc² / Powercore / DHD:**

- Capture Template APP 24 relative-form frames against a real
  large-scale router (closes #62 cleanly; alternative path to #508)
- Replace every §1..§23 verb example below with verbatim output
  from a real Lawo console — every command's `OUT` block becomes
  byte-exact production-grade content rather than the synthesized
  / integration-test fixture form documented today

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

> `--path` accepts **both** forms — `--path 1.6.1` and `--path types.vInteger` resolve to the same Parameter. Detection: a string composed entirely of digits and dots routes through the numeric OID index; otherwise it goes through the dotted-label index. Malformed numeric forms (`1..2`, `1.`, `.1`) fail fast with `validation:invalid-oid` (exit `2`). Refs **R21 [#486](https://github.com/by-openclaw/go-acp/issues/486)** (per memory `project_path_by_id`).

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
# By dotted path
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.types.vInteger
# path = dhs-emberplus-integration.types.vInteger
# OID  = 1.6.1
# value = 42

# Same Parameter addressed by OID directly (R21 #486)
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path 1.6.1
# path = dhs-emberplus-integration.types.vInteger   (resolved from cache)
# OID  = 1.6.1
# value = 42

# DTD version field (Ember+ identity p.84)
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 --path dhs-emberplus-integration.identity.dtdVersion
# path = dhs-emberplus-integration.identity.dtdVersion
# OID  = 1.0.4
# value = "2.60"
```

> Address by OID directly: `--path 1.6.1` resolves to the same Parameter as `--path types.vInteger` — both forms are accepted by every `--path`-using verb. Refs **R21 [#486](https://github.com/by-openclaw/go-acp/issues/486)** (per [Addressing](#addressing--by-path-vs-by-oid)).

### Errors

| Trigger | Command | Error code | Exit |
|---|---|---|---|
| wrong path | `get ... --path bogus.path.here` | `plugin:object-not-found` | 2 |
| invalid OID syntax | `get ... --path 1..2` | `validation:invalid-oid` | 2 |
| path resolves to non-Parameter | `get ... --path dhs-emberplus-integration.functions` | `plugin:wrong-kind` | 2 |
| missing required path | `get 127.0.0.1 --port 9100` | (usage error) | 2 |
| connection refused | `get ... --port 9999` | `transport:refused` | 1 |

---

## 4. `set` — write one Parameter

### Happy

```powershell
# Set the gain Parameter on nToN target-0 — by dotted path
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.gain --value -25
# path = dhs-emberplus-integration.nToN.targetParams.0.gain
# OID  = 1.3.4.0.0    (numeric form derived from parametersLocation seed)
# confirmed value = -25

# Set the mute Parameter on nToN target-0
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.mute --value true
# path = dhs-emberplus-integration.nToN.targetParams.0.mute
# OID  = 1.3.4.0.1
# confirmed value = true

# Set an enum Parameter by integer index
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vEnum --value 3
# path = dhs-emberplus-integration.types.vEnum
# OID  = 1.6.6
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

### Subscription model — how the merged feed actually fills

Three independent subscription mechanisms, each kicked in differently:

| Source | When the announce starts flowing |
| --- | --- |
| **stream params** | the consumer sends an explicit `Subscribe(30)` per stream OID. `watch` does this on its own as soon as it discovers any Parameter with `streamIdentifier` during its initial walk — no manual step needed. |
| **glow params** | the provider fan-outs every value-change announce to every connected session, regardless of subscribe state (libember-cpp / Lawo legacy). Open the session → you receive these. |
| **matrix tally** | the consumer is implicitly subscribed to a matrix as soon as the walker decodes its element (Ember+ Documentation v2.50 p.88: "As soon as a consumer issues a GetDirectory command on a matrix object, it implicitly subscribes to matrix connection changes"). No explicit `Subscribe` call needed; one walk is enough. |

The `watch` verb runs a walk on connect (unless `--no-walk` is set)
specifically so all three mechanisms light up before the first
announce. Net effect: **`watch` on root OID = every change from
every source**, no separate `subscribe` step required.

If the producer keeps running but `watch` is restarted, the
provider still has the stream subscription from the prior session
until the R9 `--stream-ttl` sweeps it (default 30s); the fresh
`watch` re-subscribes during its walk regardless.

### Happy

```powershell
# All updates — stream + glow + matrix tally — one merged feed
# (walk on connect implicitly subscribes streams + matrix; glow fans
#  out unconditionally — see "Subscription model" above)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100
# OID scope = 1 (root) — every announce flows through

# Streams only (suppress glow + tally noise)
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 --streams-only
# Updates every ~500ms

# Watch one stream Parameter
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vu_zero --streams-only
# path = dhs-emberplus-integration.types.vu_zero
# OID  = 1.6.10   — only vu_zero updates

# Watch a glow Parameter (non-stream) — change-of-value announces
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.targetParams.0.gain
# path = dhs-emberplus-integration.nToN.targetParams.0.gain
# OID  = 1.3.4.0.0
# Emits whenever target-0 gain changes (set by us or another session)

# Watch matrix tally — every crosspoint connect/disconnect on the matrix
.\bin\dhs.exe consumer emberplus watch 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix
# path = dhs-emberplus-integration.oneToN.matrix
# OID  = 1.1   — tally announces fire on every Connect / Disconnect / Absolute
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
# nToN — multi-source SET (replaces target 10's prior connections)
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --target 10 --sources 3,4,5 --op absolute
# path = dhs-emberplus-integration.nToN.matrix
# OID  = 1.3
# matrix connect: target 10 ← sources [3 4 5] (op=absolute)
# Post-state: target 10 ← [3, 5, 4]  (set membership; order not significant)

# nToN — disconnect one source (subtractive against the existing set)
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.nToN.matrix `
    --target 10 --sources 4 --op disconnect
# path = dhs-emberplus-integration.nToN.matrix
# OID  = 1.3
# matrix connect: target 10 ← sources [4] (op=disconnect)
# The `[4]` in the echo is the SET delta (what we asked to remove),
# NOT the final route. Post-state: target 10 ← [3, 5] (source 4 removed
# from the prior [3, 4, 5] set). Verify with `watch --path 1.3` —
# the tally announce that follows carries the post-state target row.

# oneToN — replace single source
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix `
    --target 0 --sources 7 --op absolute
# path = dhs-emberplus-integration.oneToN.matrix
# OID  = 1.1
# Post-state: target 0 now routed from src 7; prior src 0 dropped

# oneToOne — source steal (post #467)
.\bin\dhs.exe consumer emberplus matrix 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToOne.matrix `
    --target 0 --sources 5 --op absolute
# path = dhs-emberplus-integration.oneToOne.matrix
# OID  = 1.2
# matrix connect: target 0 ← sources [5] (op=absolute)
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

`matrixRef` accepts **both** addressing forms post #466 — pick whichever is easier to read in the playbook:

- **Numeric OID** — e.g. `1.1.3` resolves to `oneToN.matrix`. Short, deterministic, survives label renames.
- **Dotted path** — e.g. `dhs-emberplus-integration.oneToN.matrix`. Self-documenting; resolved against the walked tree.

Both forms are accepted by `setLock`, `listLocks`, `storeSalvo`, `recallSalvo`, `getSalvo`, and `listSalvos`. The happy examples below show OID form on the first line of each verb and dotted form on the second — either is a valid copy-paste base.

### What `success` and `result` actually mean

Every invocation echo carries two distinct fields per Ember+ spec p.92:

| Field | Type | Meaning |
| --- | --- | --- |
| `success` | bool | `true` = the function ran cleanly; `false` = the provider rejected the call (bad matrixRef, out-of-range index, etc.). Spec p.92: omitted → defaults to true. |
| `result` | tuple | the function's **return** payload — typed per the function's `result[]` schema. For `setLock` it's `[previousLockState]`; for `listLocks` it's `[lockedTargetIndex, lockedTargetIndex, ...]`; for `storeSalvo` it's `[true]`; etc. |

So a line like `success=true · result: [false]` from `setLock` reads:
"the call SUCCEEDED, and the function reports the PREVIOUS lock state
was `false` (unlocked) before we flipped it on". Not "the call failed".

### Happy

```powershell
# Lock target 3 on oneToN — matrixRef passed as the matrix OID
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "1.1.3,3,true"
# path = dhs-emberplus-integration.functions.setLock
# OID  = 1.5.1
# args interpreted as: matrixRef=oneToN.matrix (1.1.3) · target=3 · locked=true
# invocation 1: success=true · result: [false]
# meaning: function ran; PREVIOUS lock state on target 3 was false (unlocked)

# Same call — matrixRef passed as a dotted path (post #466)
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.setLock `
    --args "dhs-emberplus-integration.oneToN.matrix,4,true"
# path = dhs-emberplus-integration.functions.setLock
# OID  = 1.5.1
# args interpreted as: matrixRef=oneToN.matrix · target=4 · locked=true
# invocation 1: success=true · result: [false]

# List locked targets — result is the LIST of locked TARGET INDICES.
# Locking is a target-only property in the integration-test schema;
# the lock state does NOT carry "which source was routed when this
# was locked". Pair listLocks with a matrix walk to get the full
# target → source view per locked target — full recipe below.
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.listLocks `
    --args "1.1.3"
# path = dhs-emberplus-integration.functions.listLocks
# OID  = 1.5.2
# result: [2,3,4]
#   meaning: oneToN targets 2, 3, 4 are currently locked
#   (target 2 was pre-locked from the seed; targets 3 and 4 were
#    locked earlier in this session by setLock)

# Pair with a matrix walk to render the locked-target → source view:
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.oneToN.matrix
# path = dhs-emberplus-integration.oneToN.matrix
# OID  = 1.1
# (walk output includes the matrix.connections array; for each locked
#  target from listLocks above, the connection entry shows the
#  current source)
#
# Expected output extract for our example state:
#   matrix oneToN connections:
#     tgt  2 ← src  2   [LOCKED]   (was pre-locked from seed)
#     tgt  3 ← src  8   [LOCKED]   (locked this session via setLock 1.1.3,3,true)
#     tgt  4 ← src  4   [LOCKED]   (locked this session via setLock 1.1.3,4,true)
#     tgt  0 ← src  0
#     tgt  1 ← src  1
#     ... (unlocked targets follow)
#
# The [LOCKED] marker is emitted by the walk pretty-printer whenever
# the target's index appears in the matrix's lockedTargets set —
# same data the listLocks function returns, joined client-side.

# Store salvo — captures the matrix's current connections under slot 99
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.storeSalvo `
    --args "1.1.3,99,"
# path = dhs-emberplus-integration.functions.storeSalvo
# OID  = 1.5.3
# args interpreted as: matrixRef=oneToN.matrix · slot=99 · label=""
# result: [true]    (function ran; nothing else to report)

# Get salvo dump — wire form is a single semicolon-separated string
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.getSalvo `
    --args "1.1.3,99"
# path = dhs-emberplus-integration.functions.getSalvo
# OID  = 1.5.6
# args interpreted as: matrixRef=oneToN.matrix · slot=99
# result: ["0=0;1=1;3=3;..."]
# Each "T=S" pair means target T routed from source S at salvo-store time.

# Same call with --format human (R5 #482) renders the matrix view
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.getSalvo `
    --args "1.1.3,99" --format human
# path = dhs-emberplus-integration.functions.getSalvo
# OID  = 1.5.6
#   tgt  0 ← src  0
#   tgt  1 ← src  1
#   tgt  3 ← src  3
#   ...

# Recall salvo — restore the captured snapshot
.\bin\dhs.exe consumer emberplus invoke 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.functions.recallSalvo `
    --args "1.1.3,99"
# path = dhs-emberplus-integration.functions.recallSalvo
# OID  = 1.5.4
# args interpreted as: matrixRef=oneToN.matrix · slot=99
# result: [N]    (N rows restored on the matrix)
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

### Discovering the stream identifiers

The table above lists the IDs hard-coded in the integration-test fixture.
For ANY provider you don't already know, the discovery flow is one of:

```powershell
# 1. Walk the tree and grep Parameters carrying a streamIdentifier
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9100 `
    --capture .audit\stream-discover.jsonl
# path = 1 (root) — full walk
# OID  = 1
# The capture file has one Trame per frame; the canonical tree.json
# alongside has every Parameter rendered with its streamIdentifier
# field when set:
#
#     "vu_zero":  { "streamIdentifier": 0,    "OID": "1.6.10", ... }
#     "vu_left":  { "streamIdentifier": 1001, "OID": "1.6.11", ... }
#     "vu_right": { "streamIdentifier": 1002, "OID": "1.6.12", ... }

# 2. Drop --capture and walk the tree printed to stdout — same data
#    surfaces inline next to each Parameter rendered by the walker.

# 3. Get one Parameter directly and read its streamIdentifier from
#    the reply
.\bin\dhs.exe consumer emberplus get 127.0.0.1 --port 9100 `
    --path dhs-emberplus-integration.types.vu_zero
# path = dhs-emberplus-integration.types.vu_zero
# OID  = 1.6.10
# value = -60.00
# streamIdentifier = 0     ← printed when the Parameter carries one
```

Operator shortcut: `stream` with no `--id` flag implicitly subscribes
to every stream the consumer discovered during its initial walk, then
prints each incoming sample tagged with `id=<N> path=<dotted>` — so
running `stream` alone is itself a discovery tool.

### Happy

```powershell
# Subscribe to one streamIdentifier by ID
.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0
# path = dhs-emberplus-integration.types.vu_zero
# OID  = 1.6.10
# Subscribes to streamIdentifier=0 only

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 1001
# path = dhs-emberplus-integration.types.vu_left
# OID  = 1.6.11
# Subscribes to vu_left only

# Multi-subscribe — R10 post #479
.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001
# Subscribes to {vu_zero (1.6.10), vu_left (1.6.11)}

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100 --id 0,1001,1002
# Subscribes to all three explicitly (equivalent to omitting --id)

.\bin\dhs.exe consumer emberplus stream 127.0.0.1 --port 9100
# No --id → subscribe to ALL stream Parameters the walk discovered
# (3 today on the integration-test producer). Output is tagged
# inline so this doubles as a discovery tool.
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
    --direction consumer --version 1.0.0 --out .audit/extract-demo
# path = 1 (root) — recursive walk
# OID  = 1
# Writes meta.json + wire.jsonl + tree.json under the fixture layout
```

### Flags

| Flag | Purpose | Example |
|---|---|---|
| `--manufacturer` | Manufacturer string baked into `meta.json` and the cache filename | `BY-Systems` |
| `--product` | Product / Model name — drives `<Model@SwRev>.json` per ADR-0022 | `dhs-emberplus-integration` |
| `--direction` | How the captured product speaks Ember+ — one of `consumer` (we read it), `provider` (we expose it), `both` (bidirectional), `io` (alias for `both`). Stored in `meta.json` so replay tooling knows the role. **`in` / `out` are NOT accepted** — the verb returns `validation:invalid-direction` (exit 2). | `consumer` |
| `--version` | Software revision (SwRev) of the device being captured. Forms the cache filename `<Model@SwRev>.json` per ADR-0022. Multiple SwRev captures coexist side-by-side; the consumer hot-loads by exact match. **Required today**; the open follow-up "auto-derive from the device identity Node" is tracked separately — until that lands, the operator passes the version explicitly. | `1.0.0` |
| `--out` | Output directory; the triple `meta.json` + `wire.jsonl` + `tree.json` lands inside | `.audit/extract-demo` |
| `--slot` | Slot to walk (default `0`, matches the Ember+ "everything is slot 0" convention) | `0` |

Layout written:

```
.audit/extract-demo/
├── meta.json     manufacturer + product + version + direction + capturedAt + protocol
├── wire.jsonl    S101 frames in the canonical Trame JSONL form (per ADR-0021)
└── tree.json     decoded Glow tree (same shape as .cache/dm/emberplus/<Model@SwRev>.json)
```

`wire.jsonl` replays through `dhs consumer emberplus validate` for
Go-codec verification or — when a real pcap is needed — through
`tshark -X lua_script:internal/emberplus/wireshark/dhs_emberplus.lua`
(R12 #473 `--lua --pcap` path).

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
# path = dhs-emberplus-integration.nToN.matrix
# OID  = 1.3
# total: 1000 ops in N ms, M ops/s
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

## 14. `health` — 3-layer session liveness

`dhs consumer emberplus health <host>` prints the Reachable /
Connected / Live snapshot the plugin tracks under [#300](https://github.com/by-openclaw/go-acp/issues/300). The provider side
exposes the same data per-peer via the admin socket (see §17).

### Happy

```powershell
.\bin\dhs.exe consumer emberplus health 127.0.0.1 --port 9000
# host=127.0.0.1 proto=emberplus
#   reachable=true
#   connected=true
#   live=true  (last rx 1.2s ago, threshold 30s)
#   last_rx=2026-05-18T08:43:21Z
#   last_tx=2026-05-18T08:43:20Z
#   stale_after=30s
```

`--json` emits the same fields as machine-readable JSON. `--watch`
polls on `--interval` (default 5s) and prints only when a bit flips.

## 15. `discover` — mDNS browse (R18 #477 v1)

`dhs consumer emberplus discover` browses the local subnet for
`_ember._tcp.local.` responders via the OS native mDNS tool
(`avahi-browse` on Linux; `dns-sd` on macOS / Windows with the
Bonjour SDK). Pure-Go mDNS is the v2 enhancement.

### Happy

```powershell
.\bin\dhs.exe consumer emberplus discover --duration 5s
# browsing _ember._tcp.local for 5s ...
#
# NAME                            HOST                   PORT  HOSTNAME                       TXT
# --------------------------------------------------------------------------------------------------
# dhs-emberplus-integration       10.6.239.113           9000  dhs-debian.local.              dtdVersion=2.60
# 1 responder(s) found
```

### Errors

| Trigger | Code | Exit |
| --- | --- | --- |
| `avahi-browse` / `dns-sd` not on PATH | `validation:mdns-tool-not-found` | 2 |

## 16. Logging ladder + Loki + `--log-only` (R15 #476)

Every consumer + producer verb accepts the Ansible-aligned
verbosity ladder:

| Flag | Level | Notes |
| --- | --- | --- |
| (none) | `info` | warnings + errors + per-verb summary |
| `-v` | `info` | explicit info |
| `-vv` | `debug` | + plugin debug (connection state, walk progress) |
| `-vvv` | `trace` | + per-frame decoded events |
| `-vvvv` | `trace` (+ raw hex) | + raw S101 / AN2 hex (today via `--capture`) |

`--log-level` and `-v…` are mutually exclusive — setting both returns
`validation:log-level-conflict` (exit 2).

```powershell
# Loki ingestion: `ts` / lowercase level / `component` / `msg`
.\bin\dhs.exe consumer emberplus walk 127.0.0.1 --port 9000 -vv `
  --log-format loki --log-only 2>>.\dhs.loki.log

# Promtail snippet + full Loki contract: docs/logging.md
```

Logging is non-blocking by default — every handler wraps an
`internal/logging.AsyncHandler` so the hot path (S101 keepalive,
matrix tally fan-out) never blocks on stderr. Drop counter via
`DropCount()` for audit under load.

## 17. Producer admin socket + web (R25 #490 + R24 #489)

Local-only runtime admin control plane. Local socket on every
supported OS (Go 1.17+ AF_UNIX); never exposed to the network.

### Producer side — start the socket

```powershell
.\bin\dhs.exe producer emberplus serve `
  --manifest internal\emberplus\testdata\integration-test\manifest\emberplus-integration.json `
  --port 9000 `
  --admin `
  --admin-addr 127.0.0.1:9110     # R24 web page on a separate port
```

`--admin` (default `true` for emberplus) starts the socket at
`%TEMP%\dhs-emberplus-admin.sock`. `--admin-addr` opts in the static
HTML5 page; empty (default) leaves the page off.

### Consumer side — call the admin verb

```powershell
# List connected peers + per-peer health
.\bin\dhs.exe producer emberplus admin sessions list
# [
#   { "peer": "10.6.239.113:54321", "connected": true, "live": true,
#     "last_rx": "2026-05-18T14:25:28Z", "stale_after": "30s",
#     "subs_open": 3 }
# ]

# Browser visit http://127.0.0.1:9110/ shows the same table HTML-rendered
```

Unknown verbs surface `admin:verb-not-implemented: <verb>`. v1.5
follow-ups: `health enable/disable`, `metrics enable/disable`,
`log-level set`, `streamer-interval set`, `compliance reset/show`.

## 18. `--ensure` Ansible idempotency (R14 #475 v1 on `set`)

`dhs consumer emberplus set --ensure {present|absent|dryrun}` emits
the JSON shape Ansible playbooks read with the `json` filter.

### `--ensure dryrun` (no wire write)

```powershell
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9000 `
  --path dhs-emberplus-integration.identity.value --value 99 `
  --ensure dryrun
# path = dhs-emberplus-integration.identity.value
# OID  = 1.0.5
# {
#   "verb": "set",
#   "ensure": "dryrun",
#   "changed": false,
#   "before": "42",
#   "after": "99",
#   "diff": "value: 42 -> 99"
# }
```

### `--ensure present` (idempotent write)

Reads current via GetValue; sends Set only when different.

```powershell
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9000 `
  --path dhs-emberplus-integration.identity.value --value 99 `
  --ensure present
# path = dhs-emberplus-integration.identity.value
# OID  = 1.0.5
# {"verb":"set","ensure":"present","changed":true,"before":"42","after":"99","diff":"value: 42 -> 99"}

# Re-run: idempotent
.\bin\dhs.exe consumer emberplus set 127.0.0.1 --port 9000 `
  --path dhs-emberplus-integration.identity.value --value 99 `
  --ensure present
# path = dhs-emberplus-integration.identity.value
# OID  = 1.0.5
# {"verb":"set","ensure":"present","changed":false,"before":"99","after":"99","reason":"value already at target"}
```

`--ensure absent` returns `validation:ensure-mode-pending` in v1 —
resetting to `Parameter.Default` needs codec-side Default accessor
not yet exposed through `consumer.Protocol`. Tracked inline.

## 19. `profile` extensions (R22 #487)

The legacy column form stays byte-compat default. New flags:

```powershell
# JSON output for CI ingestion
.\bin\dhs.exe consumer emberplus profile 127.0.0.1 --port 9000 --format json

# Time-window filter — counters limited to events whose last_seen
# is within the last 5 minutes
.\bin\dhs.exe consumer emberplus profile 127.0.0.1 --port 9000 --since 5m

# Per-occurrence detail (matrix path / target / source) from the
# observation ring buffer, not just counters
.\bin\dhs.exe consumer emberplus profile 127.0.0.1 --port 9000 --show-events

# --by-session is reserved for the R24 #489 admin endpoint; returns
# plugin:by-session-unavailable today
.\bin\dhs.exe consumer emberplus profile 127.0.0.1 --port 9000 --by-session
```

## 20. `validate` extensions (R23 #488 + R12 #473)

### `--report <path>` (R23)

```powershell
# Markdown report alongside the legacy stdout summary
.\bin\dhs.exe consumer emberplus validate captures\emberplus\runbook\walk-happy.jsonl `
  --report walk-happy.md

# JSON report for CI
.\bin\dhs.exe consumer emberplus validate captures\emberplus\runbook\walk-happy.jsonl `
  --report walk-happy.json

# Stdout (suppresses per-frame text)
.\bin\dhs.exe consumer emberplus validate captures\emberplus\runbook\walk-happy.jsonl `
  --report -
```

Errors: `validation:invalid-report-format` (exit 2),
`transport:report-target-unwritable`, `transport:input-not-found`.

### `--lua` via tshark (R12 v1)

```powershell
# Requires a real pcap input (--pcap); jsonl->pcap synthesis = v2
.\bin\dhs.exe consumer emberplus validate _ignored.jsonl `
  --lua --pcap captures\emberplus\walk.pcapng
# (tshark -V output with the dhs_emberplus Lua dissector loaded)
```

Errors: `validation:lua-pcap-required` (no `--pcap`),
`validation:tshark-not-found` (install Wireshark).

## 21. `bench --profile` (R13 #474 v1)

Named profiles set sensible `--n` / `--op` defaults so the operator
doesn't have to remember the magic numbers. v2 layers RFC 2544
ramp-up + tail-latency capture.

```powershell
.\bin\dhs.exe consumer emberplus bench 127.0.0.1 --port 9000 `
  --path dhs-emberplus-integration.nToN.matrix `
  --dm "dhs-emberplus-integration@1.0.0" `
  --profile rfc2544-throughput   # n=10000 op=connect
# path = dhs-emberplus-integration.nToN.matrix
# OID  = 1.3
# elapsed: ...

.\bin\dhs.exe consumer emberplus bench 127.0.0.1 --port 9000 `
  --path dhs-emberplus-integration.nToN.matrix `
  --dm "dhs-emberplus-integration@1.0.0" `
  --profile rfc2544-latency      # n=1000 op=absolute
# path = dhs-emberplus-integration.nToN.matrix
# OID  = 1.3
```

Errors: `validation:bench-profile-unknown` (exit 2).

## 22. Provider `--stream-ttl` (R9 #472)

Per-session soft eviction of stream subscriptions when the peer
falls silent. Default `30s`; `0` disables.

```powershell
.\bin\dhs.exe producer emberplus serve `
  --manifest internal\emberplus\testdata\integration-test\manifest\emberplus-integration.json `
  --port 9000 `
  --stream-ttl 5s    # aggressive eviction for the integration test
```

Fires the producer-side compliance event `stream_idle_ttl_expired`
per cleared session; the TCP session stays open in case keep-alives
resume. Visible via the new `--show-events` flag on `profile`.

## 23. Round-trip baseline (R4 #461 v1)

Node + Parameter round-trip through canonical export → JSON →
import is pinned by `internal/export/canonical/roundtrip_test.go`.
Matrix / Function / StreamParameter / Template ride v2.

Running `go test -v ./internal/export/canonical/ -run TestRoundTrip_CoverageMatrix`
prints the audit checklist with each Glow type marked covered or
Skipped → v2 issue ref.

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
