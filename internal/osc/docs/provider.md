# OSC Provider

> **Status: shipping**
>
> Producer (sender / responder) for Open Sound Control 1.0 + 1.1. OSC is a
> **message/push** protocol, so the provider does not "serve a tree" the way the
> ACP1 provider serves a canonical device. Instead it **pushes** OSC packets to a
> destination (`send`, `fader`) or binds a port to log inbound traffic (`serve`).
> Symmetric to the consumer at [../consumer/](../consumer/); reuses the same
> `codec.Message` / `codec.Bundle` codec and type constants.
>
> CLI: `dhs producer osc-v10 send …` · `… fader …` · `… serve …`.
> See [runbook.md](runbook.md) for the operator workflow.

---

## References

| Document | Path |
|---|---|
| Spec (1.0 / 1.1) | <https://opensoundcontrol.org/spec-1_0.html> · <https://opensoundcontrol.org/spec-1_1.html> |
| Wire format | [../CLAUDE.md](../CLAUDE.md) |
| Wireshark dissector | [../wireshark/dhs_osc.lua](../wireshark/dhs_osc.lua) |
| Source | [../provider/](../provider/) — `Server`, `NewServerV10` / `NewServerV11` |
| CLI dispatcher | [../../../cmd/dhs/cmd_osc.go](../../../cmd/dhs/cmd_osc.go) |

> Cite **opensoundcontrol.org**, never `osc.org` (Orlando Science Center).

---

## Verbs

| Verb | Role | Key flags |
|---|---|---|
| `send` | emit one OSC message and exit | `--to host:port` `--transport` `--address` `--types` `[args…]` |
| `fader` | continuous high-rate fader simulator (perf measurement) | `--to` `--transport` `--address` `--rate` `--duration` `--min` `--max` `--pattern ramp\|sine\|random` |
| `serve` | bind a port and log inbound packets (act-as-OSC-device, no echo) | `--bind transport:port` `--pattern` |

