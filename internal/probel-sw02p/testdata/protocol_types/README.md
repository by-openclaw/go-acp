# SW-P-02 per-type fixtures (ADR-0020 Bucket 1)

One folder per wire-type-or-verb. Each folder will, when fixtures are
captured, contain:

| File | Source |
| --- | --- |
| `frames.jsonl` | Wire-trace per ADR-0021 — drives codec encode/decode tests |
| `capture.pcapng` | OS-socket capture — drives the Wireshark Lua dissector check |
| `tshark.tree` | Frozen dissector output — regression diff target |
| `README.md` | Scenario context + spec citation + dhs commit at capture time |

These fixtures are consumed by `internal/probel-sw02p/codec/*_test.go`
(byte-level round-trip) and by the dissector cross-check pipeline
(`tshark -X lua_script:wireshark/dhs_probel_sw02p.lua` on the
`.pcapng`).

## Initial folders

| Folder | Direction | Cmd byte | Spec | Description |
| --- | --- | ---: | --- | --- |
| `rx_001_interrogate/` | controller → matrix | 1 | §3.2.3 | "What's connected at dst?" |
| `rx_002_connect/` | controller → matrix | 2 | §3.2.4 | "Connect src to dst." |
| `tx_003_tally/` | matrix → controller | 3 | §3.2.5 | Spontaneous tally fan-out. |
| `tx_004_crosspoint_connected/` | matrix → controller | 4 | §3.2.6 | Reply to rx 1 / rx 2. |
| `rx_007_status_request/` | controller → matrix | 7 | §3.2.9 | Status query (used as poll keepalive). |
| `tx_009_status_response_2/` | matrix → controller | 9 | §3.2.11 | Status response carrying matrix state. |

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

Live captures live LOCAL ONLY at `captures/probel-sw02p/<scenario>/
frames.jsonl` (gitignored, per ADR-0021). To promote a live capture
into a committed per-type fixture:

1. Capture: `dhs consumer probel-sw02p ... --capture
   captures/probel-sw02p/<scenario>/frames.jsonl`
2. Trim to the relevant single frame (or paired req/reply).
3. Drop the trimmed `frames.jsonl` here under the matching command
   folder, alongside a one-paragraph `README.md` citing the scenario
   and the dhs commit at capture time.
