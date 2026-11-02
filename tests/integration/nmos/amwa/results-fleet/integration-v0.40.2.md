# Integration, pairings and manual rows — the released v0.40.2

Run from the control node on 2026-10-06 against the released binary the
fleet runs (`dhs v0.40.2`), the suite built from `main`
(`internal/amwa/integration/`). The peers: the nmos-cpp reference
registry and node on the tooling host, a transient nmos-cpp Node, the
NMOS-Reference IS-11 Node, and the Neuron CONVERT. Verdict lines exactly
as the suite printed them. Each play was run twice; the second run
changed nothing on the hosts (`changed=0`) and gave the same verdicts.

## `ansible/playbooks/amwa-interop-nmos-cpp.yml`

The registry and the mirror, scored by nmos-cpp. All pass.

```
peer_test.go:63: PASS: node 1a03472f-6fd7-5a3a-b525-71017a4cfd22 — 91 resource(s) of its Node API are in our Query API, document for document
peer_test.go:87: PASS: listed at every one of 15 looks, health 1791288279 -> 1791288294
peer_test.go:128: PASS: 22 sender(s) over 11 page(s) of 2, each once
peer_test.go:151: PASS: the first grain carries the peer's 22 receiver(s), document for document
peer_test.go:178: PASS: everything registered again 5 s after our registry forgot the node
peer_test.go:260: PASS: 91 resource(s) level in the oracle registry at the fill and after a live registration; forwarded 184, deleted 91, refused or failed 0, repairs 1; announced on its WebSocket; gone after the mirror stopped
peer_test.go:289: PASS: the node and every resource under it expired 9 s after the kill
peer_test.go:339: PASS: 92 resource(s) of the oracle registry are in ours, document for document, held 15 s; forwarded 92, refused or failed 0, repairs 0
```

## `ansible/playbooks/amwa-integration.yml`

The controller, the node, the model verbs, the mirror copy and the
dissector replay. 20 tests pass — two of them reject paths that
correctly rejected (FAIL-expected) — and none fails.

```
controller_pairing_test.go:91: PASS: the watch printed node 2c47bf5e-1b2c-4abc-9def-deadbeef0001 as added when it registered and as removed when it left
controller_pairing_test.go:140: PASS: receiver 6fbe0f69-9f18-5697-82c4-8e1299d7cd5a staged as scheduled, not active before its time, active on sender 40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd 5.1 s after the request (asked: 5s)
controller_pairing_test.go:178: PASS: 2 receivers connected in one salvo and disconnected in another, as read from the node's own IS-05
controller_pairing_test.go:242: PASS: sender 40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd — 2 leg(s) moved from [232.162.3.127 232.212.47.226] to [239.255.200.77 239.255.201.77] and back, as read from the node's own IS-05
controller_pairing_test.go:317: PASS: 5 state message(s) of source 9fa74b97-ee1c-51b3-b908-4b2f3dbee723, type number/temperature/C — the type the node's own Events API reports
controller_test.go:26: PASS: 2 nodes, 22 senders, 22 receivers — the same ids as the registry's own Query API
controller_test.go:47: PASS: node b7011c4e-5f39-5a1a-a6eb-a8036b0a5fd9 — 176 senders, 176 receivers, the same ids as the device's own Node API
controller_test.go:156: PASS: dry run via http://10.6.255.102:3000/x-nmos/connection/v1.1, the device's staged state untouched
controller_test.go:170: FAIL-expected: the unknown Receiver was refused by name
controller_test.go:195: PASS: subscription opened, 2 node(s) in the first grain
controller_test.go:234: PASS: receiver 6fbe0f69-9f18-5697-82c4-8e1299d7cd5a connected to sender 40af44c4-3dd3-53c0-9c54-e2aec5c7f1fd and disconnected, as read from the node's own IS-05
dissector_replay_test.go:80: PASS: 19 captures replayed through dhs_nmos.lua, each as its committed tree
model_test.go:127: PASS: 38 objects over IS-12, the role paths the node's own IS-14 lists
model_test.go:150: PASS: root.ExampleControl.1p6 read, set to "dhs integration" and back to "Example control worker" over IS-12, each as the node's own IS-14 serves it
model_test.go:171: PASS: a change made through the node's IS-14 was printed by the IS-12 watch
model_test.go:212: PASS: root.ExampleControl.1p6 read, set and set back; a backup of 1 object(s) validated by the node for a restore, nothing applied
model_test.go:280: PASS: output0:0 routed from input0:0 and unrouted, as read from the node's own active map
model_test.go:302: FAIL-expected: no IS-11 on this Device, refused by the control's name
node_test.go:129: PASS: node 2c47bf5e-1b2c-4abc-9def-deadbeef0001 registered 1 device(s), 9 sender(s), 3 receiver(s), stayed 15 s on its heartbeats, and deregistered on stop
peer_test.go:339: PASS: 92 resource(s) of the oracle registry are in ours, document for document, held 15 s; forwarded 92, refused or failed 0, repairs 0
```