> **N/A — `serve --tree fixture.json`.** The ACP1 provider serves a canonical
> tree.json as a device. OSC has no tree to serve; `serve` is a passive logger
> (it shares the consumer's `watch` decode path), not a tree responder.

---

## send

Builds one `codec.Message{Address, Args}` from `--address` + `--types` + the
positional argument tokens, encodes it, and sends it over the chosen transport.
Each `--types` char maps to one arg, except the payload-less `T F N I` and array
markers `[ ]`, which consume **zero** tokens.

Live-captured round-trips (consumer `watch` on the matching port shown what
arrived):

```
# UDP, osc-v10
$ dhs_osc.exe producer osc-v10 send --to 127.0.0.1:19000 --transport udp --address /mixer/fader1 --types fsi 0.75 PGM 42
osc-v10 sent /mixer/fader1 [,fsi] to udp://127.0.0.1:19000
# consumer decoded:  [udp      ] /mixer/fader1  ,fsi  0.75 "PGM" 42

# RGBA colour (4 hex bytes)
$ dhs_osc.exe producer osc-v10 send --to 127.0.0.1:19000 --transport udp --address /color --types r FF8800FF
# consumer decoded:  [udp      ] /color  ,r  #ff8800ff

# TCP length-prefix, osc-v10
$ dhs_osc.exe producer osc-v10 send --to 127.0.0.1:19001 --transport tcp-len --address /q/go --types s START
# consumer decoded:  [tcp-len  ] /q/go  ,s  "START"

# TCP SLIP, osc-v11 — 1.1 payload-less tags + array
$ dhs_osc.exe producer osc-v11 send --to 127.0.0.1:19002 --transport tcp-slip --address /q/go --types iTFNI 7
# consumer decoded:  [tcp-slip ] /q/go  ,iTFNI  7 T F N I
$ dhs_osc.exe producer osc-v11 send --to 127.0.0.1:19002 --transport tcp-slip --address /array --types "i[ii]" 1 10 20
# consumer decoded:  [tcp-slip ] /array  ,i[ii]  1 [ 10 20 ]
```

### Type-tag → token mapping

| Tags | Tokens consumed | Token format |
|---|---|---|
| `i` `h` | 1 | decimal int |
| `f` `d` | 1 | decimal float |
| `s` `S` | 1 | string |
| `b` | 1 | hex blob |
| `t` | 1 | u64 timetag (`0x…` accepted) |
| `c` | 1 | first byte of the token |
| `r` `m` | 1 | exactly 4 hex bytes |
| `T` `F` `N` `I` `[` `]` | **0** | — (payload-less / markers) |

> **Note — help shows `--args`.** The producer help text examples write
> `--types ifs --args 42 3.14 hello`, but the implementation reads the trailing
> **positional** tokens (`fs.Args()`), not an `--args` flag. Use bare positionals
> after the flags, as in the captures above. A leading negative number directly
> after `--types` is parsed by Go's flag package as an unknown flag
> (`flag provided but not defined: -3.5`); place negatives only after at least one
> preceding positional (captured: `--types if 1 -6.0` works).

---

## fader

High-rate fader simulator for throughput / latency measurement. Sends a single
float per frame at `--rate` Hz for `--duration`, in `ramp` / `sine` / `random`
shape, and prints a perf summary to stderr.

**Live-captured (UDP, 1000 Hz, 2 s, sine — real numbers from this host):**

```
$ dhs_osc.exe producer osc-v10 fader --to 127.0.0.1:19000 --transport udp --address /fader --rate 1000 --duration 2s --pattern sine
osc-v10 fader → udp://127.0.0.1:19000  rate=1000Hz  duration=2s  pattern=sine  frames≈2000

=== fader perf (udp) ===
  frames     : 1979  (errors: 0)
  wall       : 2.000349s
  throughput : 989 frames/s
  send-call latency  mean=3µs  p50=0s  p95=0s  p99=0s  max=1.029ms
```

(Frame count and latencies vary per run — these are one real measured run.)

---

## serve

Binds a port and logs every inbound packet using the same decode path as the
consumer `watch` verb — it does **not** echo or respond. Useful as a passive
"act-as-OSC-device" sink.

**Live-captured:**

```
$ dhs_osc.exe producer osc-v10 serve --bind udp:19009
osc-v10 serving (logging) udp:19009 (pattern="") — Ctrl-C to stop
# after a peer sent /play ,s "GO":
[udp      ] /play  ,s  "GO"
```

---

## Bundles

The provider can group N messages under one timetag into a single outgoing
bundle (`Server.SendBundle`). The consumer fans the element messages to
subscribers in order. Verified by `TestV10_UDP_Loopback_Bundle_FansToSubscribers`
(three element messages all delivered). The committed
[`../testdata/fixtures/bundle_timetag.bin`](../testdata/fixtures/) (68 bytes,
real codec output) pins the `#bundle` + NTP-timetag + element-size envelope.

> **N/A — `--play` value drift.** ACP1's provider self-drives object value drift
> for `watch`. OSC's equivalent live source is `fader` (continuous push) or a
> `send` loop; there is no object tree to oscillate.

---

## Broadcast & multi-destination

UDP providers honour broadcast / subnet-broadcast destinations (SO_BROADCAST on
the egress socket, per [../CLAUDE.md](../CLAUDE.md)). Multiple `AddDestination`
calls fan one message to several peers. There is no per-destination session or
identity — OSC is connectionless in spirit even over TCP.

---

## Provider verb summary

| Verb | Transport surface | Output |
|---|---|---|
| `send` | udp / tcp-len (v10) / tcp-slip (v11) | one packet, then exit; banner to stderr |
| `fader` | same | continuous float push; perf summary to stderr |
| `serve` | same (binds, logs) | decoded inbound lines to stdout |
</content>
