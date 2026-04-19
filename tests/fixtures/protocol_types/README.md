# Per-type fixtures

One slimmed pcap + frozen `tshark -V` tree per wire-level element type. The goal
is to give a single, byte-exact reference for every Glow / ACP1 / ACP2 element
a dissector or codec must decode.

## Layout

```
tests/fixtures/protocol_types/<protocol>/<type>/
  capture.pcapng    slimmed pcap (usually <1 KB, single representative frame)
  tshark.tree      frozen `tshark -V` dissection (volatile timestamps masked)
  README.md         spec page + expected shape + CLI hint
```

Each pcap is extracted from a full capture in `bin/` via
[`scripts/fixturize.sh`](../../../scripts/fixturize.sh). The frozen tree acts
as a golden reference for the parity test under
[`tests/unit/fixture_parity/`](../../unit/fixture_parity/) — any dissector
regression causes the parity test to fail.

## Supported protocols

| Protocol | Fixture count | Status                                  |
|----------|---------------|-----------------------------------------|
| Ember+   | 14            | ✅ done (#60 / #61). 5-type gap in #62. |
| ACP1     | 13            | ✅ done. 2-type gap (file, announcement) |
| ACP2     | —             | queued                                  |

## Re-generating

```bash
# One fixture
scripts/fixturize.sh bin/emberplus_glow_mtx_lua.pcapng \
    tests/fixtures/protocol_types/emberplus/matrix 41

# All Ember+ fixtures
make fixtures-emberplus
# All ACP1 fixtures
make fixtures-acp1
# Everything
make fixtures
```

Wireshark / tshark ≥ 4.x required. The dissector under
`assets/emberplus/dissector_emberplus.lua` must be installed in the
personal plugins dir (see [`docs/wireshark.md`](../../../docs/wireshark.md)).
