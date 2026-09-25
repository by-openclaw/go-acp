# mnset testdata

What the connector is tested against without a device.

| Folder | What it is |
|---|---|
| `protocol_types/<kind>/` | one exchange per resource kind the module serves — `wire.jsonl` (the ADR-0028 capture: request out, reply back), `body.txt` (the response body alone, which is what the decoder reads) and a `README.md` saying what the kind is. Replayed by `internal/MNSet/consumer/replay_test.go`. |
| `fixtures/fusion6-walk.jsonl` | the golden scenario: a whole `walk` of the FusioN6, 469 exchanges, as captured. |
| `exports/` | canonical exports for round-trip checks. |
| `integration-test/` | the committed DM + manifest (ADR-0025 #4), checked by `fixture_test.go`. |
| the loose files at this level | notes and scrapes from the scoping work (the MN SET UI, the NMOS node document, the raw endpoint list) — kept because they are how the dictionary was written, not because a test reads them. |

Everything here came off **10.6.40.53** (FusioN6, firmware `0x68cd783f`,
serial 125061600012) on 2026-09-24. Re-capture after a firmware change:

```
dhs consumer mnset walk 10.6.40.53 --slot 0 --capture cap/     # the wire
dhs consumer mnset walk 10.6.40.53 --slot 0                    # the DM
dhs consumer mnset export 10.6.40.53 --format csv --out …      # the export
```

A test that fails after a re-capture is the module telling you it
changed shape. Read the diff before changing the test.
