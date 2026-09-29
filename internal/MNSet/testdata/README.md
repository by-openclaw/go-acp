# mnset testdata

What the connector is tested against without a device.

| Folder | What it is |
|---|---|
| `protocol_types/<kind>/` | one exchange per resource kind the module serves (16) — `capture.pcapng` (that TCP conversation cut from a live walk), `tshark.tree` (its frozen decode through `wireshark/dhs_mnset.lua`), `wire.jsonl` (the ADR-0028 capture), `body.txt` (the body the decoder reads) and a `README.md`. Replayed by `replay_test.go`; the dissector contract is pinned by `fixture_parity_test.go`. |
| `fixtures/fusion6-walk.jsonl` | the golden scenario: a whole `walk` of the FusioN6, 469 exchanges, as captured. |
| `exports/` | canonical export for round-trip checks — typed (#1185), re-captured 2026-09-29 from 10.6.40.54. |
| `integration-test/` | the committed DM + manifest (ADR-0025 #4), checked by `fixture_test.go`. |
| the loose files at this level | notes and scrapes from the scoping work (the MN SET UI, the NMOS node document, the raw endpoint list) — kept because they are how the dictionary was written, not because a test reads them. |

The walk, the per-type exchanges and the DM came off the same module —
FusioN6, firmware `0x68cd783f`, serial 125061600012 — first at 10.6.40.53
(2026-09-24), the pcapng captures, typed DM and export at **10.6.40.54**
(2026-09-29). Re-capture after a firmware change:

```
dhs consumer mnset walk 10.6.40.54 --slot 0 --capture cap/     # the wire (JSONL)
dhs consumer mnset walk 10.6.40.54 --slot 0                    # the DM
dhs consumer mnset export 10.6.40.54 --format csv --out …      # the export
# pcapng + tree per kind: dumpcap during a walk, then
# scripts/fixturize.sh walk.pcapng protocol_types/<kind> <frames of one tcp.stream>
```

A test that fails after a re-capture is the module telling you it
changed shape. Read the diff before changing the test.