## `ansible/playbooks/amwa-interop-is11.yml`

The `compat` verb against the NMOS-Reference IS-11 Node: a Sender read,
dry-run, constrained to 1920x1080, refused on a parameter it does not
support (FAIL-expected) and released; a Receiver read. All pass, each
step as the peer's own IS-11 API reports it (`compat_peer_test.go`).

## `ansible/playbooks/amwa-verify-manual.yml`

The rows the tool leaves to a person, each driven and read back.

```
IS-04-02 test_25_1     REST ancestry both directions      PASS
IS-08-01 test_09       input properties authored          PASS
IS-08-01 test_10       output properties authored         PASS
IS-04-03 test_02       ver_rcv TXT +1 on IS-05 activation PASS
IS-09-02 test_05       heartbeat_interval applied (INFO log) PASS
BCP-008 test_04        fault clear held by reporting delay PASS
BCP-008 test_11        sync change dips PartiallyHealthy  PASS
BCP-008-01 test_15/16  late counters inject/get/reset     PASS
BCP-008-02 test_15     tx error counters inject/get/reset PASS
BCP-007-03 test_15     MXL monitors track master_enable   PASS
```

Eleven passes of this play ran on v0.40.2; nine gave the ten rows
above with `changed=0`, two did not:

- One failed on `BCP-008 test_11`: the play had set the reporting delay
  to 1 s and read the dip one task later, after it was over. The play's
  delay is 4 s since, and no pass has failed there again. The node was
  not at fault.
- One failed on `IS-04-03 test_02`: after the activation, five browses
  in a row did not print the Node's `ver_rcv`. It has not happened
  again in the eight passes since, and its cause is not known (#1417);
  the play now keeps what that browse printed.

The reboot row (`amwa-reboot-resilience.yml`) was scored on the released
v0.40.3 on 2026-10-06, from the secondary runner: the plant host
rebooted at 21:11:51 UTC and was back 21 s later, the registry active
3 s after boot.

```
reboot resilience OK: 24 unit(s) active, all 22 of our nodes re-registered under their ids (23 nodes in the shared registry), ids stable on every node across 5 collections (200 resource ids persisted — IS-04-01 test_22 equivalent), mirror cache nodes=25,
boot_id b8f5e42e-3f23-4b2a-981d-e644c2ff944b -> fe7bc709-d60c-4fcb-93bb-7f75ebda19b8
```

## The plant mirror

Over the whole round the plant's own mirror forwarded 31 073 documents
and deleted 812, with 0 refused (#1346).

## Against the earlier releases

- v0.35.0: the registry and mirror pairing failed — one Source refused
  (#1334) and the mirror placing resources at the wrong minor (#1336).
- v0.36.0: everything passed but the copy of the nmos-cpp registry into
  ours, which differed on one document (#1338).
- v0.36.1 fixed that and showed later minors at earlier endpoints
  (#1337); on the plant its mirror then misplaced 81 sources (#1351).
- v0.36.2: all of it passed (19 tests).
- v0.37.0: all of it passes, with the dissector replay added (20 tests).
- v0.40.2: all of it passes; the IS-11 pairing and a 19th replayed
  capture are added.
