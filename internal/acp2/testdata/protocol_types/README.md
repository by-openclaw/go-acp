# ACP2 per-type fixtures

One slimmed capture + frozen tshark tree per ACP2 wire element, as defined
by `internal/acp2/assets/acp2_protocol.pdf` (object types + functions +
error codes) and the AN2 transport spec. The dissector under
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

## Coverage (100%)

### Object types (acp2_protocol.pdf §2 "Object types")

| obj_type | Name   | Fixture dir              |
|---------:|--------|--------------------------|
| 0        | Node   | [`node/`](node/)          |
| 1        | Preset | [`preset/`](preset/)      |
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

| stat | Name              | Fixture dir                                             | Triggered by                                   |
|-----:|-------------------|---------------------------------------------------------|------------------------------------------------|
| 0    | protocol          | [`error_protocol/`](error_protocol/)                     | `dhs consumer acp2 diag` probe "unknown func=0xFF" |
| 1    | invalid_obj_id    | [`error_invalid_obj_id/`](error_invalid_obj_id/)         | `dhs consumer acp2 diag --slot 99`                 |
| 2    | invalid_idx       | [`error_invalid_idx/`](error_invalid_idx/)               | `dhs consumer acp2 get ... --idx 99`               |
| 3    | invalid_pid       | [`error_invalid_pid/`](error_invalid_pid/)               | `dhs consumer acp2 get ... --pid 99`               |
| 4    | no_access         | [`error_no_access/`](error_no_access/)                   | `dhs consumer acp2 set` on a read-only object      |
| 5    | invalid_value     | [`error_invalid_value/`](error_invalid_value/)           | `dhs consumer acp2 set ... --raw <out-of-range>`   |

## Provider policy: clamp vs reject on set_property

Documented in detail at
[`error_invalid_value/README.md`](error_invalid_value/README.md). Summary:

| Object type              | Out-of-range policy             | Error returned |
|--------------------------|---------------------------------|----------------|
| Number (s8..u64, float)  | **CLAMP** to [min, max] silently | none           |
| Enum                     | **REJECT** when idx ≥ len(options) | stat=5       |
| IPv4                     | Reject if len(bytes) ≠ 4         | stat=5         |
| String                   | Truncate to pid 6 max_length     | none           |

This means stat=5 is only reachable via Enum out-of-range or malformed
IPv4/string payload — numeric out-of-range is absorbed by the clamp.

## Using a fixture

```bash
export PATH="/c/Program Files/Wireshark:$PATH"   # Windows
tshark -r internal/acp2/testdata/protocol_types/preset/capture.pcapng -V
```

Compare against `preset/tshark.tree`. Matching output means the dissector
is still faithful to the frozen reference. Diverging output is either a
dissector regression or a genuine protocol-shape change (rare — the ACP2
spec is frozen).

## Regenerate

```bash
./scripts/capture-acp2-fixtures.sh bin/acp2_fixtures.pcapng
make fixtures-acp2
```

Wireshark / tshark ≥ 4.x required. The dissector under
`internal/acp2/wireshark/dissector_acp2.lua` must be installed in the
personal plugins dir (see [`docs/wireshark.md`](../../../../docs/wireshark.md)).
