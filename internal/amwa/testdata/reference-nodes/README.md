# Reference nodes (#849)

Two bundles that bracket the real world, for testing controllers,
registries, and the audit/probe verbs against known-shaped devices.

## `minimal-node.json`
The least a device can do and stay conformant: one device, one
source/flow/sender/receiver, RTP, **empty caps, no group hints, single
leg**. It finds every place a controller assumed more than the spec
guarantees. Verified to load and serve (`producer nmos serve --config`),
answering `/self` and one sender. Documentation addressing only.

## Full-spec node
The realistic exerciser is `ansible/roles/dhs_amwa_plant/files/
amwa-test-node.json` — every published IS-04 minor, IS-05 single+bulk,
IS-07/08/11, BCP-002 multi-role group hints, BCP-004 caps, 2022-7 SDP.
It drives every AMWA tool suite green.

Note: it is deliberately a *realistic* node, not a *clean* one — it
carries multi-transport senders (websocket tally, MQTT) that the
offline audit correctly flags (a master-enabled MQTT sender with no
destination; websocket senders with no RTP transport file). A
purpose-built zero-finding clean-full-spec bundle is tracked separately;
the realistic node is the better exerciser for the tools themselves.

## Version-mismatch matrix
Asserted deterministically in `internal/amwa/registry/version_matrix_test.go`
(`versionAllowed`), both directions, as IS-04 "Upgrade Path" words
them: a resource registered at a later minor is shown at an earlier
query, translated (keys removed) — an old controller sees new nodes;
one registered at an earlier minor is shown at a later query only
under `query.downgrade`.
