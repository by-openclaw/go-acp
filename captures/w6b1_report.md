# W6-B1 — live interop with Lawo VSM Studio (2026-04-23)

Session: validate DHS consumer + provider against a real VSM at 64×64
scale. Two independent roles tested, not a full gateway passthrough.

## Setup

| role        | endpoint                   | tree                                   |
|-------------|----------------------------|----------------------------------------|
| DHS consumer| → 10.6.239.155:7800 (VSM)  | —                                      |
| DHS provider| listen 10.6.239.113:2008   | `interop_1mtx_64_1lvl.json` (1×64×64×1)|
| VSM         | 10.6.239.155 (server mode) | internal 64×64                         |

Provider built from `feat/probel-scaffold` HEAD with `--metrics-addr :9100
--log-format json`.

## Consumer → VSM (outbound)

| exchange                      | result                                        |
|-------------------------------|-----------------------------------------------|
| TCP connect + SW-P-08 §2 hello| clean, DLE ACK on every framed message        |
| rx 008 Dual-Ctl-Status Request| VSM server mode **does not reply** (known; memory-logged). Consumer ctx-deadline-exceeds cleanly, no retries. |
| rx 021 Tally-Dump Request     | VSM → 1× tx 022 byte-form, 64 dsts, all src=1 |
| rx 100 All-Source-Names (12ch)| VSM → 7× tx 106 pages, 64 names (`src_01 - v` … `src_64 - v`), zero decode errors |
| **bench**: 64× rx 001 Interrogate | wall **386 ms**, p50 **5.5 ms**, p95 **9.8 ms**, p99 **11.8 ms**, max 15.7 ms, **0 errors / 0 NAKs / 0 timeouts / 0 retries** |

Session summary line from the bench run:

```
uptime=408ms  rx=187/1465B  tx=187/895B  errs=decode:0 nak:0 to:0 retry:0 rec:0
```

rx count (187) > n (64) because VSM broadcasts async tx 003 Tally and
tx 004 Connected events from other in-flight controllers while we
interrogate — our Subscribe listeners see them alongside our replies.

## Provider ← VSM (inbound, unsolicited)

VSM connected inbound within 3 s of provider startup (before we even
dialled out). Snapshot after **3 m 22 s** of uptime:

| metric            | value      |
|-------------------|-----------:|
| Rx frames         | 13 317     |
| Tx frames         | 12 997     |
| Rx bytes          | 127 674    |
| Tx bytes          | 120 727    |
| CPU %             | 2.33       |
| Handler p50 / p95 | 100 / 1000 µs (log-linear) |
| Decode errors     | 0          |
| NAKs              | 0          |
| Timeouts          | 0          |
| Retries           | 0          |
| Reconnects        | 0          |

Per-cmd breakdown of what VSM actually pushes at a matrix it's
connected to (raw counts from `dhs_connector_rx_cmd_hits_total`):

| cmd                          | count | note                                  |
|------------------------------|------:|---------------------------------------|
| 121 Crosspoint Go-Salvo      | 5 686 | steady salvo-fire stream              |
| 120 Crosspoint Connect-On-Go | 659   | salvo build (~8 goes per build)       |
| 106 Source Names Response    | 192   | **unsolicited label push** (Auto-Transmit) |
| 107 Dest Assoc Names Response| 128   | **unsolicited label push**            |
| 8 Dual Ctl Status Request    | 8     | VSM probes our matrix periodically    |
| 21 Crosspoint Tally Dump Req | 2     | VSM asked us for our tally table      |

On the tx side we emitted the expected counterparts — tx 009 (8×) for
dual-status, tx 122 (659×) for salvo-build ack, tx 123 (5 686×) for
salvo go-done, tx 022 (2×) for tally dumps.

The unsolicited `tx 106` / `tx 107` we received confirms the
**Auto-Transmit label push** behaviour documented in
`memory/project_probel_vsm_validation.md`.

## Observations

1. **Cross-direction wire-level validation is clean.** 26 k+ frames
   crossed between VSM and DHS in <4 min with zero decode errors, NAKs,
   timeouts or retries. The S1/S2/W3 refactors did not regress frame
   handling under real controller load.
2. **Matrix side absorbed a high salvo rate unsolicited.** VSM appeared
   to be running a salvo-fire loop (~110 frames/s sustained) against
   our matrix role. Our dispatcher goroutine + per-session bounded
   channel (S1 refactor) kept up at 2.3 % CPU on the host.
3. **VSM server mode does NOT reply to cmd 008.** Matches prior
   memory; confirmed again at 64×64. Consumer ctx-timeout is the
   correct outcome.
4. **No passthrough numbers.** W6-B1's original intent was a full
   Commie → DHS-consumer → VSM → DHS-provider chain with ≤2 ms
   downstream + ≤1 ms upstream overhead. That requires a gateway
   binding that doesn't exist yet — parked as future W6-B2.

## Artefacts captured in this folder

- `w6b1_provider.log` — provider JSON log (startup + sessions)
- `w6b1_vsm_bench.csv` / `.md` — bench per-op + summary
- `w6b1_provider_metrics.csv` / `.md` — early snapshot (~30 s in)
- `w6b1_provider_metrics_final.csv` / `.md` — final snapshot at stop
- `interop_1mtx_64_1lvl.json` (under `internal/probel-sw08p/assets/`)
  — the 64×64 tree fixture used by the provider
