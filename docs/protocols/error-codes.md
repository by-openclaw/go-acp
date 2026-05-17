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
| `transport:refused` | defined (R1b) | TCP dial returns ECONNREFUSED — `classifyDialError` detects via `errors.Is(err, syscall.ECONNREFUSED)` | OS / Go `net.OpError` |
| `transport:timeout` | defined (R1b) | dial / read / write deadline elapsed | OS / `net.Error.Timeout()` |
| `transport:dial-failed` | defined (R1b) | dial failed with an unrecognised error mode (fallback when not refused / not timeout) | OS |
| `transport:listen-failed` | defined (R1b) | bind/listen on a UDP port failed | OS |
| `transport:nil-conn` | defined (R1b) | method called on nil `*TCPConn` / `*UDPConn` / `*UDPListener` — programming error | dhs |
| `transport:payload-too-large` | defined (R1b) | send payload exceeded MLEN cap (4 GiB on TCP) | dhs |
| `transport:set-deadline-failed` | defined (R1b) | `SetReadDeadline` / `SetWriteDeadline` syscall failed | OS |
| `transport:write-failed` | defined (R1b) | TCP/UDP write returned an OS error mid-send | OS |
| `transport:short-write` | defined (R1b) | UDP wrote fewer bytes than the payload — datagram truncated | OS |
| `transport:read-failed` | defined (R1b) | TCP/UDP read returned an OS error mid-receive (not a deadline; deadlines map to `context.DeadlineExceeded` for compatibility) | OS |
| `transport:oversized-datagram` | defined (R1b) | received UDP datagram > caller-supplied `maxSize` | dhs |
| `transport:mlen-out-of-range` | defined (R1b) | TCP framer: MLEN < 8 or > caller-supplied `maxPayload` | dhs framing |
| `transport:wrong-conn-type` | defined (R1b) | `net.Dial` / `net.ListenPacket` returned a connection of unexpected concrete type | dhs |
| `transport:close-failed` | defined (R1b) | connection `Close()` returned an OS error | OS |
| `transport:capture-create-failed` | defined (R1b) | `os.Create` on the `--capture` file failed (perms / dir missing) | OS |
| `transport:reset` | pending (future) | TCP RST mid-session — would need read/write site detection via `errors.Is(err, syscall.ECONNRESET)`; currently surfaces as `transport:read-failed` or `transport:write-failed` | OS / ECONNRESET |

### s101

| Code | Status | When | Anchor |
|---|---|---|---|
| `s101:bad-frame` | defined (R1c) | frame missing BOF or EOF marker | Ember+ Doc §S101 Framing p.94 |
| `s101:crc-mismatch` | defined (R1c) | CRC-16/CCITT verify failed on unescaped inner bytes | spec p.94 |
| `s101:truncated` | defined (R1c) | frame ends mid-header or mid-content (insufficient bytes for declared shape) | spec p.94 |
| `s101:frame-too-large` | defined (R1c) | running buffer grew past 64 KiB safety cap before EOF — defends against unbounded pre-frame garbage on a misbehaving peer | dhs framing safety |
| `s101:read-failed` | defined (R1c) | underlying `io.Reader` returned an error during frame scan (BOF search or content collection); EOF passes through unwrapped for stream-close semantics | OS |
| `s101:write-failed` | defined (R1c) | underlying `io.Writer` returned an error during `WriteFrame` | OS |
| `s101:bad-escape` | pending (future) | escape-stuffing rule violated (`0xFD 0xDE` expected) — currently surfaces as `s101:crc-mismatch` because escape errors cascade into CRC failures | spec p.94 |
| `s101:unknown-message-type` | pending (future) | S101 header carries an unrecognized type byte — currently surfaces as `s101:bad-frame` | spec p.94 |
| `s101:multi-frame-truncated` | pending (future) | MPM flag set but stream ended mid-message — currently surfaces as `s101:truncated` | spec p.94 |

### glow / ber

| Code | Status | When | Anchor |
|---|---|---|---|
| `glow:decode-failed` | defined (R1d) | `DecodeRoot` top-level wrap; preserves the underlying `ber:*` cause in the `errors.Is` chain via dual-`%w` so callers can dispatch on either layer | dhs |
| `ber:truncated` | defined (R1d) | input buffer ends mid-element (insufficient bytes for declared tag / length / content) | X.690 §8.1 |
| `ber:tag-too-long` | defined (R1d) | high-tag-number form exceeds 4-byte cap (X.690 §8.1.2.4 permits unbounded; dhs caps for safety) | X.690 §8.1.2.4 |
| `ber:length-too-long` | defined (R1d) | long-form length exceeds 4-byte cap (X.690 §8.1.3.5 permits unbounded; dhs caps for safety) | X.690 §8.1.3.5 |
| `ber:invalid-real` | defined (R1d) | REAL bytes fail to decode per X.690 §8.5 + the Ember+ ecosystem normalised-fraction convention | X.690 §8.5 + memory `reference_emberplus_ber_real` |
| `ber:integer-overflow` | defined (R1d) | INTEGER content bytes encode a value outside int64 range | X.690 §8.3 |
| `glow:bad-tag` | pending (future) | unknown APPLICATION tag at glow-schema level — currently surfaces as `ber:tag-too-long` or cascades to `glow:decode-failed` | Ember+ Doc §p.84-91 |
| `glow:bad-length` | pending (future) | alias for `ber:length-too-long` in glow-schema context | X.690 §8.1.3 |
| `glow:bad-real` | pending (future) | alias for `ber:invalid-real` in glow-schema context | X.690 §8.5 |
| `glow:unknown-application-tag` | pending (future) | APPLICATION tag valid in BER but not in the GlowDTD enum (1..25) | Ember+ Doc §p.84-91 |
| `ber:bad-relative-oid` | pending (future) | RELATIVE-OID decode failed — no callsite emits this distinctly today | X.690 §8.20 |

