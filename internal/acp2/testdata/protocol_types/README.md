# ACP2 per-type fixtures

One slimmed capture + frozen tshark tree per ACP2 wire element, as defined
by `internal/acp2/assets/acp2_protocol.pdf` (object types) and the AN2
transport spec. The dissector under
`internal/acp2/wireshark/dissector_acp2.lua` is expected to render every
element exactly as frozen — the parity test under
`internal/acp2/consumer/fixture_parity_test.go` asserts that.

## Capture source

Self-driven loopback capture via `dhs producer acp2 serve` +
`dhs consumer acp2 walk|get|set|watch|diag` while tshark records. No
external hardware required.

- Fixture tree (producer side): [`fixture_tree.json`](fixture_tree.json) —
  one leaf per ACP2 object type on slot 1, plus a rack-controller slot 0.
- Orchestration: [`scripts/capture-acp2-fixtures.sh`](../../../../scripts/capture-acp2-fixtures.sh)
- Makefile target: `make fixtures-acp2` regenerates every fixture under
  this directory from a fresh `bin/acp2_fixtures.pcapng`.

## Coverage

### Object types (acp2_protocol.pdf §2 "Object types")

| obj_type | Name   | Fixture dir              |
|---------:|--------|--------------------------|
| 0        | Node   | [`node/`](node/)          |
| 1        | Preset | — see "Not covered" below |
| 2        | Enum   | [`enum/`](enum/)          |
| 3        | Number | [`number/`](number/)      |
| 4        | IPv4   | [`ipv4/`](ipv4/)          |
| 5        | String | [`string/`](string/)      |

### Functions (acp2_protocol.pdf §3 "Functions")

| func | Name          | Fixture dir                          |
|-----:|---------------|--------------------------------------|
| 0    | get_version   | [`get_version/`](get_version/)        |
| 1    | get_object    | [`get_object/`](get_object/)          |
| 2    | get_property  | [`get_property/`](get_property/)      |
| 3    | set_property  | [`set_property/`](set_property/)      |

### Announces (type=2)

| type | Fixture dir                 |
|-----:|-----------------------------|
| 2    | [`announce/`](announce/)     |

### Error codes (acp2_protocol.pdf §4 "Error stat codes")

| stat | Name              | Fixture dir                                            |
|-----:|-------------------|--------------------------------------------------------|
| 0    | protocol          | — see "Not covered" below                               |
| 1    | invalid_obj_id    | [`error_invalid_obj_id/`](error_invalid_obj_id/)         |
| 2    | invalid_idx       | — see "Not covered" below                               |
| 3    | invalid_pid       | — see "Not covered" below                               |
| 4    | no_access         | [`error_no_access/`](error_no_access/)                   |
| 5    | invalid_value     | — see "Not covered" below                               |

## Not covered

| Element                          | Reason                                                                                       |
|----------------------------------|---------------------------------------------------------------------------------------------|
| obj_type 1 Preset                | Provider does not yet emit preset children with pid 7 preset_depth. Tracked under #79.        |
| stat 0 protocol-error            | Requires a raw-bytes probe the `dhs consumer acp2 diag` set does not yet send.                |
| stat 2 invalid_idx               | Requires a GetProperty with idx=99 on a non-preset object — not yet exposed via the CLI.      |
| stat 3 invalid_pid               | Requires GetProperty with an unknown pid on a valid obj — not yet exposed via the CLI.        |
| stat 5 invalid_value             | The CLI pre-coerces out-of-range enum input to 0 before encoding, so the wire-level rejection never fires. Needs a `--raw` value probe. |

Follow-ups that would close these gaps: extend `dhs consumer acp2 diag`
with targeted stat-probes (or add a `--bypass-encode` mode to `set`), then
rerun `make fixtures-acp2`.

## Using a fixture

```bash
export PATH="/c/Program Files/Wireshark:$PATH"   # Windows
tshark -r internal/acp2/testdata/protocol_types/enum/capture.pcapng -V
```

Compare against `enum/tshark.tree`. Matching output means the dissector
is still faithful to the frozen reference. Diverging output is either a
dissector regression or a genuine protocol-shape change (rare — the ACP2
spec is frozen).

## Regenerate

```bash
make fixtures-acp2
```

Wireshark / tshark ≥ 4.x required. The dissector under
`internal/acp2/wireshark/dissector_acp2.lua` must be installed in the
personal plugins dir (see [`docs/wireshark.md`](../../../../docs/wireshark.md)).
