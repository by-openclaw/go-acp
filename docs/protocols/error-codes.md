# Error codes — canonical list

Every error returned by dhs follows the contract:

- **Exit code**: `0` success · `1` runtime / wire / protocol error · `2` usage / validation / state error. Standard Unix; never `3+`. Cross-OS uniform (PowerShell `$LASTEXITCODE`, Bash `$?`, `cmd.exe %ERRORLEVEL%` all parse the same).
- **Stderr message**: `<layer>:<code>: <human message>` — stable, grep-able, identical on Linux / macOS / Windows.

Implementation: `internal/errcode/` defines `Code` + `Layer` + `Class`. Each layer declares typed sentinels (e.g. `transport.ErrRefused`). Call sites wrap with `fmt.Errorf("%w: %s", ErrCode, dynamic)`. Callers dispatch with `errors.Is(err, transport.ErrRefused)` or `errcode.From(err)`. The CLI binary exits via `os.Exit(errcode.Exit(err))`.

Memory: `feedback_error_contract_cross_os` locks the rule across every connector.

---

## Layers

| Layer | Owned by | Exit class |
|---|---|---|
| `transport` | `internal/transport/` + Go `net` package | 1 |
| `s101` | `internal/emberplus/codec/s101/` | 1 |
| `glow` | `internal/emberplus/codec/glow/` | 1 |
| `ber` | `internal/emberplus/codec/ber/` | 1 |
| `matrix` | `internal/emberplus/codec/matrix/` | 1 |
| `emberplus` | `internal/emberplus/{consumer,provider}/` | 1 |
| `acp1` | `internal/acp1/` (future) | 1 |
| `acp2` | `internal/acp2/` (future) | 1 |
| `probel` | `internal/probel-*/` (future) | 1 |
| `validation` | `internal/consumer/` validation layer | 2 |
| `plugin` | `internal/<proto>/` plugin-state checks | 2 |
| `session` | session-state layer | 1 |

---

## Codes

> **Migration status**: this file is the canonical list. R1a (this PR) ships the `internal/errcode/` infrastructure only. Subsequent PRs (R1b, R1c, ...) migrate each layer's existing free-text errors to typed `*errcode.Code` sentinels. Codes listed below with `status: pending` are planned but not yet defined in code; codes with `status: defined` have a corresponding `Err<Code>` var in their layer's package.

### transport

| Code | Status | When | Anchor |
|---|---|---|---|
| `transport:refused` | pending (R1b) | TCP dial returns ECONNREFUSED | OS / Go `net.OpError` |
| `transport:timeout` | pending (R1b) | dial / read / write deadline elapsed | OS / `net.Error.Timeout()` |
| `transport:reset` | pending (R1b) | TCP RST mid-session | OS / ECONNRESET |

### s101

| Code | Status | When | Anchor |
|---|---|---|---|
| `s101:crc-mismatch` | pending (R1c) | CRC-16/CCITT verify failed on frame inner bytes | Ember+ Doc §S101 Framing p.94 |
| `s101:bad-escape` | pending (R1c) | escape-stuffing rule violated (`0xFD 0xDE` expected) | spec p.94 |
| `s101:unknown-message-type` | pending (R1c) | S101 header carries an unrecognized type byte | spec p.94 |
| `s101:multi-frame-truncated` | pending (R1c) | MPM flag set but stream ended mid-message | spec p.94 |

### glow / ber

| Code | Status | When | Anchor |
|---|---|---|---|
| `glow:bad-tag` | pending (R1d) | BER tag parse failure | ITU-T X.690 §8.1 |
| `glow:bad-length` | pending (R1d) | BER length encoding invalid | X.690 §8.1.3 |
| `glow:bad-real` | pending (R1d) | BER REAL decode failed | X.690 §8.5 + memory `reference_emberplus_ber_real` |
| `glow:unknown-application-tag` | pending (R1d) | APPLICATION tag not in the GlowDTD enum | Ember+ Doc §p.84-91 |
| `ber:bad-relative-oid` | pending (R1d) | RELATIVE-OID decode failed | X.690 §8.20 |

