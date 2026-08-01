# Probel SW-P-08 — operational runbook

> Source-of-truth for verb-by-verb validation against the loopback
> emulator. Every wire sample referenced here is a **real captured frame**
> (see [Capture procedure](#capture-procedure-how-the-samples-in-these-docs-were-made));
> none is hand-written.

## Scope

| Dimension | Coverage |
|---|---|
| **Protocol** | SW-P-08 Issue 30 (per [`../CLAUDE.md`](../CLAUDE.md)); level-scoped `<matrix, level, dst, src>` |
| **Roles** | dhs as **consumer** (outbound) and **provider** (inbound); both exercised against the loopback emulator |
| **Producer source** | committed fixture [`../testdata/exports/matrix_tree.json`](../testdata/exports/matrix_tree.json) — one 16×16 one-to-N matrix at matrix 0 / level 0 |
| **OS** | Windows 11 (primary host, PowerShell); Linux LXCs for Ansible parity |
| **Out of scope** | live-matrix Tier-3 (VPN-gated) |

## Setup — build + serve

```powershell
git switch main
git pull --ff-only origin main
go build -o bin\dhs.exe ./cmd/dhs

.\bin\dhs.exe producer probel-sw08p serve `
    --tree internal\probel-sw08p\testdata\exports\matrix_tree.json `
    --host 127.0.0.1 `
    --port 2008 `
    --log-level info
```

Expected log:

```
level=INFO msg="probel provider listening" addr=127.0.0.1:2008 matrices=1
```

## Tree shape (fixture)

```
router (oid 1)
└── matrix-0 (oid 1.1) — oneToN, linear, 16×16, level-0 labels
```

The fixture declares no explicit labels, so the provider synthesises
positional defaults (`SRC 0001…`, `DST 0001…`, `DEV 0009`).

## Verb catalogue

| Verb | rx → tx | Status | Returns |
|---|---|---|---|
| `interrogate` | 001 → 003 | ✅ | current source on (matrix, level, dst) |
| `connect` | 002 → 004 | ✅ | confirmed crosspoint |
| `tally-dump` | 021 → 022/023 | ✅ | every crosspoint on (matrix, level) |
| `protect-interrogate` | 010 → 011 | ✅ | protect state + owner |
| `protect-connect` | 012 → 013 | ✅ | protect state (1) |
| `protect-disconnect` | 014 → 015 | ✅ | protect state (0) |
| `master-protect` | 029 → 013 | ✅ | protect state (2) |
| `protect-name` | 017 → 018 | ✅ | 8-char owner name |
| `protect-dump` | 019 → 020 | ✅ | protect table |
| `dual-status` | 008 → 009 | ✅ | master/slave/active/idle-faulty |
| `maintenance` | 007 → — | ✅ | fire-and-forget |
| `all-source-names` | 100 → 106 | ✅ | source label table |
| `single-source-name` | 101 → 106 | ✅ | one source label |
| `all-dest-names` | 102 → 107 | ✅ | dest assoc label table |
| `single-dest-name` | 103 → 107 | ✅ | one dest assoc label |
| `all-source-assoc-names` | 114 → 116 | ✅ | source assoc table |
| `single-source-assoc-name` | 115 → 116 | ✅ | one source assoc label |
| `update-name` | 117 → — | ✅ | fire-and-forget write |
| `discover` | composite | ✅ | dual-status + names + tally-dump |
| `watch` | passive | ✅ | async tally feed |
| `bench` | 001/002 ×N | ✅ | per-cmd latency CSV/MD |
| `salvo-connect` | 120/121 → 122/123 | ✅ | build ×N → go set → go clear; verb owns `--dsts`/`--level` |

Legend: ✅ working.

---

## Capture procedure (how the samples in these docs were made)

All wire samples in [verbs.md](verbs.md) / [consumer.md](consumer.md) were
captured against the loopback emulator. PowerShell host (per repo memory).

```powershell
# 1. Build a unique binary (avoid clobbering parallel agents)
go build -o dhs_sw08p.exe ./cmd/dhs

# 2. Start the loopback provider on a unique port 12008
.\dhs_sw08p.exe producer probel-sw08p serve `
    --tree internal\probel-sw08p\testdata\exports\matrix_tree.json `
    --host 127.0.0.1 --port 12008 --log-level info     # background

# 3. Drive each verb with --capture (JSONL {ts,proto,dir,hex,len} per frame)
$T = "127.0.0.1:12008"
.\dhs_sw08p.exe consumer probel-sw08p interrogate $T --matrix 0 --level 0 --dst 5  --capture cap_interrogate.jsonl
.\dhs_sw08p.exe consumer probel-sw08p connect     $T --matrix 0 --level 0 --dst 5 --src 12 --capture cap_connect.jsonl
# … one --capture file per verb …

# 4. Tear down + delete scratch (commit only the 5 .md files)
Get-Process dhs_sw08p | Stop-Process -Force
Remove-Item dhs_sw08p.exe, cap_*.jsonl
```

### Checksum cross-check (proves the hex is real)

The captured `interrogate` TX frame is `10 02 01 00 00 05 04 f6 10 03`:

| Bytes | Meaning |
|---|---|
| `10 02` | DLE STX (SOM) |
| `01 00 00 05` | data: cmd 001, matrix 0, level 0, dst 5 |
| `04` | BTC = 4 data bytes |
| `f6` | checksum = `0x100 − ((01+00+00+05+04) & 0xFF)` = `0x100 − 0x0A` = **0xF6** ✓ |
| `10 03` | DLE ETX (EOM) |

The `connect` TX frame `10 02 02 00 00 05 0c 05 e8 10 03` checks the same
way: Σ(`02 00 00 05 0c 05`) = `0x18`, cksum = `0x100 − 0x18` = **0xE8** ✓.

---

## 1. `interrogate`

### Happy

```powershell
.\bin\dhs.exe consumer probel-sw08p interrogate 127.0.0.1:2008 --matrix 0 --level 0 --dst 5
# crosspoint tally  matrix=0 level=0 dst=5 → src=12
```

Real frames: `{"dir":"tx","hex":"10020100000504f61003"}` →
`{"dir":"rx","hex":"1006"}` (ACK) →
`{"dir":"rx","hex":"1002030000050005f31003"}` (tx 003).

### Errors

| Trigger | Command | Result | Exit |
|---|---|---|---|
| missing host | `interrogate --matrix 0` | `missing <host:port>` | non-0 |
| matrix out of range | `interrogate ... --matrix 999` | `--matrix out of range (0-255)` | non-0 |
| connection refused | `interrogate 127.0.0.1:1 ...` | transport error | non-0 |

---

## 2. `connect`

### Happy

```powershell
.\bin\dhs.exe consumer probel-sw08p connect 127.0.0.1:2008 --matrix 0 --level 0 --dst 5 --src 12
# crosspoint connected  matrix=0 level=0 dst=5 src=12
```

Real frames: `{"dir":"tx","hex":"1002020000050c05e81003"}` →
`{"dir":"rx","hex":"1002040000050c05e61003"}` (tx 004). A follow-up
`interrogate` reads back `→ src=12`, proving the route stuck.

### Errors

| Trigger | Command | Result |
|---|---|---|
| src out of range | `connect ... --src 99999` | `--src out of range (0-65535)` |
| dst > targetCount | `connect ... --dst 99` (16×16 fixture) | provider rejects; consumer surfaces the error |

---

## 3. `tally-dump`

### Happy

```powershell
.\bin\dhs.exe consumer probel-sw08p tally-dump 127.0.0.1:2008 --matrix 0 --level 0
# tally-dump (byte) matrix=0 level=0 first_dst=0 tallies=16
#   dst=5 → src=12
```

Real `rx 021` → `tx 022` byte-dump (DLE-stuffed `10 10` for a real `0x10`
data byte): `{"dir":"rx","hex":"1002160010100000000000000c00...14ba1003"}`.
Byte form for ≤ 256 dsts; word form (`tx 023`) above.

---

## 4. protect — `protect-connect` / `protect-interrogate` / `protect-disconnect`

### Happy (full lifecycle)

```powershell
.\bin\dhs.exe consumer probel-sw08p protect-connect    127.0.0.1:2008 --matrix 0 --level 0 --dst 4 --device 9
# protect connected     matrix=0 level=0 dst=4 device=9 state=1
.\bin\dhs.exe consumer probel-sw08p protect-disconnect 127.0.0.1:2008 --matrix 0 --level 0 --dst 4 --device 9
# protect disconnected  matrix=0 level=0 dst=4 device=9 state=0
```

Real `rx 012` → `tx 013`: `{"dir":"tx","hex":"10020c0000040905e21003"}` →
`{"dir":"rx","hex":"10020d000100040906df1003"}` (state byte `01`).

### Errors

| Trigger | Command | Result |
|---|---|---|
| device out of range | `protect-connect ... --device 9999` | `--device out of range (0-1023)` |

---

## 5. `master-protect`

```powershell
.\bin\dhs.exe consumer probel-sw08p master-protect 127.0.0.1:2008 --matrix 0 --level 0 --dst 6 --device 3
# master-protect connected  matrix=0 level=0 dst=6 device=3 state=2
```

Real `rx 029` → `tx 013` (state `02`):
`{"dir":"tx","hex":"10021d00000006000307d31003"}` →
`{"dir":"rx","hex":"10020d000200060306e21003"}`.

---

## 6. `protect-name` / `protect-dump`

```powershell
.\bin\dhs.exe consumer probel-sw08p protect-name 127.0.0.1:2008 --device 9
# device 9 name="DEV 0009"
```

Real `rx 017` → `tx 018`: `{"dir":"tx","hex":"100211000903e31003"}` →
`{"dir":"rx","hex":"100212000944455620303030390b121003"}` (`"DEV 0009"`).

---

## 7. `dual-status`

```powershell
.\bin\dhs.exe consumer probel-sw08p dual-status 127.0.0.1:2008
# dual-controller  who=MASTER active=true idle_faulty=false
```

Real `rx 008` → `tx 009`: `{"dir":"tx","hex":"10020801f71003"}` →
`{"dir":"rx","hex":"100209020003f21003"}`.

> The loopback emulator is single-controller by construction (Master /
> active / idle OK). A live redundant pair may legitimately report
> Slave-active / idle-faulty.

---

## 8. `maintenance`

```powershell
.\bin\dhs.exe consumer probel-sw08p maintenance 127.0.0.1:2008 --function soft-reset
# maintenance sent: function=soft-reset matrix=0 level=0
```

Fire-and-forget (§3.2): `{"dir":"tx","hex":"1002070102f61003"}` →
`{"dir":"rx","hex":"1006"}` (ACK only, no app reply).

### Errors

| Trigger | Result |
|---|---|
| `--function bogus` | `unknown --function "bogus"` |

---

## 9. name family

```powershell
.\bin\dhs.exe consumer probel-sw08p all-source-names         127.0.0.1:2008 --matrix 0 --level 0 --size 8
.\bin\dhs.exe consumer probel-sw08p single-source-name       127.0.0.1:2008 --matrix 0 --level 0 --size 8 --src 4
.\bin\dhs.exe consumer probel-sw08p all-dest-names           127.0.0.1:2008 --matrix 0 --size 8
.\bin\dhs.exe consumer probel-sw08p single-source-assoc-name 127.0.0.1:2008 --matrix 0 --size 8 --src 4
# source name  matrix=0 level=0 src=4  "SRC 0005"
```

Real `rx 101` → `tx 106` (one 8-char name `"SRC 0005"`):
`{"dir":"tx","hex":"1002650001000405911003"}` →
`{"dir":"rx","hex":"10026a000100040153524320303030350eb51003"}`.

`--size` selects 4 / 8 / 12 / 16-char wire field width.

---

## 10. `update-name`

```powershell
.\bin\dhs.exe consumer probel-sw08p update-name 127.0.0.1:2008 --type source --width 8 `
    --matrix 0 --level 0 --first-id 0 --names "CAM-1,CAM-2,CAM-3"
# update-name sent  type=source width=8 matrix=0 level=0 first_id=0 count=3
```

Real `rx 117` (space-padded names, fire-and-forget — ACK only):
`{"dir":"tx","hex":"1002750001000000000343414d2d31202020...b71003"}` →
`{"dir":"rx","hex":"1006"}`. A follow-up `all-source-names` confirms
`src=0 "CAM-1"` stuck.

> ⚠ Pushing labels to an Aurora / system-editor-managed controller causes
> a database mismatch when its config editor next connects. Use deliberately.

---

## 11. `discover`

```powershell
.\bin\dhs.exe consumer probel-sw08p discover 127.0.0.1:2008 --matrix 0 --level 0 --size 8
# dual-status + source names + dest names + tally-dump for (M0, L0)
```

Composite of `rx 008` + `rx 100` + `rx 102` + `rx 021`. 15 s timeout.

---

## 12. `watch`

```powershell
.\bin\dhs.exe consumer probel-sw08p watch 127.0.0.1:2008 --timeout 3s
# event  cmd=0x03 payload_len=4
```

While a second session ran `connect --dst 8 --src 3`, `watch` observed the
real fanned-out `tx 003 Tally` frame `10 02 03 00 00 08 03 05 ed 10 03`
(captured from the consumer's stderr hex log). `--timeout 0` = run until
Ctrl-C.

---

## 13. `bench`

```powershell
.\bin\dhs.exe consumer probel-sw08p bench 127.0.0.1:2008 --matrix 0 --size 16 --phase both --progress 0
# === Bench summary ===
# interrogate  n=16 errors=0 wall=11ms  p50=571.4µs p95=655.2µs p99=655.2µs max=5.5116ms
# connect      n=16 errors=0 wall=7ms   p50=556.4µs p95=604.3µs p99=604.3µs max=646µs
# overall wall: 18ms  (matrices=[0] size=16)
```

(Real numbers from the loopback run; absolute latency is host-dependent.)
One persistent TCP connection for the whole run. `--csv` / `--md` write
per-op rows + a summary table.

---

## salvo-connect — controller-side batch

```powershell
.\bin\dhs.exe consumer probel-sw08p salvo-connect 127.0.0.1:2008 --matrix 0 --level 0 --src 7 --dsts 10-11 --salvo 5
```

Stages one `rx 120` per dst (each acked by `tx 122`), fires with
`rx 121` op=set (`tx 123` Go-Done status=Set), then op=clear by default.
`--dsts` takes a CSV or `N-M` range routed to the single `--src` (fan-out).
The verb owns its `--dsts` / `--level`; `probelSubcommand` in
[`cmd_probel.go`](../../../cmd/dhs/cmd_probel.go) makes the global
matrix-config extractor skip them for this subcommand, so `--dsts 0-2`
reaches the verb — pinned by
[`cmd_probel_salvo_dsts_test.go`](../../../cmd/dhs/cmd_probel_salvo_dsts_test.go).
The salvo path is proven working over a real TCP round-trip by
[`TestSalvoConnectOnGoThenGo`](../integration/loopback_test.go) (stage 3
slots via `rx 120` → fire via `rx 121` → `tx 123` Go-Done status=Set →
every slot reads back). Add `--capture salvo.jsonl` for a wire trace; the
annotated trace is in
[`consumer.md`](consumer.md#salvo-connect--controller-side-batch-route).

---

## Integration & idempotency (ADR-0025 deliverables 3 + 6)

Driven by Ansible — no PowerShell `.ps1`. The play runs the Go
`-tags integration` body twice-safe (the provider is in-process and torn
down by the test, so net change = 0 → run-twice = same result;
`changed_when: false`).

```bash
# loopback only (CI-safe, no VPN):
ansible-playbook -i inventory/hosts.ini playbooks/probel-sw08p-integration.yml

# plus a live SW-P-08 matrix (lab, VPN):
PROBEL_SW08P_TEST_HOST=10.100.0.42 \
  ansible-playbook -i inventory/hosts.ini playbooks/probel-sw08p-integration.yml
```

Go-only equivalents:

```powershell
go test ./internal/probel-sw08p/...                              # unit
go test -tags integration ./internal/probel-sw08p/integration/  # loopback round-trips
```

---

## Cleanup

```powershell
# Preferred: Ctrl-C in the producer window (sessions get a clean shutdown).
# If detached:
Get-Process dhs* | Where-Object { $_.Path -like '*dhs*' } | Stop-Process -Force
```

> ⚠ When capturing samples, build a **uniquely named** binary
> (`dhs_sw08p.exe`) and use a **unique port** (e.g. 12008) so parallel
> agents / a long-running default-port producer aren't clobbered. Delete
> the scratch binary + `cap_*.jsonl` afterward — commit only the docs.
