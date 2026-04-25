# ACP1 per-type fixtures

One slimmed capture + frozen tshark tree per ACP1 wire element, as defined
by AXON-ACP v1.4. The dissector under `internal/acp1/wireshark/dhs_acpv1.lua`
is expected to render every element exactly as frozen — the CI parity test
under `tests/unit/fixture_parity/` asserts that.

Capture source: Synapse Simulator emulator (UDP 2071, mode A direct).

## Coverage

### Object types (AXON-ACP_v1_4.pdf, pp. 2-7)

| type | Name         | Fixture dir                     | Spec page |
|------|--------------|---------------------------------|-----------|
| 0    | Root         | [`root/`](root/)                 | p. 3      |
| 1    | Integer      | [`integer/`](integer/)           | p. 4      |
| 2    | IP Address   | [`ip_address/`](ip_address/)     | p. 4      |
| 3    | Float        | [`float/`](float/)               | p. 4      |
| 4    | Enumerated   | [`enumerated/`](enumerated/)     | p. 5      |
| 5    | String       | [`string/`](string/)             | p. 5      |
| 6    | Frame Status | [`frame_status/`](frame_status/) | p. 6      |
| 7    | Alarm        | [`alarm/`](alarm/)               | p. 7      |
| 9    | Long         | [`long/`](long/)                 | p. 4      |
| 10   | Byte         | [`byte/`](byte/)                 | p. 4      |

### Message types (AXON-ACP_v1_4.pdf, p. 11)

| MTYPE | Name        | Fixture dir              |
|-------|-------------|--------------------------|
| 1     | Request     | [`request/`](request/)    |
| 2     | Reply       | [`reply/`](reply/)        |
| 3     | Error       | [`error/`](error/)        |

## Not covered (Synapse Simulator gap)

| type | Name        | Reason                                         |
|------|-------------|------------------------------------------------|
| 8    | File        | Emulator does not expose File objects          |
| —    | Announcement (MTID=0) | Emulator broadcasts did not reach loopback capture; needs real hardware |

Re-capture against a real rack controller (Axon Synapse or Cerebrum-connected
device) with `Broadcasts=On` will fill these gaps.

## Using a fixture

```bash
export PATH="/c/Program Files/Wireshark:$PATH"   # Windows
tshark -r tests/fixtures/protocol_types/acp1/integer/capture.pcapng -V
```

Compare the output to `integer/tshark.tree`. Matching indicates the
dissector is faithful to the frozen reference; diverging output is either
a dissector regression or a genuine protocol-shape change (rare —
ACP1 v1.4 is frozen).

## Regenerate

```bash
make fixtures-acp1
```

Wireshark / tshark ≥ 4.x required. The dissector under
`internal/acp1/wireshark/dhs_acpv1.lua` must be installed in the personal
plugins dir (see [`docs/wireshark.md`](../../../../docs/wireshark.md)).
