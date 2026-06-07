# `validate` — full reference

Linked from the main runbook §12. Offline frame decode + report generation (ADR-0021).

## Today on `main`

`validate` reads a `frames.jsonl` capture (the `--capture` output from `walk` / `watch`) and decodes each frame through the Go codec. Default output: PASS/FAIL per frame on stdout.

### Happy

```powershell
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl
# frame   0  PASS  s101+glow+ber  dir=rx ts=2026-05-17T13:22:01.123Z
# frame   1  PASS  s101+glow+ber  dir=tx ts=2026-05-17T13:22:01.124Z
# ...
# frame 547  PASS  s101+glow+ber  dir=rx ts=2026-05-17T13:22:04.812Z
#
# summary: 548 frames, 548 PASS, 0 FAIL
```

### Wireshark dissector mode — R12 [#473](https://github.com/by-openclaw/go-acp/issues/473) (pending)

`validate --lua` replays each frame through the project's Wireshark dissector (`internal/emberplus/wireshark/dhs_emberplus.lua`) via `tshark -X lua_script:<path>`. Output matches `tshark -V` text-tree format — useful for operators familiar with Wireshark.

```powershell
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl --lua
# frame   0  ── Frame 1: 76 bytes on wire (608 bits), 76 bytes captured
#                 dhs_emberplus
#                     S101 framing
#                         BOF: 0xfe
#                         Length: 71
#                     ...
```

Pending — see [#473](https://github.com/by-openclaw/go-acp/issues/473).

## R23 (not yet filed) — `--report <md|json>`

Generate a structured report file alongside the PASS/FAIL stdout:

```powershell
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl --report report.md
.\bin\dhs.exe consumer emberplus validate captures/emberplus/runbook/walk-happy.jsonl --report report.json
```

### Markdown report shape

```markdown
# Validation report — walk-happy.jsonl

- File: `captures/emberplus/runbook/walk-happy.jsonl`
- Frames: 548
- Pass:   548 (100.0%)
- Fail:   0
- Started: 2026-05-17T13:22:01.123Z
- Ended:   2026-05-17T13:22:04.812Z

## Per-layer pass rate

| Layer | Pass | Fail | Notes |
|---|---:|---:|---|
| S101 framing | 548 | 0 | CRC + escape + keepalive all clean |
| BER decode | 548 | 0 | every APPLICATION tag resolved |
| Glow tree | 548 | 0 | every element type round-trips |
| Streams | 12 | 0 | id=0 + id=1001 + id=1002 |

## Failures

(empty when all PASS — populated with frame# + offset + decoder error when FAIL)
```

### JSON report shape

```json
{
  "file": "captures/emberplus/runbook/walk-happy.jsonl",
  "frames": 548,
  "pass": 548,
  "fail": 0,
  "started": "2026-05-17T13:22:01.123Z",
  "ended": "2026-05-17T13:22:04.812Z",
  "byLayer": {
    "s101":   { "pass": 548, "fail": 0 },
    "ber":    { "pass": 548, "fail": 0 },
    "glow":   { "pass": 548, "fail": 0 },
    "stream": { "pass": 12,  "fail": 0 }
  },
  "failures": []
}
```

### Acceptance for R23

- `--report report.md` writes a deterministic markdown report (timestamps + per-layer table + failure list)
- `--report report.json` writes the same data as JSON for CI consumption
- Mutually exclusive: `--report` and `--lua` can stack (`--lua` controls the per-frame decoder; `--report` controls the summary file)
- If `--report` target unwritable → `validate: open <path>: permission denied`, exit 1

## Errors

| Trigger | Command | Expected | Exit |
|---|---|---|---|
| missing file | `validate does-not-exist.jsonl` | `validate: open does-not-exist.jsonl: no such file` | 1 |
| not jsonl | `validate file.txt` | `validate: line 1: invalid JSON: ...` | 1 |
| frame decode failure | (corrupted frame in jsonl) | `frame N  FAIL  layer=glow offset=...  decode: ...`; summary line counts FAIL | 1 |
| tshark missing for `--lua` (R12) | `validate ... --lua` (no tshark on PATH) | `validation: tshark not found — install Wireshark (https://www.wireshark.org)` | 2 |
| report target unwritable (R23) | `validate ... --report /no/perm/r.md` | `validate: open /no/perm/r.md: permission denied` | 1 |

## Refs

- [ADR-0021 — wire-trace JSONL contract + validate verb](../../../../docs/adr/0021-wire-trace-jsonl-contract.md)
- [`internal/emberplus/wireshark/dhs_emberplus.lua`](../../wireshark/dhs_emberplus.lua) — byte-exact dissector
- [#473](https://github.com/by-openclaw/go-acp/issues/473) — R12 `--lua` mode
- R23 (not yet filed) — `--report <md|json>`
