# SNMP — where the connector stands against ADR-0025

Measured, not claimed. "Released" is the binary the fleet runs. Each play
was run twice from the control node with the second run changing nothing.

Two oracles, neither written by us: the **ATEME DR5000** decoder
`10.6.255.114` (v1 and v2c) and **net-snmp** (v3, both directions).

## The six deliverables (2026-10-09, released v0.43.0)

| # | Deliverable | State | Evidence |
|---|---|---|---|
| 1 | Consumer (the manager) | done on v1 / v2c / v3; **no v3 run on a real device yet** (below) | `get`, `walk`, `set`, `validate`, `health`, `--capture` on the DR5000; v3 discovery and authPriv poll against net-snmp's agent |
| 2 | Producer (the agent) | done | net-snmp's manager reads our agent in v3 (get, walk), reads our Report on a wrong password as "authentication failure", and exchanges informs with us both ways |
| 3 | Integration: Go suite + Ansible, against an oracle | done | `snmp-dr5000-verify.yml` `ok=17 changed=0`; `snmp-integration.yml` 9 pass, 2 skipped; `snmp-v3-verify.yml` `ok=25 changed=0` |
| 4 | DM + manifest fixture — committed, or generated | done | `testdata/integration-test/` |
| 5 | Wireshark dissector | done | `wireshark/dhs_snmp.lua`; decodes all 36 frames of the committed DR5000 capture |
| 6 | Replay fixture set | done for v1 / v2c | `testdata/protocol_types/` (6 folders) and `testdata/fixtures/dr5000-snmp.pcapng`, real DR5000 traffic; none for v3 |

## On the DR5000

- **Read:** `get`, `walk` (the whole tree is 381 079 objects and takes
  this agent 12 h 05 — net-snmp takes the same; 327 680 of them are one
  placeholder table), `validate` of a capture, `health`.
- **Write, on one object** (`dr5000UnitName.0`, a label): written with
  the write community, read back, put back; the read community is
  refused the write.

More in [`runbook.md`](runbook.md).

## Open, by name

- **v3 on a real device** — the Arista fabrics answer v3; the play is
  written (draft #1469) and waits for its credentials to be served by
  Vault.
- **Traps from a real device** — `trap-listen` receives and decodes
  net-snmp's traps and informs. The DR5000, with its trap target set to
  the listener, sent none in four lock / unlock tests. Parked.
