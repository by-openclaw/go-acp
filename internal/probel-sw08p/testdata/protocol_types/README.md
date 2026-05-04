# SW-P-08 per-type fixtures (ADR-0020 Bucket 1)

One folder per wire-type-or-verb. Each folder will, when fixtures are
captured, contain:

| File | Source |
| --- | --- |
| `frames.jsonl` | Wire-trace per ADR-0021 — drives codec encode/decode tests |
| `capture.pcapng` | OS-socket capture — drives the Wireshark Lua dissector check |
| `tshark.tree` | Frozen dissector output — regression diff target |
| `README.md` | Scenario context + spec citation + dhs commit at capture time |

These fixtures are consumed by `internal/probel-sw08p/codec/*_test.go`
(byte-level round-trip) and by the dissector cross-check pipeline
(`tshark -X lua_script:wireshark/dhs_probel_sw08p.lua` on the
`.pcapng`).

## Initial folders

| Folder | Direction | Cmd byte | Spec | Description |
| --- | --- | ---: | --- | --- |
| `rx_001_crosspoint_interrogate/` | controller → matrix | 1 | §3.2.1 | "What's connected at (matrix, level, dst)?" |
| `rx_002_crosspoint_connect/` | controller → matrix | 2 | §3.2.2 | "Connect src to dst on (matrix, level)." |
| `tx_003_crosspoint_tally/` | matrix → controller | 3 | §3.2.3 | Spontaneous "this dst is now connected to src." |
| `tx_004_crosspoint_connected/` | matrix → controller | 4 | §3.2.4 | Reply to cmd 1 / cmd 2 — "the answer is src." |
| `rx_007_maintenance/` | controller → matrix | 7 | §3.2.7 | Maintenance / liveness probe. |

Add new folders as we land per-command fixtures (NN_command_name/
matching `cmd_rxNNN_xxx.go` / `cmd_txNNN_xxx.go` filenames in
`consumer/` and `provider/`).

## Why this layout

- Codec test discovery is `for d in protocol_types/*/`, no manifest.
- The folder name carries direction + command byte + name so a
  reviewer reading the `git diff` understands the scope without
  cracking the binary.
- Same name across protocols (Bucket 1 layout per ADR-0020) lets the
  cross-protocol fixture validator stay protocol-agnostic.

## Pairing with `captures/`

Live captures live LOCAL ONLY at `captures/probel-sw08p/<scenario>/
frames.jsonl` (gitignored, per ADR-0021). To promote a live capture
into a committed per-type fixture:

1. Capture: `dhs consumer probel-sw08p ... --capture
   captures/probel-sw08p/<scenario>/frames.jsonl`
2. Trim to the relevant single frame (or paired req/reply).
3. Drop the trimmed `frames.jsonl` here under the matching command
   folder, alongside a one-paragraph `README.md` citing the scenario
   and the dhs commit at capture time.
