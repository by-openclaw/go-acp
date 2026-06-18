# Probel SW-P-08 Connector

Consumer connector for the Probel SW-P-08 / SW-P-88 General Remote Control
Protocol (level-scoped matrix routing over DLE/STX-framed TCP).

---

## References

| Document | Path | Description |
|---|---|---|
| Spec (authoritative) | [SW-P-08 Issue 30.doc](../assets/probel-sw08p/SW-P-08%20Issue%2030.doc) | General Remote Control Protocol, Issue 30. Read via antiword (PDF corrupted) |
| Spec (text) | [SW-P-08_issue_30.txt](../assets/probel-sw08p/SW-P-08_issue_30.txt) | antiword extraction for grep |
| SW-P-88 catalogue | [SW-P-88 Issue 3.pdf](../assets/probel-sw08p/SW-P-88%20Issue%203.pdf) | command-byte cross-reference |
| Protocol reference | [../CLAUDE.md](../CLAUDE.md) | wire format, §2 framing, command catalogue, quirks |
| Source code | [internal/probel-sw08p/consumer/](../../../internal/probel-sw08p/consumer/) | plugin implementation |
| Codec | [internal/probel-sw08p/codec/](../../../internal/probel-sw08p/codec/) | stdlib-only byte codec |
| Unit tests | [internal/probel-sw08p/codec/](../../../internal/probel-sw08p/codec/) | table-driven, expected bytes from the spec |
| Integration tests | [internal/probel-sw08p/integration/loopback_test.go](../integration/loopback_test.go) | full TCP round-trips against the loopback emulator |

### Spec section index (for debugging)

| Topic | Section |
|---|---|
| Transmission protocol — ACK / NAK, retry, 10 ms, 128-byte DATA | §2 (**not** §3.5) |
| Narrow matrix/level packing + multiplier (4-bit + 3-bit DIV-128) | §3.1.2 |
| Multiplier semantics for protect / tally | §3.1.6 |
| RX general (controller → matrix) | §3.2 |
| TX general (matrix → controller) | §3.3 |
| RX / TX extended (wide addressing) | §3.4 / §3.5 |

---

## Transport

DLE/STX framing over TCP. Both general and extended wire forms are
implemented; the codec auto-escalates to extended form when an address
exceeds the general field width (`needsExtended()`).

| Field | Value | Notes |
|---|---|---|
| TCP port (default) | 2008 | `splitHostPort` falls back to 2008 when the port is omitted |
| Framing | `DLE STX <data> <btc> <cksum> DLE ETX` | §2 |
| DLE-stuffing | any `0x10` inside `<data>…<cksum>` doubled to `10 10` | framing DLEs never stuffed |
| Checksum | 8-bit two's complement of `(data \|\| btc)` | `0x100 − (Σ & 0xFF)` |
| Link ACK / NAK | `DLE ACK` = `10 06`, `DLE NAK` = `10 15` | §2 — every frame answered |
| ACK timeout / retries | 1 s / 5× | §2 |
| DATA cap | 128 soft / 255 hard | §2 |

### Firewall rules

```
TCP 2008  outbound      (default SW-P-08 port)
```

### CLI transport selection

```
dhs consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
dhs consumer probel-sw08p interrogate 10.100.0.42      --matrix 0 --level 0 --dst 5   # :2008 implied
```

SW-P-08 is **level-scoped**: every crosspoint / protect / name command
carries `<matrix, level, dst, src>`. Do not assume level 0 — pass
`--matrix` / `--level` explicitly.

---

## Command catalogue

The verbs below map to these wire command bytes (general form; extended
form flips bit 7). Full table in [`../CLAUDE.md`](../CLAUDE.md).

| Verb | rx cmd | reply tx cmd | Scope |
|---|---|---|---|
| `interrogate` | 001 | 003 Tally | matrix+level+dst |
| `connect` | 002 | 004 Connected | matrix+level+dst+src |
| `maintenance` | 007 | — (fire-and-forget) | matrix+level |
| `dual-status` | 008 | 009 | — |
| `protect-interrogate` | 010 | 011 Protect Tally | matrix+level+dst+device |
| `protect-connect` | 012 | 013 Protect Connected | matrix+level+dst+device |
| `protect-disconnect` | 014 | 015 Protect Disconnected | matrix+level+dst+device |
| `protect-name` | 017 | 018 | device |
| `protect-dump` | 019 | 020 | matrix+level+first-dst |
| `tally-dump` | 021 | 022 byte / 023 word | matrix+level |
| `master-protect` | 029 | 013 Protect Connected | matrix+level+dst+device |
| `all-source-names` | 100 | 106 | matrix+level |
| `single-source-name` | 101 | 106 | matrix+level+src |
| `all-dest-names` | 102 | 107 | matrix |
| `single-dest-name` | 103 | 107 | matrix+dst |
| `all-source-assoc-names` | 114 | 116 | matrix |
| `single-source-assoc-name` | 115 | 116 | matrix+src |
| `update-name` | 117 | — (fire-and-forget) | matrix+level+first-id+names |
| `salvo-connect` | 120 / 121 | 122 / 123 | matrix+level+dsts+src+salvo |
| `watch` | — (passive) | 003 / 004 async | — |
| `discover` | 008/100/102/021 | composite | matrix+level |
| `bench` | 001/002 ×N | 003/004 | matrix(es)+size |

