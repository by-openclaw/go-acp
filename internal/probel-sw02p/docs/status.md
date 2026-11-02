# Probel SW-P-02 — where the connector stands against ADR-0025

Measured, not claimed. "Released" is the binary the fleet runs. Each play
was run twice from the control node with the second run changing nothing.

**No device in the lab speaks SW-P-02.** The connector's third-party
evidence is therefore its producer under a controller we did not write
(Cerebrum), and that is the part still open.

## The six deliverables (2026-10-09, released v0.43.0)

| # | Deliverable | State | Evidence |
|---|---|---|---|
| 1 | Consumer, every spec verb | code done; **no third-party oracle** | loopback against our own producer only: `internal/probel-sw02p/integration`, `ansible/playbooks/probel-sw02p-integration.yml`, `probel-sw02p-verbs.yml` |
| 2 | Producer, every spec verb | code done; **not yet proven by a controller we did not write** | `probel-sw02p-producer.yml`: resident `dhs-probel-sw02p.service` on the plant host, `:2002`, 64 x 64, metrics `:9104`; `ok=6 changed=0`, twice |
| 3 | Integration: Go suite + Ansible, against an oracle | **partial** — loopback only | the plays above; `probel-sw02p-producer-verify.yml` is written and waits for a controller session |
| 4 | DM + manifest fixture — committed, or generated | done | `tools/gen-probel-tree`, `testdata/exports/matrix_tree.json` |
| 5 | Wireshark dissector | done | `wireshark/dhs_probel_sw02p.lua` |
| 6 | Replay fixture set | **missing** | `testdata/protocol_types/` holds six folders with their README and no capture yet |

## The producer

Resident on the plant host, `10.6.250.101:2002`, serving the same 64 x 64
one-level matrix as the SW-P-08 producer. The deploy play checks that it
answers an interrogate and serves its metrics — a smoke test with our own
consumer, not an oracle (ADR-0034).

`probel-sw02p-producer-verify.yml` asserts what a controller's session
proves: connected from Cerebrum's address, frames received and sent both
advancing, nothing undecodable, no NAK. Cerebrum has no SW-P-02 router
device pointing at the producer yet, so there is no session to assert.

## Fixed on the way

- An unrouted destination inside the matrix answered source 1023, the
  spec's "out of range" value; it now answers source 0, and 1023 only for
  a destination the matrix does not have (#1448, v0.41.0).

## Open, by name

- **Third-party proof** (deliverables 1–3): a Cerebrum router device on
  `10.6.250.101:2002`, then `probel-sw02p-producer-verify.yml`.
- **Replay fixtures**: captured from that Cerebrum session.
