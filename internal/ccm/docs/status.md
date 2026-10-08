# CCM — where the connector stands against ADR-0025

Measured, not claimed. "Released" is the binary the fleet runs. The play
was run twice from the control node, the second run changing nothing.

## The six deliverables (2026-10-09, released v0.43.0)

| # | Deliverable | State | Evidence |
|---|---|---|---|
| 1 | Consumer | done | the verbs in [`consumer.md`](consumer.md), on the Neuron SHUFFLE `10.6.255.103` (state pushed) and the CONVERT `10.6.255.102` (state polled) |
| 2 | Producer | **partial** | serves the `/api/v1` shape; no event channel, no SHUFFLE shape (#1214); not yet driven by a controller we did not write — [`provider.md`](provider.md) |
| 3 | Integration: Go suite + Ansible, against an oracle | done on the consumer side | `ansible/playbooks/ccm-integration.yml` runs `internal/ccm/integration` with the released CLI against both devices: 9 of 9 |
| 4 | DM + manifest fixture — committed, or generated | done | generated from the device by `dhs consumer ccm export` (the DM is built from the OpenAPI document the device serves); manifests in `testdata/integration-test/manifest/` |
| 5 | Wireshark dissector | **missing** | not shipped |
| 6 | Replay fixture set | **missing** | none committed |

## What the nine tests prove

On the real devices, each write read back and put back:

- `info` names the device as it names itself; `get` reads one value
  without a walk; a value the device's document forbids is refused
  before it is sent;
- a delay, a gain and a crosspoint are changed, each seen by a `watch`
  with nothing polled, and restored;
- the device model is collected once and reused by the next `watch`;
- `export` reads a device that has no API root (3 088 streams, 42 972
  resources, 3 min 22 s);
- a device without the event channel is polled.

## Open, by name

- **Producer** — event channel and SHUFFLE shape: #1214. Third-party
  proof: Cerebrum's CCM driver, when Cerebrum is available.
- **Replay fixtures** — wait for Cerebrum, with the other connectors.
- **Dissector** — after the connector is in use.
