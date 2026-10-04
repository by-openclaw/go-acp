# Integration and nmos-cpp pairing — the released v0.36.0

Run from the control node on 2026-10-04 against the released binary the
fleet runs (`dhs v0.36.0`), the suite built from `main`
(`internal/amwa/integration/`). The peers: the nmos-cpp reference
registry and node on the tooling host, a transient nmos-cpp Node, and
the Neuron CONVERT. Verdict lines exactly as the suite printed them.

## `ansible/playbooks/amwa-interop-nmos-cpp.yml`

The registry and the mirror, scored by nmos-cpp. The transient Node was
checked to be in neither the plant registry nor the reference one before
the suite ran.

```
peer_test.go:63: PASS: node 1a03472f-6fd7-5a3a-b525-71017a4cfd22 — 91 resource(s) of its Node API are in our Query API, document for document
peer_test.go:87: PASS: listed at every one of 15 looks, health 1791136444 -> 1791136459
peer_test.go:128: PASS: 22 sender(s) over 11 page(s) of 2, each once
peer_test.go:151: PASS: the first grain carries the peer's 22 receiver(s), document for document
peer_test.go:178: PASS: everything registered again 5 s after our registry forgot the node
peer_test.go:260: PASS: 91 resource(s) level in the oracle registry at the fill and after a live registration; forwarded 184, deleted 91, refused 0, repairs 1; announced on its WebSocket; gone after the mirror stopped
peer_test.go:289: PASS: the node and every resource under it expired 10 s after the kill
peer_test.go:328: FAIL-real: our registry is not level with the oracle registry: [nodes 0cda2a74-84c1-5087-88d1-a9354ff14871 differs in [services]]
```

`TestMirrorCopiesTheOracleRegistry` — our mirror copying the nmos-cpp
registry into a registry of ours — is the one FAIL-real: 92 resources,
one document differs.

```
peer_test.go:328: FAIL-real: our registry is not level with the oracle registry: [nodes 0cda2a74-84c1-5087-88d1-a9354ff14871 differs in [services]]
```

That is #1338: our registry returns a re-encoding of the Node, with
`"authorization": false` added to each of its `services`.

## `ansible/playbooks/amwa-integration.yml`

The controller, the node, and the model verbs. 18 checks pass — two of
them reject paths that correctly rejected (FAIL-expected) — and the
mirror copy above fails here too; the play changed nothing on the host
(`changed=0`).

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
model_test.go:127: PASS: 38 objects over IS-12, the role paths the node's own IS-14 lists
model_test.go:150: PASS: root.ExampleControl.1p6 read, set to "dhs integration" and back to "Example control worker" over IS-12, each as the node's own IS-14 serves it
model_test.go:171: PASS: a change made through the node's IS-14 was printed by the IS-12 watch
model_test.go:212: PASS: root.ExampleControl.1p6 read, set and set back; a backup of 1 object(s) validated by the node for a restore, nothing applied
model_test.go:280: PASS: output0:0 routed from input0:0 and unrouted, as read from the node's own active map
model_test.go:302: FAIL-expected: no IS-11 on this Device, refused by the control's name
node_test.go:129: PASS: node 2c47bf5e-1b2c-4abc-9def-deadbeef0001 registered 1 device(s), 9 sender(s), 3 receiver(s), stayed 15 s on its heartbeats, and deregistered on stop
```
