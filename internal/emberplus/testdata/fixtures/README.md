# Ember+ test fixtures

Multi-frame golden scenarios and expected-output files for Ember+ tests,
completing the ADR-0025 deliverable-6 replay set alongside `../protocol_types/`
(per-type captures) and `../exports/` (canonical round-trip exports).

## The replay set (deliverable 6)

| Dir | Contents | Used by |
|---|---|---|
| `../protocol_types/<type>/` | one folder per glow type / command (`capture.pcapng` + `tshark.tree` + `README.md`) | codec + dissector replay |
| `../exports/` | `device.yaml` / `device.csv` — canonical export of the committed integration tree, for round-trip checks | export/import round-trip |
| `../integration-test/` | `manifest/` + `dm/emberplus/` — the committed manifest+DM the producer serves | integration tier (ADR-0025 #3/#4) |
| `./` (this dir) | multi-frame golden scenarios (`.bin` / `.pcapng` + expected) | scenario/regression tests |

## How `../exports/` is regenerated (reproducible, repo-only)

The exports are derived from the committed manifest — no live device needed:

```sh
dhs producer emberplus serve \
  --manifest ../integration-test/manifest/emberplus-integration.json \
  --cache-dir ../integration-test --port 19000 --host 127.0.0.1 &
dhs consumer emberplus export 127.0.0.1 --port 19000 --format yaml --out ../exports/device.yaml
dhs consumer emberplus export 127.0.0.1 --port 19000 --format csv  --out ../exports/device.csv
```

## Adding a golden scenario here

Capture a multi-frame exchange (e.g. a matrix connect + the resulting tally
broadcast) with Wireshark on the S101 port (9000/9092), save the inner Ember+
payload, and add an expected-output file next to it. Promotion rules from local
`captures/emberplus/<ip>/<scenario>/` to this committed dir live in
`captures/README.md` (size cap, edge-case justification, byte-stability).