---

## Capabilities & compliance status

| Capability | Spec § | Status | Notes |
|---|---|---|---|
| §2 framing (DLE/STX, DLE-stuffing, BTC, checksum) | §2 | ✅ fully compliant | wire-verified against the loopback emulator |
| Link ACK / NAK + 5× retry + 1 s timeout | §2 | ✅ fully compliant | `OnRetry` → compliance event + metric |
| Crosspoint interrogate / connect (general + extended) | §3.2.1 / 3.2.2 | ✅ fully compliant | auto-escalates to extended above general field width |
| Crosspoint tally-dump (byte 022 + word 023, streamed) | §3.2.21 | ✅ fully compliant | byte form ≤ 256 dsts, word above; decode emits per-dst |
| Protect interrogate / connect / disconnect | §3.2.10/12/14 | ✅ fully compliant | multi-state (not a plain lock bit) |
| Master protect connect | §3.2.29 | ✅ fully compliant | returns state 2 (master) |
| Protect device name + protect dump | §3.2.17 / 3.2.19 | ✅ fully compliant | 8-char owner names |
| Name family 100/101/102/103/114/115 | §3.2.x | ✅ fully compliant | 4/8/12/16-char widths; space/NUL pad |
| Update-name (write labels) | §3.2.26 | ✅ fully compliant | fire-and-forget (no reply) |
| Dual-controller status | §3.2.8 | ✅ fully compliant | master/slave/active/idle-faulty |
| Salvo connect-on-go + go (build / fire / clear) | §3.2.30 | ✅ codec + provider; ⚠ CLI blocked | see [salvo-connect](#salvo-connect--cli-blocked) |
| Application keepalive (auto-answer matrix ping) | TS #91 | ✅ fully compliant | plugin answers `tx 011` keepalive with `rx 034` |
| Async tally fan-out (`watch`) | §3.2.3 | ✅ fully compliant | observes `tx 003` broadcast to all sessions |
| Scale bench (persistent TCP, 2 mtx × 65535) | — (our extension) | ✅ fully compliant | per-cmd latency to CSV/MD |

Legend: ✅ fully compliant · ⚠ partial / blocked · ⛔ not implemented.

---

## Timeouts

| Timer | Default | Override |
|---|---|---|
| Per-command operation | 5 s | `--timeout DUR` (most verbs) |
| `discover` | 15 s | n/a (composite of 4 reads) |
| `bench` overall | 30 min | `--timeout DUR` |
| `watch` | unbounded (Ctrl-C) | `--timeout DUR` (0 = run forever) |
| Link ACK timeout | 1 s | §2 constant |
| ACK retries | 5× | §2 constant |

---

## CLI commands reference

Every subcommand usable against an SW-P-08 matrix, with a runnable example
and a real captured wire frame. Captures are real `--capture` JSONL frames
from the loopback emulator (`127.0.0.1:12008`); see
[runbook.md](runbook.md#capture-procedure-how-the-samples-in-these-docs-were-made)
for the procedure. For per-verb wire-byte breakdowns + checksum
cross-checks, see **[verbs.md](verbs.md)**.

Global flag (every subcommand): `--capture FILE.jsonl` records every wire
frame (TX + RX, including `DLE ACK` / `DLE NAK`) as JSONL —
`{ts, proto, dir, hex, len}` per frame, same shape as the acp1 / acp2 /
emberplus capture.

### `interrogate` — read the source on one crosspoint

```
dhs consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
→ crosspoint tally  matrix=0 level=0 dst=5 → src=12
```

### `connect` — route a source to a destination

```
dhs consumer probel-sw08p connect 127.0.0.1:2008 --matrix 0 --level 0 --dst 5 --src 12
→ crosspoint connected  matrix=0 level=0 dst=5 src=12
```

### `tally-dump` — dump every crosspoint on (matrix, level)

```
dhs consumer probel-sw08p tally-dump 127.0.0.1:2008 --matrix 0 --level 0
→ tally-dump (byte) matrix=0 level=0 first_dst=0 tallies=16
    dst=5 → src=12
```

### `protect-interrogate` / `protect-connect` / `protect-disconnect`

```
dhs consumer probel-sw08p protect-connect    127.0.0.1:2008 --matrix 0 --level 0 --dst 4 --device 9
dhs consumer probel-sw08p protect-interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 4 --device 9
dhs consumer probel-sw08p protect-disconnect 127.0.0.1:2008 --matrix 0 --level 0 --dst 4 --device 9
→ protect connected     matrix=0 level=0 dst=4 device=9 state=1
→ protect disconnected  matrix=0 level=0 dst=4 device=9 state=0
```

`--device` range 0–1023.

### `master-protect` — master-override protect connect

```
dhs consumer probel-sw08p master-protect 127.0.0.1:2008 --matrix 0 --level 0 --dst 6 --device 3
→ master-protect connected  matrix=0 level=0 dst=6 device=3 state=2
```

### `protect-name` / `protect-dump`

```
dhs consumer probel-sw08p protect-name 127.0.0.1:2008 --device 9
→ device 9 name="DEV 0009"

dhs consumer probel-sw08p protect-dump 127.0.0.1:2008 --matrix 0 --level 0 --first-dst 0
→ protect tally-dump matrix=0 level=0 first_dst=0 items=16
```

### `dual-status` — read 1:1 redundancy state

```
dhs consumer probel-sw08p dual-status 127.0.0.1:2008
→ dual-controller  who=MASTER active=true idle_faulty=false
```

### `maintenance` — send a maintenance message

```
dhs consumer probel-sw08p maintenance 127.0.0.1:2008 --function soft-reset
→ maintenance sent: function=soft-reset matrix=0 level=0
```

`--function`: `hard-reset | soft-reset | clear-protects | database-transfer`.
Fire-and-forget — returns once the frame is ACKed.

### name family — `all-source-names` / `single-source-name` / `all-dest-names` / `single-dest-name` / `all-source-assoc-names` / `single-source-assoc-name`

```
dhs consumer probel-sw08p all-source-names         127.0.0.1:2008 --matrix 0 --level 0 --size 8
dhs consumer probel-sw08p single-source-name       127.0.0.1:2008 --matrix 0 --level 0 --size 8 --src 4
dhs consumer probel-sw08p all-dest-names           127.0.0.1:2008 --matrix 0 --size 8
dhs consumer probel-sw08p single-dest-name         127.0.0.1:2008 --matrix 0 --size 8 --dst 4
dhs consumer probel-sw08p all-source-assoc-names   127.0.0.1:2008 --matrix 0 --size 8
dhs consumer probel-sw08p single-source-assoc-name 127.0.0.1:2008 --matrix 0 --size 8 --src 4
→ source name  matrix=0 level=0 src=4  "SRC 0005"
```

`--size`: on-wire field width `4 | 8 | 12 | 16` chars.

### `update-name` — write labels (fire-and-forget)

```
dhs consumer probel-sw08p update-name 127.0.0.1:2008 --type source --width 8 \
    --matrix 0 --level 0 --first-id 0 --names "CAM-1,CAM-2,CAM-3"
→ update-name sent  type=source width=8 matrix=0 level=0 first_id=0 count=3
```

`--type`: `source | source-assoc | dest-assoc | umd`. `--width`: `4|8|12|16`.
`--names` is comma-separated, applied from `--first-id` upward.

> ⚠ Pushing labels to an Aurora / system-editor-managed controller causes
> a database mismatch when its config editor next connects online.

### `discover` — one-shot read-only probe

```
dhs consumer probel-sw08p discover 127.0.0.1:2008 --matrix 0 --level 0 --size 8
→ dual-status + source names + dest names + tally-dump for (M0, L0)
```

### `watch` — follow async tallies

```
dhs consumer probel-sw08p watch 127.0.0.1:2008 --timeout 3s
→ event  cmd=0x03 payload_len=4     (a tx 003 tally fanned out by the matrix)
```

`--timeout 0` runs until Ctrl-C.

### `bench` — scale benchmark

```
dhs consumer probel-sw08p bench 127.0.0.1:2008 --matrix 0,1 --size 65535 --csv bench.csv --md bench.md
```

Flags: `--phase interrogate|connect|both`, `--matrix CSV`, `--size N`,
`--csv` / `--md`, `--progress N`, `--timeout DUR`, `--skip-warmup`.

### `salvo-connect` — CLI BLOCKED

```
dhs consumer probel-sw08p salvo-connect 127.0.0.1:2008 --matrix 0 --level 0 --src 7 --dsts 0-2 --salvo 5
→ error: --dsts: strconv.ParseUint: parsing "0-2": invalid syntax
```

The global `--dsts` matrix-config flag (parsed before sub-command
dispatch) shadows `salvo-connect`'s own `--dsts`. The salvo path itself is
proven working over TCP by the integration test
[`TestSalvoConnectOnGoThenGo`](../integration/loopback_test.go); the CLI
capture is **pending the flag fix** — no salvo CLI sample is fabricated.
See [verbs.md §10](verbs.md#10-salvo-connect-controller-side-batch--cli-blocked-today).

---

## Protect states

`codec.ProtectState` is a 4-value enum carried in `tx 011 / 013 / 015 / 020`:

| State | Value | Meaning |
|---|---|---|
| `ProtectNone` | 0 | unprotected |
| `ProtectProbel` | 1 | Pro-Bel (standard) protect, owner echoed |
| (master) | 2 | master-override protect (`master-protect`) |

Protect is multi-state by design — never collapse it to locked/unlocked.

---

## Addressing — matrix / level / multiplier

SW-P-08 packs `(matrix, level)` and a 3-bit multiplier into the narrow
general fields per §3.1.2 (4-bit matrix/level + 3-bit DIV-128). When an
address exceeds the general field width the codec auto-escalates to the
extended command form (bit 7 set). Callers never compute byte offsets —
everyone goes through `codec.Foo{...}.Marshal()` / `codec.ParseFoo(...)`.

---

## Raw capture

```
dhs consumer probel-sw08p <verb> 127.0.0.1:2008 [flags] --capture run.jsonl
```

Format: JSONL, one line per wire message (TX + RX, including `DLE ACK` /
`DLE NAK`). Used for unit-test replay and protocol analysis. Every wire
sample in [verbs.md](verbs.md) and [runbook.md](runbook.md) is a real line
from one of these files.

---

## Compliance & known deviations

The plugin carries a `compliance.Profile`. Deviations are absorbed (the
plugin keeps running) and surfaced as named events — never silently
patched.

- **NAK = "command unsupported," not fatal.** A peer NAK that survives 5×
  retry fires an event and the verb continues; it does not crash the
  session.
- **Salvo commit emits `cmd 04 Connected` (issue #92).** §3.2.30 tells
  listeners to track salvo tally from `cmd 122` + `cmd 123` and tells the
  matrix NOT to emit `cmd 04` on the salvo path. Neither Commie nor Lawo
  VSM implement that listener path — both track tally from `cmd 04` — so
  our provider emits one `tx 004` per applied slot and fires
  `probel_salvo_emitted_connected`. This is the documented
  "every shipping controller contradicts the spec" exception (root
  `CLAUDE.md`). See [`../CLAUDE.md`](../CLAUDE.md) "Known deviations".
- **Short tally-dump frames** from some viewer devices are absorbed via
  `compliance.Profile`, event fired, no silent workaround.

---

## Error reference

| Class | Example | Recovery |
|---|---|---|
| Transport | connection refused / reset | check the matrix is up + reachable on :2008 |
| Frame | checksum / BTC mismatch | line-quality issue; dissector flags it |
| Link | `DLE NAK` after 5× retry | command unsupported by the peer (compliance event) |
| Validation | `--matrix out of range (0-255)`, `--device out of range (0-1023)` | fix the flag value |
| CLI flag | `salvo-connect ... --dsts 0-2` collision | pending fix; see [salvo-connect](#salvo-connect--cli-blocked) |

---

## Test devices

| Device | Notes |
|---|---|
| Loopback emulator (our provider) | `dhs producer probel-sw08p serve --tree internal/probel-sw08p/testdata/exports/matrix_tree.json --port 2008` — CI-safe, 16×16 one-to-N at M0/L0 |
| Commie.exe | full SW-P-08 receiver; UI is 1-based across the board ([`../CLAUDE.md`](../CLAUDE.md) Testbed) |
| Lawo VSM Studio | controller mode (drives connect + salvos) and server mode (read-only 8×8) |
| Real codeowner device | live confirmation, lab network (VPN-only), `PROBEL_SW08P_TEST_HOST` |
