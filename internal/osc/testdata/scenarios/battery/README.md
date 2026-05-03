# OSC scenario: battery

Multi-frame OSC capture used as a replay smoke test.

| Field | Value |
|---|---|
| Source | `osc.js` cross-implementation oracle |
| Transport | UDP |
| Direction | mixed (tx + rx) |
| Frames | hundreds (full session) |
| Spec | OSC 1.0 / 1.1 |

## Files

| File | Purpose |
|---|---|
| `capture.pcapng` | OS-socket capture for Wireshark Lua dissector verification (`tshark -X lua_script:internal/osc/wireshark/dhs_osc.lua`) |

## Replay

Once the `replay` verb (per ADR-0002 + ADR-0021) lands:

```text
dhs consumer osc replay internal/osc/testdata/scenarios/battery/frames.jsonl --validate-only
```

`frames.jsonl` is not yet committed alongside this scenario — the
pcapng was authored before ADR-0021 locked the wire-trace JSONL
contract. Adding `frames.jsonl` (extracted from the pcapng) is a
follow-up per ADR-0021.
