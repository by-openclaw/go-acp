# Ember+ — use-case matrix

Last verified: `HEAD` on `feat/emberplus-stream-idle-ttl-472` (Ember+ DOD branch). Manual baseline per R20 #485; CI auto-bump is a future step.

| UC | Use case | Consumer | Provider | Implementation | Tests | Wireshark | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
| UC-1 | `info` — device summary | ✅ | ✅ | [cmd/dhs/cmd_info.go](../../../cmd/dhs/cmd_info.go), [internal/emberplus/consumer/plugin.go](../../../internal/emberplus/consumer/plugin.go) | live runbook §1 + [internal/emberplus/consumer](../../../internal/emberplus/consumer) | `dhs_emberplus.lua` § GetDirectory + Node identity | DTD field surfaced by R6 #470 |
| UC-2 | `walk` — full tree enumeration | ✅ | ✅ | [cmd/dhs/cmd_walk.go](../../../cmd/dhs/cmd_walk.go), [plugin.go::Walk](../../../internal/emberplus/consumer/plugin.go) | runbook §2 | `dhs_emberplus.lua` § Glow / QualifiedNode | Multi-frame reassembly handled |
| UC-3 | `get` — read one Parameter | ✅ | ✅ | [cmd/dhs/cmd_get.go](../../../cmd/dhs/cmd_get.go) | runbook §3 | `dhs_emberplus.lua` § Parameter | OID + dotted-label both accepted (R21 #486) |
| UC-4 | `set` — write one Parameter | ✅ | ✅ | [cmd/dhs/cmd_set.go](../../../cmd/dhs/cmd_set.go) | runbook §4 | `dhs_emberplus.lua` § Parameter set | Range/step/enum constraints applied client-side (R16 #483) |
| UC-5 | `watch` — live updates | ✅ | ✅ | [cmd/dhs/cmd_watch.go](../../../cmd/dhs/cmd_watch.go), [cmd_watch_hotplug.go](../../../cmd/dhs/cmd_watch_hotplug.go) | [cmd_watch_test.go](../../../cmd/dhs/cmd_watch_test.go) | `dhs_emberplus.lua` § Subscribe/announce | Tally + stream + glow merged feed |
| UC-6 | `matrix` connect / disconnect / absolute | ✅ | ✅ | [cmd/dhs/cmd_matrix.go](../../../cmd/dhs/cmd_matrix.go), [provider/matrix.go](../../../internal/emberplus/provider/matrix.go) | runbook §6 | `dhs_emberplus.lua` § Matrix.connections | oneToOne source-steal accepted with compliance event |
| UC-7 | `invoke` — function RPC | ✅ | ✅ | [cmd/dhs/cmd_invoke.go](../../../cmd/dhs/cmd_invoke.go) | [cmd_invoke_format_test.go](../../../cmd/dhs/cmd_invoke_format_test.go) | `dhs_emberplus.lua` § QualifiedFunction.invoke | `--format human` pretty-prints getSalvo (R5 #482) |
| UC-8 | `stream` — explicit subscribe | ✅ | ✅ | [cmd/dhs/cmd_stream.go](../../../cmd/dhs/cmd_stream.go), [provider/streamer.go](../../../internal/emberplus/provider/streamer.go) | [cmd_stream_test.go](../../../cmd/dhs/cmd_stream_test.go) | `dhs_emberplus.lua` § Subscribe (CmdSubscribe) | Server-side idle-TTL eviction via R9 #472 `--stream-ttl` |
| UC-9 | `profile` — compliance classification | ✅ | n/a | [cmd/dhs/cmd_profile.go](../../../cmd/dhs/cmd_profile.go) | [cmd_profile_test.go](../../../cmd/dhs/cmd_profile_test.go) | n/a (post-walk aggregate) | `--format json`, `--since`, `--show-events`, `--by-session` (R22 #487) |
| UC-10 | `export` / `import` — round-trip | ⚠️ partial | n/a | [cmd/dhs/cmd_export.go](../../../cmd/dhs/cmd_export.go), [cmd_import.go](../../../cmd/dhs/cmd_import.go) | [cmd_extract_test.go](../../../cmd/dhs/cmd_extract_test.go) | n/a | Full Glow round-trip pending R4 [#461](https://github.com/by-openclaw/go-acp/issues/461) |
| UC-11 | `extract` — per-product DM triple | ✅ | n/a | [cmd/dhs/cmd_extract.go](../../../cmd/dhs/cmd_extract.go) | [cmd_extract_test.go](../../../cmd/dhs/cmd_extract_test.go) | n/a | Cache + manifest co-written |
| UC-12 | `validate` — offline frame decode | ✅ | n/a | [cmd/dhs/cmd_validate.go](../../../cmd/dhs/cmd_validate.go) | [cmd_validate_test.go](../../../cmd/dhs/cmd_validate_test.go) | n/a (decodes via dissector parity) | `--report <md\|json>` lands report (R23 #488); `--lua` mode pending R12 [#473](https://github.com/by-openclaw/go-acp/issues/473) |
| UC-13 | `bench` — matrix latency | ✅ | n/a | [cmd/dhs/cmd_emberplus_bench.go](../../../cmd/dhs/cmd_emberplus_bench.go) | _ad-hoc fixtures only_ | `dhs_emberplus.lua` § Matrix.connections | RFC 2544 profiles pending R13 [#474](https://github.com/by-openclaw/go-acp/issues/474) |
| UC-14 | `health` — connector self-check | ❌ #300 | ❌ #300 | n/a today | n/a | n/a | TODO both sides — tracked at [#300](https://github.com/by-openclaw/go-acp/issues/300) |
| UC-15 | `discover` — mDNS / IS-04 peers | ❌ R18 #477 | ❌ R18 #477 | n/a today | n/a | n/a | Bidirectional mDNS pending R18 [#477](https://github.com/by-openclaw/go-acp/issues/477) |
| UC-16 | `diff` / `convert` — offline format ops | ✅ | n/a | [cmd/dhs/cmd_diff.go](../../../cmd/dhs/cmd_diff.go), [cmd_convert.go](../../../cmd/dhs/cmd_convert.go) | _golden-file fixtures_ | n/a | Offline-only; spec-strict format negotiation |

## Error-code surface (per R1 #468)

| Verb | Codes emitted today | Pending |
| --- | --- | --- |
| `info` | `transport:*`, `s101:*`, `plugin:not-connected` | — |
| `walk` | `s101:*`, `glow:*`, `ber:*` | — |
| `get` | `plugin:object-not-found`, `validation:invalid-oid` (R21) | — |
| `set` | `validation:out-of-range-{low,high}`, `validation:step-misaligned`, `validation:invalid-enum-label`, `validation:enum-not-supported`, `validation:round-not-applicable` (R16) | — |
| `watch` | `transport:reconnect` (info), `s101:*` | — |
| `matrix` | `matrix:target-locked`, `matrix:source-not-available` | source-steal event (compliance only) |
| `invoke` | `plugin:invocation-failed` | — |
| `stream` | `plugin:no-stream-parameter` | — |
| `profile` | `validation:invalid-format`, `validation:invalid-duration`, `plugin:by-session-unavailable` (R22) | R24 admin endpoint unlocks `--by-session` |
| `export` / `import` | `glow:json-decode-failed`, `validation:invalid-format` | full Glow round-trip codes (R4) |
| `extract` | `plugin:product-resolve-failed` | — |
| `validate` | `validation:invalid-report-format` (R23), `transport:report-target-unwritable`, `transport:input-not-found`, per-layer `s101:*` / `ber:*` / `glow:*` | `--lua` mode codes (R12) |
| `bench` | `plugin:bench-config` | RFC 2544 profile codes (R13) |
| `health` | _pending #300_ | — |
| `discover` | _pending R18 #477_ | — |
| `diff` / `convert` | `validation:invalid-format` | — |

Exit-class conventions: `2` for `validation:*` and `plugin:*` (caller's input fault); `1` for `transport:*`, `s101:*`, `ber:*`, `glow:*`, `matrix:*` (runtime / wire fault). See [`internal/errcode/errcode.go`](../../../internal/errcode/errcode.go).

## Logging surface (per R15 #476)

Every verb above accepts the shared `-v / -vv / -vvv / -vvvv` ladder plus `--log-format text|json|loki` and `--log-only`. See [`docs/logging.md`](../../logging.md).

## Cross-references

- Runbook (verb-by-verb walkthrough): [internal/emberplus/docs/runbook.md](../../../internal/emberplus/docs/runbook.md)
- Atomic protocol context: [internal/emberplus/CLAUDE.md](../../../internal/emberplus/CLAUDE.md)
- Wireshark dissector: [internal/emberplus/wireshark/dhs_emberplus.lua](../../../internal/emberplus/wireshark/dhs_emberplus.lua)
- Spec PDFs: [internal/emberplus/assets/](../../../internal/emberplus/assets/)
- Connector definition of done: [docs/adr/0025-per-connector-definition-of-done.md](../../adr/0025-per-connector-definition-of-done.md)
- Error-code taxonomy (R1 #468): [internal/errcode/](../../../internal/errcode/)
