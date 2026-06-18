# OSC Consumer

Consumer (listener / monitor) for Open Sound Control 1.0 + 1.1. OSC is a
**message/push** protocol: the consumer binds a port and observes inbound
packets — it does **not** dial a device, walk a tree, or read object values.
The single verb is `watch`.

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (1.0, authoritative) | <https://opensoundcontrol.org/spec-1_0.html> | OSC 1.0 |
| Spec (1.1) | <https://opensoundcontrol.org/spec-1_1.html> | OSC 1.1 additions |
| Wire format | [../CLAUDE.md](../CLAUDE.md) | Byte-exact wire spec + quirks |
| Wireshark dissector | [../wireshark/dhs_osc.lua](../wireshark/dhs_osc.lua) | Byte-exact reference (filter `dhs_osc`) |
| Fixtures | [../testdata/fixtures/](../testdata/fixtures/) | Golden codec-produced `*.bin` packets |
| Source | [../consumer/](../consumer/) | `package osc` — `Plugin`, `NewPluginV10` / `NewPluginV11` |
| CLI dispatcher | [../../../cmd/dhs/cmd_osc.go](../../../cmd/dhs/cmd_osc.go) | `watch` handler |

> Cite **opensoundcontrol.org**, never `osc.org` (Orlando Science Center).

---

## Transport

| Kind | `--listen` value | Default port | Version | Framing |
|---|---|---|---|---|
| UDP | `udp:8000` | 8000 | both | one datagram = one packet |
| TCP length-prefix | `tcp-len:8000` | 8000 | osc-v10 only | int32 BE size + packet |
| TCP SLIP | `tcp-slip:8001` | 8001 | osc-v11 only | RFC 1055 double-END |

### Firewall rules

```
UDP 8000  inbound              (UDP listener — primary)
TCP 8000  inbound              (osc-v10 length-prefix)
TCP 8001  inbound              (osc-v11 SLIP)
UDP 8000  inbound broadcast    (broadcast / subnet-broadcast sources; SO_REUSEADDR)
```

### CLI

```
dhs consumer osc-v10 watch --listen udp:8000
dhs consumer osc-v10 watch --listen tcp-len:8000
dhs consumer osc-v11 watch --listen tcp-slip:8001 --pattern "/{pgm,pvw}"
```

---

## Capabilities & Compliance Status

| Capability | Spec | Status | Notes |
|---|---|---|---|
| UDP listener (default 8000) | 1.0 §"Transport" | ✅ fully compliant | SO_REUSEADDR; multi-listener (best-effort on platforms without SO_REUSEPORT) |
| TCP length-prefix decode (1.0) | 1.0 framing | ✅ fully compliant | int32 BE size prefix; `osc-v10` only |
| TCP SLIP decode (1.1) | 1.1 SLIP / RFC 1055 | ✅ fully compliant | double-END; `osc-v11` only |
| Core type tags `i f s b` | 1.0 §"Type Tags" | ✅ fully compliant | decoded + printed |
| Extended tags `h d t S c r m` | 1.0 (commonly impl.) | ✅ fully compliant | int64/float64/timetag/symbol/char/RGBA/MIDI |
| 1.1 payload-less `T F N I` | 1.1 | ✅ fully compliant | zero-byte args; captured live (`,iTFNI`) |
| 1.1 array markers `[ ]` | 1.1 | ✅ fully compliant | nested-sequence tags; captured live (`,i[ii]`) |
| Bundle decode (recursive) | 1.0 §"Bundle" | ✅ fully compliant | fans element messages to subscribers in order |
| Address-pattern subscription (`* ? [] {}`) | 1.0 §"Address Pattern Matching" | ✅ fully compliant | `*` matches within one part, not across `/` (captured) |
| Compliance note `osc_type_tag_unknown` | — (our extension) | ✅ wired | fired by codec on an unknown tag |
| Remaining compliance catalogue | — | ⏳ defined, wired incrementally | catalogue in [../CLAUDE.md](../CLAUDE.md); only `osc_type_tag_unknown` is wired in the decode path today |
| Device info / walk / get / set | — | ⛔ not applicable | OSC has no device-info reply and no walkable object tree |
| Export / import / tree report | — | ⛔ not applicable | no tree/DM to serialize |

Legend: ✅ fully compliant · ⏳ pending · ⛔ not applicable.

---

## Timeouts

OSC `watch` is a **passive listener with no request/reply**, so there are no
per-request retry or receive timeouts. The listener runs until `ctx` is
cancelled (Ctrl-C):

```
... — Ctrl-C to stop
```