### matrix

| Code | Status | When | Anchor |
|---|---|---|---|
| `matrix:cardinality-exceeded` | pending (R1e) | matrix type (oneToOne / oneToN) limit violated | Ember+ Doc §p.33 |
| `matrix:target-locked` | pending (R1e) | target's `ConnectionDisposition=Locked(3)` | Ember+ Doc §p.89; site already exists in `internal/emberplus/codec/matrix/state.go` |
| `matrix:max-connects-per-target` | pending (R1e) | nToN per-target capacity exceeded | Ember+ Doc §p.88 |
| `matrix:max-total-connects` | pending (R1e) | nToN total-connections capacity exceeded | Ember+ Doc §p.88 |

### emberplus

| Code | Status | When | Anchor |
|---|---|---|---|
| `emberplus:invocation-failed` | pending (R1f) | `InvocationResult.Success=false`, no description | Ember+ Doc §p.92 |
| `emberplus:invocation-failed-with-description` | pending (R1f) | `Success=false` + provider populated `description` | Ember+ Doc §p.92 |

### validation

| Code | Status | When | Anchor |
|---|---|---|---|
| `validation:invalid-integer` | partial (today emits free-text via `*consumer.ValidationError`; R1g formalizes) | `--value` not parseable as integer | dhs PR #453 |
| `validation:invalid-real` | partial | `--value` not parseable as real | dhs PR #453 |
| `validation:invalid-enum-index` | partial | `--value` outside enum range | dhs PR #453 |
| `validation:invalid-enum-label` | pending (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | `--value` is label not in `enumMap` | Ember+ Doc §p.86 |
| `validation:out-of-range-low` | pending (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value below Parameter `minimum` | Ember+ Doc §p.86 |
| `validation:out-of-range-high` | pending (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value above Parameter `maximum` | Ember+ Doc §p.86 |
| `validation:step-misaligned` | pending (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value not on Parameter `step` grid | Ember+ Doc §p.86 |
| `validation:invalid-oid` | pending (R21 [#486](https://github.com/by-openclaw/go-acp/issues/486)) | `--path` matches OID regex but bad syntax (`1..2`) | dhs |
| `validation:invalid-format` | pending (R11 [#482](https://github.com/by-openclaw/go-acp/issues/482) · R22 [#487](https://github.com/by-openclaw/go-acp/issues/487)) | `--format` value not in supported set | dhs |
| `validation:invalid-duration` | pending (R22 [#487](https://github.com/by-openclaw/go-acp/issues/487)) | `--since` not a Go duration | dhs |
| `validation:invalid-id-token` | pending (R1g formalizes existing `--id` CSV errors from R10) | bad / empty token in CSV `--id` | dhs PR #479 (R10) |
| `validation:invalid-direction` | pending (R1g) | `--direction` not `in` / `out` | dhs |
| `validation:device-identifier-mismatch` | pending (R4 [#461](https://github.com/by-openclaw/go-acp/issues/461)) | import file's `device.identifier` ≠ target's | ADR-0022 |
| `validation:admin-port-collides-with-wire` | pending (R24 [#489](https://github.com/by-openclaw/go-acp/issues/489)) | `--admin-addr` resolves to same `host:port` as wire `--port` | memory `feedback_admin_web_minimal` |
| `validation:admin-feature-unknown` | pending (R25 [#490](https://github.com/by-openclaw/go-acp/issues/490)) | unknown admin feature name | dhs |
| `validation:admin-action-invalid` | pending (R25) | admin action invalid for the named feature | dhs |
| `validation:admin-restart-required` | pending (R25) | admin feature can only change at restart | dhs |
| `validation:invalid-log-level` | pending (R25 + R15 [#476](https://github.com/by-openclaw/go-acp/issues/476)) | log-level value not in `info\|debug\|trace`... | dhs |

### plugin

| Code | Status | When | Anchor |
|---|---|---|---|
| `plugin:not-implemented` | partial (today: `ErrNotImplemented`) | plugin stub for unsupported operation | dhs `internal/consumer/errors.go:11` |
| `plugin:not-connected` | partial (today: `ErrNotConnected`) | call requires a live transport and none established | dhs `internal/consumer/errors.go:15` |
| `plugin:not-walked` | pending (R1g) | call requires walked tree state | dhs `internal/emberplus/consumer/plugin.go::ensureWalked` |
| `plugin:object-not-found` | partial (today: `ErrObjectNotFound`) | tree miss on path / OID | dhs `internal/consumer/value_validator.go:34` |
| `plugin:wrong-kind` | pending (R1g) | resolved object isn't the kind the verb needs (e.g. `set` on Node) | dhs |
| `plugin:by-session-unavailable` | pending (R22 [#487](https://github.com/by-openclaw/go-acp/issues/487)) | `profile --by-session` and no R24 admin endpoint reachable | dhs |
| `plugin:admin-socket-not-found` | pending (R25 [#490](https://github.com/by-openclaw/go-acp/issues/490)) | local admin socket not found (producer not running) | dhs |

### session

| Code | Status | When | Anchor |
|---|---|---|---|
| `session:write-timeout` | partial (today: `ErrWriteTimeout`) | write transmitted, no confirm within window | dhs `internal/consumer/errors.go:24` |
| `session:write-coerced` | partial (today: `ErrWriteCoerced`) | provider echoed a different value (clamp/round) | dhs `internal/consumer/errors.go:30` |
| `session:write-rejected` | partial (today: `ErrWriteRejected`) | provider's echo refuses the write | dhs `internal/consumer/errors.go:35` |
| `session:dead` | pending (R1g) | session liveness layer reports dead | memory `project_session_health` |

---

## Cross-OS dispatch examples

### Bash

```bash
err=$(dhs consumer emberplus set ... 2>&1)
case "$err" in
  transport:*)  echo "retry-able transport problem" ;;
  validation:*) echo "fix input" ;;
  matrix:*)     echo "protocol-level rejection" ;;
  emberplus:invocation-failed*) echo "Invoke returned Success=false" ;;
esac
echo "exit: $?"
```

### PowerShell

```powershell
$err = (dhs consumer emberplus set ... 2>&1)
switch -Regex ($err) {
    '^transport:'  { 'retry-able transport problem' }
    '^validation:' { 'fix input' }
    '^matrix:'     { 'protocol-level rejection' }
    '^emberplus:invocation-failed' { 'Invoke returned Success=false' }
}
"exit: $LASTEXITCODE"
```

### Ansible task

```yaml
- name: gain set
  command: dhs consumer emberplus set --path ... --value -25
  register: r
  failed_when:
    - r.rc != 0
    - r.stderr is not search('^validation:invalid-enum-label')   # tolerate one specific code
```

---

## CI gate (post R1d)

A linter check runs on `main` push:

1. Every `errcode.New(...)` invocation in the codebase has a corresponding `### <layer>` row in this file
2. Every code listed in this file has a corresponding `Err<Code>` var in its layer's package
3. Drift (orphaned codes or missing docs) fails the lint step

Pattern: `make lint-error-codes` (alias for the CI step).

---

## Refs

- R1 [#468](https://github.com/by-openclaw/go-acp/issues/468) — the parent epic
- Memory `feedback_error_contract_cross_os` — locked contract
- Memory `reference_emberplus_ber_real` — BER REAL convention anchor (referenced by `glow:bad-real`)
- Memory `feedback_admin_web_minimal` — admin-port collision rule (referenced by `validation:admin-port-collides-with-wire`)
- [`internal/emberplus/docs/runbook.md`](../../internal/emberplus/docs/runbook.md) — operator-facing summary
