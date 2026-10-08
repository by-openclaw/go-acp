# Probel SW-P-08 — where the connector stands against ADR-0025

Measured, not claimed. "Released" is the binary the fleet runs. Each play
was run twice from the control node with the second run changing nothing.

## The six deliverables (2026-10-09, released v0.43.0)

| # | Deliverable | State | Evidence |
|---|---|---|---|
| 1 | Consumer, every spec verb | done for what a real matrix serves; **8 verbs have no third-party oracle** (below) | `ansible/playbooks/probel-sw08p-neuron-verify.yml` on the EVS Neuron CONVERT `10.6.255.102:7800`: `ok=45 changed=0`, twice |
| 2 | Producer, every spec verb | code done; **not yet proven by a controller we did not write** (below) | `probel-sw08p-producer.yml`: resident `dhs-probel-sw08p.service` on the plant host, `:2008`, 64 x 64; `ok=7 changed=0`, twice |
| 3 | Integration: Go suite + Ansible, against an oracle | done on the consumer side | the play above; `internal/probel-sw08p/integration` `TestDeviceCLIRoundTrip` run by it with the released CLI |
| 4 | DM + manifest fixture — committed, or generated | done | `tools/gen-probel-tree` and the committed `testdata/exports/matrix_tree.json`, `assets/*.json` |
| 5 | Wireshark dissector | done | `wireshark/dhs_probel_sw08p.lua`; it renders the 17 real captures of the replay set as committed |
| 6 | Replay fixture set | done | `testdata/protocol_types/` — 17 command folders of real Neuron traffic, checked by a unit and an integration test |

## The consumer on the real Neuron

The CONVERT serves one matrix of 4 176 x 4 176 (the Shuffler,
`10.6.255.103`, 17 728 x 17 728, answers the same way). On it, with the
released CLI:

- **Read:** `status`, `health`, `discover`, `interrogate`, `tally-dump`
  (4 176 destinations), `all-` and `single-source-name`, `all-` and
  `single-dest-name`, `protect-interrogate`, `usage`, `export`, `replace
  --check`, `import --check`, `bench --phase interrogate` (4 000
  destinations, 0 errors).
- **Write, on one route** (destination 4175 moved to the free source 256
  and put back): `connect`, `replace`, `import`, each read back from the
  device; a second session's `watch` sees the matrix announce the change.
  The matrix confirms a connect before its own tally shows it — the route
  reads back right on the second or third read.
- **Every verb is given the matrix's source count** (`--srcs 4176`): above
  1 024 sources the consumer asks in the extended form, the only one a
  Neuron answers when the routed source does not fit ten bits.

## What a Neuron does not serve

It acknowledges the frame and sends nothing — SW-P-08 has no "unsupported"
reply. The consumer reports each as "not served by this matrix", and the
play asserts that it does:

`dual-status`, `protect-connect`, `master-protect`, `protect-dump`,
`protect-name`, `all-source-assoc-names`, `single-source-assoc-name`,
`salvo-connect`. `update-name` is acknowledged and the label is not
changed. `protect-disconnect` has nothing to disconnect.

Those verbs are proven against our own producer only (loopback and unit
tests). **They have no third-party oracle in the lab**: the Commie tool in
`assets/tools/` is a Windows GUI.

## Not run on the device, on purpose

`maintenance` (reset, clear protects) and `bench --phase connect`: both act
on every route of a matrix in use.

## The producer

Resident on the plant host since 2026-10-07. Checked by our own consumer
(64 destinations, 64 source names over four messages — the names were cut
to the first message until v0.40.4, #1435), which is a smoke test, not an
oracle.

**Open:** `probel-sw08p-producer-verify.yml` asserts a controller's
session and our counters, as for acp2. Cerebrum has no router device
pointing at `10.6.250.101:2008` yet, so there is no session to assert.

## Found and fixed on the way (v0.40.4)

- A request the matrix ACKed and did not answer was reported two ways,
  depending on which of two 5 s timers fired first (#1433).
- The producer answered an all-names request with its first message only
  (#1435).

## Since then (v0.41.0 – v0.43.0)

- `bench` exits non-zero when operations failed, naming how many and in
  which phase; it used to print `errors=4000` and exit 0 (#1471).
- Generated matrix labels are distinct within the eight characters the
  protocol carries: `SRC00001`, `DST00001` (#1446).
- The same plays, twice, on v0.43.0: `ok=45 changed=0` on the Neuron,
  `ok=7 changed=0` for the producer.
