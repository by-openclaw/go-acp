# TSL UMD per-type fixtures (ADR-0020 Bucket 1)

One folder per wire-version + frame-type. Each folder will, when
fixtures are captured, contain:

| File | Source |
| --- | --- |
| `frames.jsonl` | Wire-trace per ADR-0021 — drives codec encode/decode tests |
| `capture.pcapng` | OS-socket capture — drives the Wireshark Lua dissector check |
| `tshark.tree` | Frozen dissector output — regression diff target |
| `README.md` | Scenario context + spec citation + dhs commit at capture time |

These fixtures are consumed by `internal/tsl/codec/*_test.go` (byte-
level round-trip) and by the dissector cross-check pipeline
(`tshark -X lua_script:wireshark/dissector_tsl.lua` on the
`.pcapng`).

## Initial folders

| Folder | Wire | Spec | Description |
| --- | --- | --- | --- |
| `v31_frame/` | UDP | §3.0 | 18-byte v3.1 frame (HEADER + CTRL + DATA), 4 binary tallies, no colour. |
| `v40_frame/` | UDP | §4.0 | v3.1 + CHKSUM + VBC + XDATA — 2-bit colour per LH/Text/RH per L/R display. |
| `v50_dmsg/` | UDP ≤ 2048 B | §5.0 | UDP packet with PBC + VER + FLAGS + SCREEN + DMSG+ . Single + grouped multi-DMSG. |
| `v50_dle_stx_tcp/` | TCP wrapped | §5.0 §Phy | v5.0 TCP transport with DLE/STX wrapper + 0xFE byte-stuffing. |

## Why this layout

- Codec test discovery is `for d in protocol_types/*/`, no manifest.
- The folder name carries wire version + transport so a reviewer
  reading the `git diff` understands the scope without cracking the
  binary.
- TSL has three wire versions (v3.1, v4.0, v5.0) and v5.0 adds a
  TCP-wrapped variant — one folder per (version, transport) pair
  keeps fixtures independently testable.

## Pairing with `captures/`

Live captures live LOCAL ONLY at `captures/tsl-vXX/<scenario>/
frames.jsonl` (gitignored, per ADR-0021). Plugin registry names per
CLAUDE.md:

- `tsl-v31` → UDP v3.1
- `tsl-v40` → UDP v4.0
- `tsl-v50` → UDP v5.0 (use `--tcp` for the DLE/STX variant)

To promote a live capture into a committed per-type fixture:

1. Capture: `dhs consumer tsl-v50 listen --capture
   captures/tsl-v50/<scenario>/frames.jsonl`
2. Trim to the relevant single frame (or paired produce/consume).
3. Drop the trimmed `frames.jsonl` here under the matching wire-
   version folder, alongside a one-paragraph `README.md` citing the
   scenario and the dhs commit at capture time.

## Walk note

TSL has no walkable tree — Plugin.Walk / GetValue / SetValue /
Subscribe all return `protocol.ErrNotImplemented` by design (per
CLAUDE.md "TSL not use all the verb like walk"). These fixtures
exercise the codec layer + Validate(), not Walk; the contract is
pinned by `TestPlugin_WalkReturnsErrNotImplemented_AllVersions` in
`consumer/replay_test.go`.