> **N/A — retry / MTID / receive-timeout.** No transaction model exists; nothing
> to retry or de-duplicate. ACP1's `--timeout` / `MaxRetries` have no analogue.

---

## Compliance Profile

OSC absorbs spec deviations and records a `ComplianceNote` (see
[`../codec/errors.go`](../codec/errors.go) `ComplianceNote{Kind, Detail}`). The
named catalogue lives in [../CLAUDE.md](../CLAUDE.md):

| Event | Fires when |
|---|---|
| `osc_type_tag_unknown` | type tag outside the known set received **(wired today)** |
| `osc_alignment_violation` | OSC-string / OSC-blob not 4-byte padded |
| `osc_truncated_message` | body ends before an expected argument boundary |
| `osc_comma_missing` | type-tag string does not begin with `,` |
| `osc_slip_truncated` | SLIP frame ends mid-packet on TCP |
| `osc_array_unbalanced` | `[` without matching `]` in a 1.1 type-tag string |
| `osc_bundle_nested_depth` | bundle nested beyond the compliance guard limit |

Of these, only `osc_type_tag_unknown` is fired in the decode path today
(`internal/osc/codec/message.go`); the remainder are defined and back the
codec's typed errors (`ErrAlignment`, `ErrCommaMissing`, `ErrArrayUnbalanced`,
…) and ship as decode paths are audited — same incremental posture as acp1.

---

## Error Reference

| Name | Layer | When | Recovery |
|---|---|---|---|
| `--listen / --bind must be transport:port` | CLI | malformed `--listen` | use `udp:8000` form |
| `transport=tcp-slip requires --protocol osc-v11` | CLI | SLIP on osc-v10 | use `osc-v11` (captured §verbs.md) |
| `transport=tcp-len requires --protocol osc-v10` | CLI | length-prefix on osc-v11 | use `osc-v10` |
| `unknown transport %q` | CLI | not udp/tcp-len/tcp-slip | pick a valid kind |
| `ErrTruncated` | codec | payload ends early | absorb + note; frame skipped |
| `ErrCommaMissing` | codec | type-tag lacks leading `,` | absorb + note |
| `ErrArrayUnbalanced` | codec | `[` without `]` (1.1) | absorb + note |
| `ErrTagUnknown` | codec | unknown type tag | absorb + fire `osc_type_tag_unknown` |

---

## Discovery

> **N/A — OSC has no discovery.** A peer never advertises its address space.
> "Discovery" is purely passive observation: bind a port with `watch` and record
> which addresses + type-tags arrive. There is no `getObject`, root-count, or DM
> cache to populate.

---

## Subscriptions (the live model)

`watch` subscribes via `SubscribePattern(pattern, fn)`. Every matching inbound
packet — message or bundle element — fires the callback, which prints one line:

```
[<transport>] <address>  ,<tags>  <arg1> <arg2> ...
```

Live-captured (UDP loopback, osc-v10):

```
[udp      ] /mixer/fader1  ,fsi  0.75 "PGM" 42
```

Bundles fan their element messages to subscribers in order (verified by
`TestV10_UDP_Loopback_Bundle_FansToSubscribers`): a bundle of `/ch/1/mute`,
`/ch/2/mute`, `/ch/3/label` delivers three separate callback lines.

### Pattern matching (OSC 1.0 wildcards)

`--pattern` accepts `*`, `?`, `[abc]`, `{a,b}`. `*` matches a run within one
address part and does **not** cross `/`. Captured: `--pattern "/mixer/*/gain"`
matched `/mixer/ch3/gain` and dropped `/other/skip`.

---

## Raw Capture

The `watch` verb prints **decoded** lines to stdout (not raw hex). Byte-exact
references are the committed golden fixtures
([`../testdata/fixtures/*.bin`](../testdata/fixtures/), real codec output) and a
live OS-socket pcapng under
[`../testdata/scenarios/battery/`](../testdata/scenarios/battery/) for the
Wireshark dissector replay.

> **N/A — `--capture <path>.jsonl`.** The osc `watch` verb does not expose a
> JSONL raw-frame capture flag today (unlike acp1). Capture live wire bytes with
> Wireshark/tshark + `dhs_osc.lua`, or use the committed fixtures.

---

## Test Peer

| Peer | Where | Role |
|---|---|---|
| osc.js reference | [../assets/test-harness](../assets/test-harness) | cross-implementation oracle (byte oracle + interop) |
| dhs provider (loopback) | [../provider/](../provider/) | trusted-loopback regression only, after the osc.js tier passes |
</content>