### matrix

| Code | Status | When | Anchor |
|---|---|---|---|
| `matrix:target-locked` | defined (R1e) | target's `ConnectionDisposition=Locked(3)` blocks any state-mutating Connect/Disconnect | Ember+ Doc §p.89; site at `internal/emberplus/codec/matrix/state.go::CanConnect` |
| `matrix:cardinality-exceeded` | defined (R1e) | oneToN / oneToOne matrix: target would have >1 source | Ember+ Doc §p.33 |
| `matrix:max-connects-per-target` | defined (R1e) | nToN matrix: target's source count would exceed `MaxConnectsPerTarget` | Ember+ Doc §p.88 |
| `matrix:max-total-connects` | defined (R1e) | nToN matrix: sum-across-all-targets would exceed `MaxTotalConnects` | Ember+ Doc §p.88 |

### emberplus

| Code | Status | When | Anchor |
|---|---|---|---|
| `emberplus:invocation-failed` | defined (R1f) | `InvocationResult.Success=false` on the wire. `InvokeFunction` returns the result pointer alongside the typed error so callers can still print the (possibly-empty) result tuple while the error sets exit 1 | Ember+ Doc §p.92 |
| `emberplus:invocation-failed-with-description` | defined (sentinel only, R1f) | reserved for the case when the codec decodes the optional `description` field — currently the codec doesn't parse it, so every `Success=false` maps to `emberplus:invocation-failed`. Codec extension is separate scope | Ember+ Doc §p.92 |

### validation

| Code | Status | When | Anchor |
|---|---|---|---|
| `validation:failed` | defined (R1g) | catch-all wrap when a `ValueValidator` rejects a client-side check (wrong type, IPv4 unparseable, enum out of range) | dhs `internal/consumer/value_validator.go` |
| `validation:invalid-integer` | partial (today emits free-text via `*consumer.ValidationError`; R1g formalizes) | `--value` not parseable as integer | dhs PR #453 |
| `validation:invalid-real` | partial | `--value` not parseable as real | dhs PR #453 |
| `validation:invalid-enum-index` | partial | `--value` outside enum range | dhs PR #453 |
| `validation:invalid-enum-label` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | `--value` is label not in `enumMap` | Ember+ Doc §p.86 |
| `validation:out-of-range-low` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value below Parameter `minimum` | Ember+ Doc §p.86 |
| `validation:out-of-range-high` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value above Parameter `maximum` | Ember+ Doc §p.86 |
| `validation:step-misaligned` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | value not on Parameter `step` grid | Ember+ Doc §p.86 |
| `validation:enum-not-supported` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | `--value` is a label but Parameter has no `enumMap` | Ember+ Doc §p.86 |
| `validation:round-not-applicable` | defined (R16 [#483](https://github.com/by-openclaw/go-acp/issues/483)) | `--round` passed but target Parameter is non-numeric | dhs |
| `validation:invalid-host` | defined (R1b) | host string empty on Dial | dhs |
| `validation:invalid-port` | defined (R1b) | port outside [1, 65535] on Dial or [0, 65535] on Listen | dhs |
| `validation:empty-payload` | defined (R1b) | Send called with a zero-length payload | dhs |
| `validation:invalid-max-size` | defined (R1b) | Receive called with non-positive `maxSize` / `maxPayload` | dhs |
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
| `plugin:not-implemented` | defined (R1g) | plugin stub for unsupported operation | dhs `internal/consumer/errors.go` |
| `plugin:not-connected` | defined (R1g) | call requires a live transport and none established | dhs `internal/consumer/errors.go` |
| `plugin:unknown-label` | defined (R1g) | GetValue/SetValue request uses a Label that was never seen by the walker | dhs `internal/consumer/errors.go` |
| `plugin:object-not-found` | defined (R1g) | tree miss on path / OID | dhs `internal/consumer/value_validator.go` |
| `plugin:identity-unresolved` | defined (R1g) | `GetIdentity` could not fingerprint the card from the wire | dhs `internal/consumer/identity.go` |
| `plugin:not-walked` | pending (future) | call requires walked tree state | dhs `internal/emberplus/consumer/plugin.go::ensureWalked` |
| `plugin:wrong-kind` | pending (future) | resolved object isn't the kind the verb needs (e.g. `set` on Node) | dhs |
| `plugin:by-session-unavailable` | pending (R22 [#487](https://github.com/by-openclaw/go-acp/issues/487)) | `profile --by-session` and no R24 admin endpoint reachable | dhs |
| `plugin:admin-socket-not-found` | pending (R25 [#490](https://github.com/by-openclaw/go-acp/issues/490)) | local admin socket not found (producer not running) | dhs |

### session

| Code | Status | When | Anchor |
|---|---|---|---|
| `session:write-timeout` | defined (R1g) | write transmitted, no confirm within window | dhs `internal/consumer/errors.go` |
| `session:write-coerced` | defined (R1g) | provider echoed a different value (clamp/round/enum-remap); callers may tolerate via `errors.Is(err, ErrWriteCoerced)` | dhs `internal/consumer/errors.go` |
| `session:write-rejected` | defined (R1g) | provider's echo refuses the write | dhs `internal/consumer/errors.go` |
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
