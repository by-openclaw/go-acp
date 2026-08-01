# Probel SW-P-08 — verbs & configuration reference

Per-connector verb reference, same section order as every other connector.
Samples are **real captures** against a loopback provider built from the
committed fixture (`internal/probel-sw08p/testdata/exports/matrix_tree.json`,
a single level-scoped 16×16 one-to-N matrix at matrix 0 / level 0). Scope
is **SW-P-08 Issue 30** (ADR-0025); the loopback emulator is our own
provider serving the fixture tree, used as the CI-safe oracle.

Wire format: [`../CLAUDE.md`](../CLAUDE.md). Verbs (consumer): `discover
interrogate connect watch maintenance dual-status tally-dump
protect-interrogate protect-connect protect-disconnect protect-name
protect-dump master-protect all-source-names single-source-name
all-dest-names single-dest-name all-source-assoc-names
single-source-assoc-name update-name bench salvo-connect`.

Every frame quoted below is the lowercase-hex `hex` field from a real
`--capture` JSONL line (`{ts, proto, dir, hex, len}`), captured exactly as
[the runbook describes](runbook.md#capture-procedure-how-the-samples-in-these-docs-were-made).

---

## 1. Transport configs

SW-P-08 is **TCP with DLE/STX framing** (§2). Default port **2008**. No UDP.

```
DLE STX  <data...>  <btc>  <cksum>  DLE ETX
10  02   ........   xx     yy       10  03
```

- `<data>` = `<cmd> <addressing+payload>` per the command catalogue.
- `<btc>` = byte count of `<data>` (the bytes between STX and BTC).
- `<cksum>` = 8-bit two's-complement of `(data || btc)` — `0x100 − (Σ & 0xFF)`.
- **DLE-stuffing**: any `0x10` byte inside `<data>…<cksum>` is doubled to
  `10 10`. Framing DLEs (STX / ETX / ACK / NAK) are never stuffed.
- Every good frame → peer replies `DLE ACK` (`10 06`). Every bad frame →
  `DLE NAK` (`10 15`). ACK timeout 1 s, 5 retries (§2).
- DATA soft cap 128 bytes, hard cap 255.

```
dhs consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
dhs producer probel-sw08p serve --tree matrix.json --host 0.0.0.0 --port 2008
```

> **Checksum worked example** (real `interrogate` TX, captured below):
> data+btc = `01 00 00 05 04`, Σ = `0x0A`, cksum = `0x100 − 0x0A = 0xF6` —
> matches the `f6` byte on the wire.

## 2. ACK / NAK & retries (§2 transmission protocol)

ACK handling lives in **§2**, not §3.5. Every command the consumer sends
is answered at the link layer by a two-byte `DLE ACK` (`10 06`) before any
application-layer reply arrives. A `DLE NAK` (`10 15`) means "frame
rejected" — the consumer retries (5×, 1 s ACK timeout). A NAK from a peer
that survives retries is surfaced as a compliance event ("command
unsupported"), **not** a fatal error.

A `DLE ACK` is frame-level only — it does **not** imply the peer's
application layer processed the command. Infer "supported" only from a
follow-up app-layer reply (e.g. `tx 003` after `rx 001`) or a visible
state change.

Captured ACK after an `rx 001` interrogate (real):

```json
{"dir":"tx","hex":"10020100000504f61003","len":10}   ← rx 001 Interrogate
{"dir":"rx","hex":"1006","len":2}                     ← DLE ACK
{"dir":"rx","hex":"1002030000050005f31003","len":11}  ← tx 003 Tally reply
{"dir":"tx","hex":"1006","len":2}                     ← our DLE ACK of the reply
```

## 3. Logging & severity

Logs go to **stderr**, data to **stdout** (`2>` captures logs, `1>` the
result). Each TX/RX frame is logged on stderr as a space-separated
lowercase-hex line:

```
probel TX ... hex=10 02 01 01 00 05 0c 03 1f 10 03
```

| Side | Flag | Values |
|---|---|---|
| consumer | (default) | INFO to stderr; per-frame hex at INFO |
| producer | `--log-level` / `--log-format` | `debug \| info \| warn \| error` / `text \| json` |

```
dhs consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5 2>run.log
dhs producer probel-sw08p serve --tree matrix.json --port 2008 --log-format json
```

## 4. discover / interrogate / tally-dump (read)

**discover** — one-shot read-only probe: dual-status + source names + dest
names + tally-dump on `(matrix, level)`. Useful when pointing at an
unknown matrix (e.g. VSM acting as SW-P-08 server).

```
$ dhs consumer probel-sw08p discover 127.0.0.1:12008 --matrix 0 --level 0 --size 8
=== discover 127.0.0.1:12008 (M=0 L=0 size=8) ===
dual-status  master_active=true active=true idle_faulty=false
source names  matrix=0 level=0 count=16 (first=0)
  src=0  "SRC 0001"
  ...
dest names  matrix=0 count=16 (first=0)
  dst=0  "DST 0001"
  ...
tally dump (byte)  matrix=0 level=0 first=0 count=16
  dst=5 → src=12
```

**interrogate** — read the current source on one `(matrix, level, dst)`:

```
$ dhs consumer probel-sw08p interrogate 127.0.0.1:12008 --matrix 0 --level 0 --dst 5
crosspoint tally  matrix=0 level=0 dst=5 → src=12
```

Real frames (`rx 001` → `tx 003`):

```json
{"dir":"tx","hex":"10020100000504f61003","len":10}    ← rx 001  cmd=01 mtx=00 lvl=00 dst=05 btc=04 cks=f6
{"dir":"rx","hex":"1002030000050005f31003","len":11}  ← tx 003  cmd=03 mtx=00 lvl=00 dst=05 src=05(*) cks=f3
```

(*) the `src` byte here is the multiplier+source packing per §3.1.2; the
decoded value is reported on stdout (`→ src=12` after the connect in §5).

**tally-dump** — dump every crosspoint on `(matrix, level)`. Byte form
(`tx 022`) for ≤ 256 dsts, word form (`tx 023`) above:

```
$ dhs consumer probel-sw08p tally-dump 127.0.0.1:12008 --matrix 0 --level 0
tally-dump (byte) matrix=0 level=0 first_dst=0 tallies=16
  dst=5 → src=12
```

Real `rx 021` request → `tx 022` byte-dump (note the DLE-stuffed `10 10`
where a real `0x10` data byte appears, and `0c` = src 12 at the dst-5 slot):

```json
{"dir":"tx","hex":"1002150002e91003","len":8}
{"dir":"rx","hex":"1002160010100000000000000c0000000000000000000014ba1003","len":27}
```

## 5. connect / protect / master-protect (write)

**connect** — route a source to a destination on `(matrix, level)`. The
matrix replies `tx 004 Crosspoint Connected` to the originator:

```
$ dhs consumer probel-sw08p connect 127.0.0.1:12008 --matrix 0 --level 0 --dst 5 --src 12
crosspoint connected  matrix=0 level=0 dst=5 src=12
```

Real `rx 002` → `tx 004` (checksum check: data+btc `02 00 00 05 0c 05`,
Σ=`0x18`, cksum=`0x100−0x18=0xE8` — matches the `e8` byte):

```json
{"dir":"tx","hex":"1002020000050c05e81003","len":11}   ← rx 002 Connect
{"dir":"rx","hex":"1002040000050c05e61003","len":11}   ← tx 004 Connected
```

**protect-connect / protect-interrogate / protect-disconnect** — the
protect lifecycle on `(matrix, level, dst)` for a `--device`. Protect is
multi-state (see [protect states](consumer.md#protect-states)), not a
plain lock bit.

```
$ dhs consumer probel-sw08p protect-connect 127.0.0.1:12008 --matrix 0 --level 0 --dst 4 --device 9
protect connected  matrix=0 level=0 dst=4 device=9 state=1
```

Real `rx 012` → `tx 013` (state 1 = Pro-Bel protect):

```json
{"dir":"tx","hex":"10020c0000040905e21003","len":11}     ← rx 012 Protect Connect
{"dir":"rx","hex":"10020d000100040906df1003","len":12}   ← tx 013 Protect Connected  state=01
```

**master-protect** — master-override protect connect (`rx 029` → `tx 013`,
state 2 = master protect):

```
$ dhs consumer probel-sw08p master-protect 127.0.0.1:12008 --matrix 0 --level 0 --dst 6 --device 3
master-protect connected  matrix=0 level=0 dst=6 device=3 state=2
```

```json
{"dir":"tx","hex":"10021d00000006000307d31003","len":13}  ← rx 029 Master Protect Connect
{"dir":"rx","hex":"10020d000200060306e21003","len":12}    ← tx 013 Protect Connected  state=02
```

## 6. names — source / dest / association labels

Name-family commands are level-scoped for source names, matrix-scoped for
dest/source associations. `--size` selects the on-wire field width (4 | 8
| 12 | 16 chars per §3.2). Labels are space/NUL-padded to that width.

| Verb | rx cmd | tx cmd | Scope |
|---|---|---|---|
| `all-source-names` | 100 | 106 | matrix + level |
| `single-source-name` | 101 | 106 | matrix + level + src |
| `all-dest-names` | 102 | 107 | matrix |
| `single-dest-name` | 103 | 107 | matrix + dst |
| `all-source-assoc-names` | 114 | 116 | matrix |
| `single-source-assoc-name` | 115 | 116 | matrix + src |

```
$ dhs consumer probel-sw08p all-source-names 127.0.0.1:12008 --matrix 0 --level 0 --size 8
source names  matrix=0 level=0 size=8 first=0 count=16
  src=0  "SRC 0001"
  src=4  "SRC 0005"
```

Real `rx 100` → `tx 106` (the reply carries 16 × 8-char names; trimmed):

```json
{"dir":"tx","hex":"100264000103981003","len":9}
{"dir":"rx","hex":"10026a000100001010535243203030303153524320...86361003","len":141}
```

Single source name (`rx 101` → `tx 106`, one 8-char name `"SRC 0005"`):

```json
{"dir":"tx","hex":"1002650001000405911003","len":11}
{"dir":"rx","hex":"10026a000100040153524320303030350eb51003","len":20}
```

## 7. update-name (write labels, fire-and-forget)

`update-name` issues `rx 117 Update Name Request` — push one or more
source / source-assoc / dest-assoc / UMD labels starting at `--first-id`.
§3.2.26: the matrix sends **no reply**, so the call returns as soon as the
frame is ACKed.

```
$ dhs consumer probel-sw08p update-name 127.0.0.1:12008 --type source --width 8 \
    --matrix 0 --level 0 --first-id 0 --names "CAM-1,CAM-2,CAM-3"
update-name sent  type=source width=8 matrix=0 level=0 first_id=0 count=3
```

Real `rx 117` (3 × 8-char names, space-padded — the
`43414d2d3120202043414d2d322020...` payload is `"CAM-1   CAM-2   CAM-3   "`)
followed only by the link `DLE ACK`, no app reply:

```json
{"dir":"tx","hex":"1002750001000000000343414d2d3120202043414d2d3220202043414d2d3320202020b71003","len":38}
{"dir":"rx","hex":"1006","len":2}
```

> ⚠ Pushing labels to an Aurora / system-editor-managed controller causes
> a database mismatch the next time its config editor connects online. Use
> deliberately (carried from the consumer doc-comment).

## 8. watch — async tally fan-out

`watch` subscribes to every async frame the matrix emits until the timeout
(or Ctrl-C). The headline use is observing `tx 003 Crosspoint Tally`
broadcasts the matrix fans out to all sessions when *another* controller
mutates a crosspoint (§3.2.3 "issued on all ports").

```
$ dhs consumer probel-sw08p watch 127.0.0.1:12008 --timeout 3s
event  cmd=0x03 payload_len=4
```

Real fan-out frame observed by `watch` while a second session ran
`connect --dst 8 --src 3` (captured from the consumer's stderr hex log):

```
probel RX ... hex=10 02 03 00 00 08 03 05 ed 10 03   ← tx 003 Tally  dst=08 src=03
```

## 9. maintenance / dual-status / protect-dump / protect-name

**maintenance** — send a maintenance message (fire-and-forget, `rx 007`):

```
$ dhs consumer probel-sw08p maintenance 127.0.0.1:12008 --function soft-reset
maintenance sent: function=soft-reset matrix=0 level=0
```

`--function`: `hard-reset | soft-reset | clear-protects | database-transfer`.
Real `rx 007` + ACK only (no reply):

```json
{"dir":"tx","hex":"1002070102f61003","len":8}
{"dir":"rx","hex":"1006","len":2}
```

**dual-status** — read 1:1 redundancy state (`rx 008` → `tx 009`):

```
$ dhs consumer probel-sw08p dual-status 127.0.0.1:12008
dual-controller  who=MASTER active=true idle_faulty=false
```

```json
{"dir":"tx","hex":"10020801f71003","len":7}
{"dir":"rx","hex":"100209020003f21003","len":9}
```

**protect-dump** — dump every protect on `(matrix, level)` from `--first-dst`
(`rx 019` → `tx 020`). **protect-name** — resolve a device id to its name
(`rx 017` → `tx 018`):

```
$ dhs consumer probel-sw08p protect-name 127.0.0.1:12008 --device 9
device 9 name="DEV 0009"
```

```json
{"dir":"tx","hex":"100211000903e31003","len":9}
{"dir":"rx","hex":"100212000944455620303030390b121003","len":17}   ← tx 018 "DEV 0009"
```

## 10. salvo-connect (controller-side batch)

`salvo-connect` mimics the VSM batch-connect flow: N × `rx 120 Connect-On-Go
Salvo` (stage), then `rx 121 Go Salvo` op=set (fire), then op=clear (wipe).

```
dhs consumer probel-sw08p salvo-connect 127.0.0.1:2008 --matrix 0 --level 0 --src 7 --dsts 10-11 --salvo 5
```

`--dsts` accepts a CSV or `N-M` range; every dst is routed to the single
`--src` (fan-out). The verb owns its own `--dsts` and `--level`: the
global matrix-config extractor (`extractMatrixConfigFlags`) is told to
skip them when `probelSubcommand(args) == "salvo-connect"`
([`cmd_probel.go`](../../../cmd/dhs/cmd_probel.go)), so `--dsts 0-2`
reaches the verb intact instead of being eaten by the global uint parser.
Regression-pinned by
[`cmd_probel_salvo_dsts_test.go`](../../../cmd/dhs/cmd_probel_salvo_dsts_test.go)
(both SW-P-08 and SW-P-02 dispatchers; the global uint `--dsts` bootstrap
still works for non-salvo verbs).

The salvo path is also proven working over a real TCP round-trip by
the loopback integration test
[`TestSalvoConnectOnGoThenGo`](../integration/loopback_test.go) (stage 3
slots → fire → every slot reads back via interrogate, `tx 123 Go-Done
status=Set`). The wire shape is `rx 120` × N → `tx 122` ack × N →
`rx 121` set → `tx 123` go-done. A `--capture`'d CLI trace of this verb is
in [`consumer.md`](consumer.md#salvo-connect--controller-side-batch-route).

> Spec-vs-reality note: §3.2.30 says the matrix emits **no** `cmd 04` on
> the salvo path. Neither Commie nor Lawo VSM implement that listener
> path — both track tally from `cmd 04` — so our provider emits one
> `tx 004 Connected` per applied slot and fires
> `probel_salvo_emitted_connected`. See [`../CLAUDE.md`](../CLAUDE.md)
> "Known deviations from spec".

## 11. bench — scale benchmark

`bench` holds one persistent TCP connection and runs interrogate-all +
connect-all across the dst range on every `--matrix` id. Worst-case scope
is 2 matrices × 65535 × 1 level (root `CLAUDE.md` scale targets).

```
$ dhs consumer probel-sw08p bench 127.0.0.1:12008 --matrix 0 --size 16 --phase both --progress 0
=== Bench summary ===
interrogate  n=16 errors=0 wall=11ms  min=0s p50=571.4µs p95=655.2µs p99=655.2µs max=5.5116ms  mean-op=716.431µs
connect      n=16 errors=0 wall=7ms   min=0s p50=556.4µs p95=604.3µs p99=604.3µs max=646µs     mean-op=430.437µs
overall wall: 18ms  (matrices=[0] size=16)
```

Flags: `--phase interrogate|connect|both`, `--matrix 0,1`, `--size N`,
`--csv FILE`, `--md FILE`, `--progress N`, `--timeout DUR`,
`--skip-warmup`. Connect maps `src = 1 + dst/16` (16 dsts fan into 1 src).

## 12. Wireshark

Dissector: [`../wireshark/dhs_probel_sw08p.lua`](../wireshark/dhs_probel_sw08p.lua)
— decodes §2 framing + DLE-stuffing, `DLE ACK`/`DLE NAK` pseudo-frames,
checksum + BTC validation (expert info), and per-cmd decode for the
high-traffic bytes. Pure arithmetic (no Lua 5.3 bitops) so it loads on
Wireshark 4.x (Lua 5.2) and 5.x.

| OS | Plugin dir |
|---|---|
| Windows | `%APPDATA%\Wireshark\plugins\` |
| macOS / Linux | `~/.local/lib/wireshark/plugins/` |

```
#   display filter:  dhs_probel_sw08p
#   port decode:     tcp.port == 2008
tshark -r capture.pcapng -O dhs_probel_sw08p -Y dhs_probel_sw08p
```

## 13. Ansible (the exclusive integration / deploy driver — no .ps1)

Integration drives the consumer against the loopback emulator (always) and
optionally a live SW-P-08 matrix (gated on `PROBEL_SW08P_TEST_HOST`):
[`ansible/playbooks/probel-sw08p-integration.yml`](../../../ansible/playbooks/probel-sw08p-integration.yml).

```yaml
# loopback only (no VPN, CI-safe):
#   ansible-playbook -i inventory/hosts.ini playbooks/probel-sw08p-integration.yml
# plus live matrix (lab, VPN):
#   PROBEL_SW08P_TEST_HOST=10.100.0.42 ansible-playbook ... probel-sw08p-integration.yml
- name: "loopback — go test -tags integration (provider emulator in-process)"
  ansible.builtin.command:
    chdir: "{{ repo_root }}"
    argv: [go, test, -tags, integration, "./internal/probel-sw08p/integration/", -count=1]
  register: sw08p_loopback
  changed_when: false
```

Both tiers re-run the **same** Go `-tags integration` body; the external
tier just sets the env var. `changed_when: false` makes the
read-only / idempotent property explicit (ADR-0025 deliverable 3 + 6).

## 14. See also

- [`../CLAUDE.md`](../CLAUDE.md) — §2 framing, DLE-stuffing, level-scoping, command catalogue, deviations
- [`./README.md`](./README.md) · [`./consumer.md`](./consumer.md) · [`./provider.md`](./provider.md) · [`./runbook.md`](./runbook.md)
- [`../../../docs/adr/0025-per-connector-definition-of-done.md`](../../../docs/adr/0025-per-connector-definition-of-done.md)
